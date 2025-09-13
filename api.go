package main

// API handlers for read-only public endpoints exposed by the server.

import (
	"encoding/json"
	"net/http"
)

type ConfigResponse struct{}

type BBSListResponse struct {
	Success bool      `json:"success"`
	BBSList []BBSInfo `json:"bbsList"`
}

// handleGetConfig responds with a minimal, public-safe configuration payload.
// The app is stateless; this endpoint exists for forward compatibility.
func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	config := ConfigResponse{}

	// Stateless-only: return minimal config

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// handleGetDefaultBBSList returns the curated, approved BBS list that
// the UI presents to users for safe connection selection.
func handleGetDefaultBBSList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bbsList := GetBBSList()

	response := BBSListResponse{
		Success: true,
		BBSList: bbsList,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
