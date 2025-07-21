package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	// "github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
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

		opusTrack, err := webrtc.NewTrackLocalStaticSample(
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
			for {
				n, _, err := conn.ReadFrom(buffer)
				if err != nil {
					log.Printf("❌ Error reading RTP from Mediasoup: %v", err)
					break
				}

				packet := make([]byte, n)
				copy(packet, buffer[:n])

				err = opusTrack.WriteSample(media.Sample{
					Data:     packet,
					Duration: 20 * time.Millisecond,
				})
				if err != nil {
					log.Printf("❌ Error writing sample to WebRTC track: %v", err)
					break
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

			if state == webrtc.PeerConnectionStateConnected {
				log.Println("🔊 Starting audio transmission to WA...")
				// go func() {
				// 	if err := sendAudioFromMP3(peerConnection, opusTrack); err != nil {
				// 		log.Println("Audio send error:", err)
				// 	}
				// }()
			}
		})

		// peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		// 	log.Printf("🔊 [Track] Receiving audio track: PayloadType=%d, SSRC=%d", track.PayloadType(), track.SSRC())

		// 	go func() {
		// 		target := fmt.Sprintf("%s:%d", offer.Transport.Inbound.IP, offer.Transport.Inbound.Port)
		// 		log.Printf("➡️ [Forward] Connecting to Mediasoup inbound at %s", target)

		// 		conn, err := net.Dial("udp", target)
		// 		if err != nil {
		// 			log.Printf("❌ [Error] Failed to connect to Mediasoup inbound transport: %v", err)
		// 			return
		// 		}
		// 		defer func() {
		// 			log.Println("🔌 [Forward] Closing connection to Mediasoup")
		// 			conn.Close()
		// 		}()

		// 		packetCount := 0
		// 		for {
		// 			rtpPacket, _, err := track.ReadRTP()
		// 			if err != nil {
		// 				log.Printf("❌ [Error] RTP read failed: %v", err)
		// 				return
		// 			}

		// 			originalSSRC := rtpPacket.SSRC
		// 			rtpPacket.SSRC = offer.Transport.Inbound.SSRC

		// 			raw, err := rtpPacket.Marshal()
		// 			if err != nil {
		// 				log.Printf("❌ [Error] Failed to marshal RTP packet: %v", err)
		// 				return
		// 			}

		// 			n, err := conn.Write(raw)
		// 			if err != nil {
		// 				log.Printf("❌ [Error] Failed to write to transport: %v", err)
		// 				return
		// 			}

		// 			packetCount++
		// 			log.Printf("📦 [RTP] Packet #%d | Size=%d bytes | Seq=%d | TS=%d | SSRC(orig=%d ➝ sent=%d) | Payload(type=%s)",
		// 				packetCount, n, rtpPacket.SequenceNumber, rtpPacket.Timestamp, originalSSRC, rtpPacket.SSRC, rtpPacket.PayloadType)
		// 		}
		// 	}()
		// })

		// peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		// 	log.Printf("🔊 [Track] Receiving audio track: PayloadType=%d, SSRC=%d", track.PayloadType(), track.SSRC())

		// 	go func() {
		// 		const frameDuration = 20 * time.Millisecond
		// 		const delayFrames = 5       // 100ms / 20ms
		// 		const maxPayloadSize = 1200 // Typical Opus frame size upper bound

		// 		type sample struct {
		// 			data []byte
		// 			time time.Time
		// 		}

		// 		buffer := make([]sample, 0, delayFrames)

		// 		ticker := time.NewTicker(frameDuration)
		// 		defer ticker.Stop()

		// 		for {
		// 			rtpPacket, _, err := track.ReadRTP()
		// 			if err != nil {
		// 				log.Printf("❌ [Error] RTP read failed: %v", err)
		// 				return
		// 			}

		// 			// Add to buffer
		// 			buffer = append(buffer, sample{
		// 				data: append([]byte{}, rtpPacket.Payload...), // copy payload
		// 				time: time.Now(),
		// 			})

		// 			// Wait for playback time
		// 			<-ticker.C

		// 			// If buffer has enough samples, play the oldest
		// 			if len(buffer) >= delayFrames {
		// 				oldest := buffer[0]
		// 				buffer = buffer[1:]

		// 				err = opusTrack.WriteSample(media.Sample{
		// 					Data:     oldest.data,
		// 					Duration: frameDuration,
		// 				})
		// 				if err != nil {
		// 					log.Printf("❌ [Error] Writing sample failed: %v", err)
		// 					return
		// 				}
		// 			}
		// 		}
		// 	}()
		// })
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

		// // Send audio to WhatsApp
		// go func() {
		// 	senderConn, err := net.Dial("udp", fmt.Sprintf("%s:%d", offer.Transport.Outbound.IP, offer.Transport.Outbound.Port))
		// 	if err != nil {
		// 		log.Println("Cannot connect to WA outbound:", err)
		// 		return
		// 	}
		// 	defer senderConn.Close()

		// 	var seqNumber uint16 = 0
		// 	var timestamp uint32 = 0
		// 	ticker := time.NewTicker(20 * time.Millisecond)
		// 	defer ticker.Stop()

		// 	for range ticker.C {
		// 		sample := getOpusFrame() // You’ll implement this
		// 		packet := &rtp.Packet{
		// 			Header: rtp.Header{
		// 				Version:        2,
		// 				PayloadType:    offer.Transport.Outbound.PayloadType,
		// 				SequenceNumber: seqNumber,
		// 				Timestamp:      timestamp,
		// 				SSRC:           offer.Transport.Outbound.SSRC,
		// 			},
		// 			Payload: sample,
		// 		}
		// 		seqNumber++
		// 		timestamp += 960 // For 20ms @ 48kHz

		// 		raw, _ := packet.Marshal()
		// 		senderConn.Write(raw)
		// 	}
		// }()

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

func sendAudioFromMP3(pc *webrtc.PeerConnection, track *webrtc.TrackLocalStaticSample) error {
	for {
		// log.Println("🔁 Starting new audio loop...")

		// Open the MP3 file
		file, err := os.Open("audio.mp3")
		if err != nil {
			return fmt.Errorf("failed to open audio file: %w", err)
		}

		// Start ffmpeg: convert MP3 to raw Opus at 48kHz stereo
		cmd := exec.Command("ffmpeg",
			"-i", "pipe:0",
			"-f", "opus",
			"-ar", "48000",
			"-ac", "2",
			"-loglevel", "quiet", // remove for debugging
			"pipe:1")
		cmd.Stdin = file

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to get ffmpeg stdout: %w", err)
		}

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start ffmpeg: %w", err)
		}

		buf := make([]byte, 960) // 20ms frame @ 48kHz (Opus frame size)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		for {
			n, err := io.ReadFull(stdout, buf)
			if err != nil {
				// log.Println("🔁 End of stream, restarting:", err)
				break // break to outer loop
			}

			<-ticker.C // wait for 20ms tick

			err = track.WriteSample(media.Sample{
				Data:     buf[:n],
				Duration: 20 * time.Millisecond,
			})
			if err != nil {
				log.Println("❌ WriteSample error:", err)
				return err
			}
			// log.Printf("🎧 Sent %d bytes\n", n)
		}

		cmd.Wait()
		file.Close()
	}
}
