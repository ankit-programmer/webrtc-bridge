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
	// Serve HTTP on port 8080
	http.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Cannot read body", http.StatusBadRequest)
			return
		}

		// Parse JSON
		var offer SDPExchange
		if err := json.Unmarshal(body, &offer); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Create a new PeerConnection
		peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			http.Error(w, "Failed to create PeerConnection", http.StatusInternalServerError)
			return
		}

		// Log when connection is established
		peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
			log.Printf("Connection State: %s\n", state.String())
		})

		// Log audio being received
		peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
			log.Printf("Received track with payload type: %d", track.PayloadType())
			go func() {
				for {
					packet, _, err := track.ReadRTP()
					if err != nil {
						log.Println("Error reading RTP:", err)
						return
					}
					log.Printf("Received RTP packet: SSRC=%d, Timestamp=%d", packet.SSRC, packet.Timestamp)
					// Forward packet to Mediasoup PlainTransport (optional, next step)
				}
			}()
		})

		// Set remote offer
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

		// Set local description
		err = peerConnection.SetLocalDescription(answer)
		if err != nil {
			http.Error(w, "Failed to set local description", http.StatusInternalServerError)
			return
		}

		// Wait for ICE gathering to complete
		<-webrtc.GatheringCompletePromise(peerConnection)

		// Send SDP answer
		resp := SDPExchange{
			SDP:  peerConnection.LocalDescription().SDP,
			Type: "answer",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	fmt.Println("Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
