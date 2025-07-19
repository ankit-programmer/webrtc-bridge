package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/pion/webrtc/v3"
)

// Struct to parse incoming SDP offer and send back answer
type SDPExchange struct {
	SDP  string `json:"sdp"`
	Type string `json:"type"`
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

		// Log ICE + connection state
		peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
			log.Printf("ICE State: %s\n", state.String())
		})
		peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
			log.Printf("PeerConnection State: %s\n", state.String())
		})

		// Track handler
		peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
			log.Printf("Receiving audio (payload type %d)", track.PayloadType())
			go func() {
				for {
					packet, _, err := track.ReadRTP()
					if err != nil {
						log.Println("RTP Read Error:", err)
						return
					}
					log.Printf("Got RTP packet: TS=%d", packet.Timestamp)
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
	settingEngine.SetAnsweringDTLSRole(webrtc.DTLSRoleAuto)

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
