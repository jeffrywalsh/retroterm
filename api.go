package main

import (
	"encoding/json"
	"net/http"
)

type ConfigResponse struct{}

type BBSListResponse struct {
	Success bool      `json:"success"`
	BBSList []BBSInfo `json:"bbsList"`
}

// Handler to get public configuration
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

// Handler to get default BBS list
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
