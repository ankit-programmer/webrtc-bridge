package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	// "os"
	// "os/exec"
	// "time"

	// "github.com/pion/rtp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	// "github.com/pion/webrtc/v3/pkg/media"
)

type TransportDetails struct {
	Inbound  TransportSpec `json:"inbound"`
	Outbound TransportSpec `json:"outbound"`
}

type TransportSpec struct {
	IP          string `json:"ip"`
	Port        int    `json:"port"`
	SSRC        uint32 `json:"ssrc"`
	PayloadType uint8  `json:"payloadType,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	ClockRate   uint32 `json:"clockRate,omitempty"`
	Channels    uint16 `json:"channels,omitempty"`
}

// Struct to parse incoming SDP offer and send back answer
type SDPExchange struct {
	SDP       string           `json:"sdp"`
	Type      string           `json:"type"`
	Transport TransportDetails `json:"transport"`
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Get the current time on the server.
		currentTime := time.Now()

		// Format the time as a string.
		// You can change the layout string to format the time differently.
		// This layout "2006-01-02 15:04:05" is a specific format string Go uses as a reference.
		timeString := currentTime.Format("Mon Jan 2 15:04:05 -0700 MST 2006")

		// Write the formatted time string to the response body.
		// This is the equivalent of res.send() in Express.
		w.WriteHeader(http.StatusOK)                 // Set the HTTP status code to 200 OK.
		w.Header().Set("Content-Type", "text/plain") // Set the content type.
		fmt.Fprint(w, timeString)
	})
	http.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Cannot read body", http.StatusBadRequest)
			return
		}

		// Parse incoming SDP offer
		var offer SDPExchange
		if err := json.Unmarshal(body, &offer); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Create a new PeerConnection with public IP
		peerConnection, err := createPeerConnection("34.47.148.88") // your public IP
		if err != nil {
			http.Error(w, "Failed to create PeerConnection", http.StatusInternalServerError)
			return
		}

		opusTrack, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, Channels: 2, ClockRate: 48000},
			"audio", "pion",
		)
		if err != nil {
			log.Fatal(err)
		}
		_, err = peerConnection.AddTrack(opusTrack)
		if err != nil {
			log.Fatal(err)
		}

		go func() {
			target := fmt.Sprintf("%s:%d", offer.Transport.Outbound.IP, offer.Transport.Outbound.Port)
			log.Printf("📥 [Input] Listening for RTP from Mediasoup on %s", target)

			addr, err := net.ResolveUDPAddr("udp", target)
			if err != nil {
				log.Printf("❌ Failed to resolve Mediasoup outbound address: %v", err)
				return
			}

			conn, err := net.ListenUDP("udp", addr)
			if err != nil {
				log.Printf("❌ Failed to listen on Mediasoup outbound RTP: %v", err)
				return
			}
			defer conn.Close()

			buffer := make([]byte, 1500)
			var activeSSRC uint32 = 0
			seenSSRCs := make(map[uint32]bool)

			// Timestamp rewriting variables
			var baseTimestamp uint32 = 0       // Virtual timeline base
			var lastSourceTimestamp uint32 = 0 // Last timestamp from current source
			var timestampOffset uint32 = 0     // Offset to apply to new source
			var sequenceNumber uint16 = 0      // Continuous sequence number
			var isFirstPacket bool = true

			packetCount := 0

			for {
				n, _, err := conn.ReadFrom(buffer)
				if err != nil {
					log.Printf("❌ Error reading RTP from Mediasoup: %v", err)
					break
				}

				rtpPacket := &rtp.Packet{}
				if err := rtpPacket.Unmarshal(buffer[:n]); err != nil {
					log.Printf("❌ Failed to unmarshal RTP packet: %v", err)
					continue
				}

				// Detect NEW SSRC
				if !seenSSRCs[rtpPacket.SSRC] {
					seenSSRCs[rtpPacket.SSRC] = true

					if activeSSRC != 0 {
						log.Printf("🔄 Switching SSRC: %d → %d", activeSSRC, rtpPacket.SSRC)

						// Calculate offset to make timestamps continuous
						// New packets should continue from where old ones left off
						timestampOffset = lastSourceTimestamp - rtpPacket.Timestamp
						log.Printf("🔧 Timestamp offset: %d (last: %d, new: %d)", timestampOffset, lastSourceTimestamp, rtpPacket.Timestamp)
					} else {
						log.Printf("🎵 Started forwarding SSRC: %d", rtpPacket.SSRC)
						isFirstPacket = true
					}

					activeSSRC = rtpPacket.SSRC
					packetCount = 0
				}

				// Only forward packets from active SSRC
				if rtpPacket.SSRC == activeSSRC {
					// Initialize base timestamp on first packet
					if isFirstPacket {
						baseTimestamp = rtpPacket.Timestamp
						timestampOffset = 0
						sequenceNumber = rtpPacket.SequenceNumber
						isFirstPacket = false
						log.Printf("🎬 Initial timestamp: %d, sequence: %d", baseTimestamp, sequenceNumber)
					}

					// Rewrite timestamp to be continuous
					// originalTimestamp := rtpPacket.Timestamp
					rtpPacket.Timestamp = rtpPacket.Timestamp + timestampOffset
					lastSourceTimestamp = rtpPacket.Timestamp

					// Rewrite sequence number to be continuous
					rtpPacket.SequenceNumber = sequenceNumber
					sequenceNumber++

					// Keep the same SSRC (or use a fixed one)
					// rtpPacket.SSRC = 12345678 // Optional: use fixed SSRC

					// log.Printf("📦 Forwarding: SSRC=%d, Seq=%d, TS=%d→%d, Size=%d",rtpPacket.SSRC, rtpPacket.SequenceNumber, originalTimestamp, rtpPacket.Timestamp, len(rtpPacket.Payload))

					err = opusTrack.WriteRTP(rtpPacket)
					if err != nil {
						log.Printf("❌ Error writing RTP: %v", err)
						break
					}

					packetCount++
				}
			}
		}()
		// Log ICE + connection state
		peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
			log.Printf("ICE State: %s\n", state.String())

			if state == webrtc.ICEConnectionStateConnected {

			}
		})

		peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
			log.Printf("PeerConnection State: %s\n", state.String())

			switch state {
			case webrtc.PeerConnectionStateFailed,
				webrtc.PeerConnectionStateClosed,
				webrtc.PeerConnectionStateDisconnected:
				log.Println("🔌 Cleaning up PeerConnection...")

				peerConnection.Close() // ✅ Stop ICE, DTLS, SRTP, all tracks, transceivers
				// You can signal other goroutines to stop via channels or contexts
			}
		})
		// Audio is successfully sent to Mediasoup
		peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
			log.Printf("🔊 [Track] Receiving audio track: PayloadType=%d, SSRC=%d", track.PayloadType(), track.SSRC())

			go func() {
				target := fmt.Sprintf("%s:%d", offer.Transport.Inbound.IP, offer.Transport.Inbound.Port)
				log.Printf("➡️ Forwarding audio RTP to Mediasoup at %s", target)

				conn, err := net.Dial("udp", target)
				if err != nil {
					log.Printf("❌ Failed to connect to Mediasoup RTP port: %v", err)
					return
				}
				defer conn.Close()

				packetCount := 0
				for {
					rtpPacket, _, err := track.ReadRTP()
					if err != nil {
						log.Printf("❌ Failed to read RTP: %v", err)
						return
					}

					// Override SSRC and PayloadType to match what Mediasoup expects
					rtpPacket.SSRC = offer.Transport.Inbound.SSRC
					rtpPacket.PayloadType = offer.Transport.Inbound.PayloadType

					raw, err := rtpPacket.Marshal()
					if err != nil {
						log.Printf("❌ Failed to marshal RTP: %v", err)
						return
					}

					_, err = conn.Write(raw)
					if err != nil {
						log.Printf("❌ Failed to send RTP to Mediasoup: %v", err)
						return
					}

					packetCount++
					if packetCount%50 == 0 {
						log.Printf("📦 RTP packet #%d sent to Mediasoup (SSRC: %d, PayloadType: %d)", packetCount, rtpPacket.SSRC, rtpPacket.PayloadType)
					}
				}
			}()
		})
		// Set remote offer from WhatsApp
		err = peerConnection.SetRemoteDescription(webrtc.SessionDescription{
			SDP:  offer.SDP,
			Type: webrtc.SDPTypeOffer,
		})
		if err != nil {
			http.Error(w, "Failed to set remote description", http.StatusInternalServerError)
			return
		}

		// Create answer
		answer, err := peerConnection.CreateAnswer(nil)
		if err != nil {
			http.Error(w, "Failed to create answer", http.StatusInternalServerError)
			return
		}

		// Start ICE gathering before setting local description
		gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

		// Set the *pending* local description
		err = peerConnection.SetLocalDescription(answer)
		if err != nil {
			http.Error(w, "Failed to set local description", http.StatusInternalServerError)
			return
		}

		// Wait until ICE is fully gathered (candidates + port are ready)
		<-gatherComplete

		// Now LocalDescription() will have real IP and port
		finalSDP := peerConnection.LocalDescription().SDP

		// OPTIONAL: Confirm you're not leaking `m=audio 9`
		log.Println("Generated SDP Answer:\n", finalSDP)

		// Return the SDP answer
		resp := SDPExchange{
			SDP:  finalSDP,
			Type: "answer",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	fmt.Println("✅ Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func createPeerConnection(publicIP string) (*webrtc.PeerConnection, error) {
	// Step 1: Configure the SettingEngine with NAT1To1 IP mapping and passive DTLS
	settingEngine := webrtc.SettingEngine{}
	settingEngine.SetNAT1To1IPs([]string{publicIP}, webrtc.ICECandidateTypeHost)
	settingEngine.SetAnsweringDTLSRole(webrtc.DTLSRoleClient)

	// Step 2: Register codecs using MediaEngine
	mediaEngine := webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}

	// Step 3: Create API with custom setting and media engine
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(&mediaEngine),
		webrtc.WithSettingEngine(settingEngine),
	)

	// Step 4: Prepare config
	config := webrtc.Configuration{}

	// Step 5: Create PeerConnection
	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}

	// 🔥 Add a transceiver so that the SDP includes media section + a=setup line
	_, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	if err != nil {
		return nil, err
	}

	return peerConnection, nil
}
