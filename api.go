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

// handleGetBBSBySlug returns BBS information based on slug
func handleGetBBSBySlug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract slug from URL path
	slug := r.URL.Query().Get("slug")
	if slug == "" {
		http.Error(w, "Missing slug parameter", http.StatusBadRequest)
		return
	}

	// Get BBS directory entries
	entries, err := GetBBSDirectoryEntries()
	if err != nil {
		http.Error(w, "Failed to load BBS directory", http.StatusInternalServerError)
		return
	}

	// Find BBS by slug
	bbs := FindBBSBySlug(slug, entries)
	if bbs == nil {
		http.Error(w, "BBS not found", http.StatusNotFound)
		return
	}

	// Convert to BBSInfo format for client
	bbsInfo := BBSInfo{
		ID:          bbs.ID,
		Name:        bbs.Name,
		Host:        bbs.Host,
		Port:        bbs.Port,
		Protocol:    bbs.Protocol,
		Description: bbs.Description,
		Encoding:    bbs.Encoding,
		Location:    bbs.Location,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bbsInfo)
}
