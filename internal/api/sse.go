package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/rapidbuildapp/rapidbuild/internal/middleware"
	"github.com/rapidbuildapp/rapidbuild/internal/models"
)

// SSEHandler handles Server-Sent Events for build progress
func (h *AppHandler) SSEHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		middleware.RespondError(w, http.StatusUnauthorized, "User not found in context")
		return
	}

	vars := mux.Vars(r)
	versionID := vars["versionId"]

	// Verify user owns the app
	version, err := h.VersionService.GetVersion(r.Context(), versionID)
	if err != nil {
		middleware.RespondError(w, http.StatusNotFound, "Version not found")
		return
	}

	_, err = h.AppService.GetApp(r.Context(), version.AppID, user.Sub)
	if err != nil {
		middleware.RespondError(w, http.StatusNotFound, "App not found")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Check if version is already completed/failed
	if version.Status == "completed" || version.Status == "failed" {
		data, _ := json.Marshal(map[string]string{
			"version_id": versionID,
			"status":     version.Status,
			"message":    "Build " + version.Status,
		})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return
	}

	// Send initial connection message
	fmt.Fprintf(w, "data: {\"status\":\"connected\"}\n\n")
	flusher.Flush()

	// Check if Redis is configured
	if h.Builder.RedisClient == nil {
		fmt.Fprintf(w, "data: {\"status\":\"error\",\"message\":\"Redis not configured\"}\n\n")
		flusher.Flush()
		return
	}

	// Subscribe to Redis channel for this version
	channel := fmt.Sprintf("build:progress:%s", versionID)
	ctx := context.Background()
	pubsub := h.Builder.RedisClient.Subscribe(ctx, channel)
	defer pubsub.Close()

	// Wait for subscription confirmation
	_, err = pubsub.Receive(ctx)
	if err != nil {
		log.Printf("[SSE] Failed to confirm subscription: %v\n", err)
		fmt.Fprintf(w, "data: {\"status\":\"error\",\"message\":\"Failed to subscribe\"}\n\n")
		flusher.Flush()
		return
	}

	log.Printf("[SSE] Client connected for version %s\n", versionID)

	// Get the channel for receiving messages
	ch := pubsub.Channel()

	// Create ticker for heartbeat
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	// Create timeout (10 hours max)
	timeout := time.NewTimer(10 * time.Hour)
	defer timeout.Stop()

	// Listen for progress updates from Redis
	for {
		select {
		case <-r.Context().Done():
			log.Printf("[SSE] Client disconnected for version %s\n", versionID)
			return
		case <-timeout.C:
			log.Printf("[SSE] Timeout reached for version %s\n", versionID)
			return
		case <-heartbeat.C:
			// Send heartbeat comment to keep connection alive
			_, err := fmt.Fprintf(w, ": heartbeat\n\n")
			if err != nil {
				log.Printf("[SSE] Failed to write heartbeat: %v\n", err)
				return
			}
			flusher.Flush()
		case msg, ok := <-ch:
			if !ok {
				log.Printf("[SSE] Channel closed for version %s\n", versionID)
				return
			}

			// Parse progress message
			var progress models.BuildProgress
			if err := json.Unmarshal([]byte(msg.Payload), &progress); err != nil {
				log.Printf("[SSE] Failed to unmarshal message: %v\n", err)
				continue
			}

			// Send to client
			data, _ := json.Marshal(progress)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			// Close connection when build is complete or failed
			if progress.Status == "completed" || progress.Status == "failed" {
				log.Printf("[SSE] Build %s for version %s\n", progress.Status, versionID)
				return
			}
		}
	}
}
