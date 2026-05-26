package api

import (
	"fmt"
	"net/http"
)

// handleSSE upgrades an HTTP connection to a Server-Sent Events stream.
// Each connected player gets their own goroutine receiving from the appropriate channel.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	playerID := r.URL.Query().Get("playerId")
	side := r.URL.Query().Get("side") // "FREE_PEOPLES" | "SHADOW"
	if playerID == "" || (side != "FREE_PEOPLES" && side != "SHADOW") {
		http.Error(w, "playerId and side required", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	writeCh := make(chan []byte, 256)
	s.newConnectionCh <- SSEConnection{
		PlayerID: playerID,
		Side:     side,
		WriteCh:  writeCh,
	}

	// Per-player goroutine — reads from writeCh and writes to HTTP response
	// This goroutine is cleaned up when the client disconnects
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			s.disconnectCh <- playerID
			return
		case data, ok := <-writeCh:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
