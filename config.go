package main

import (
    "encoding/json"
    "fmt"
    "os"
)

// Config holds server and proxy settings loaded from config.json.
// Fields are a minimal subset intentionally kept stable for stateless use.
type Config struct {
	Server struct {
		Port            int    `json:"port"`
		UseCuratedList  bool   `json:"useCuratedList"`
		ExternalBaseURL string `json:"externalBaseURL"`
	} `json:"server"`
	// Email and Database removed in stateless mode; kept here for backward-compat JSON parsing
	Email    any `json:"email"`
	Database any `json:"database"`
	Proxy    struct {
		Enabled  bool   `json:"enabled"`
		Type     string `json:"type"` // "socks5"
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"proxy"`
	DefaultBBSList []BBSInfo `json:"defaultBBSList"`
}

var AppConfig *Config

// LoadConfig reads and parses a JSON config file and applies defaults where
// appropriate. It returns an error if the file is missing or invalid.
func LoadConfig(path string) (*Config, error) {
	// Check if config file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", path)
	}

    // Read config file
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("error reading config file: %v", err)
    }

	// Parse JSON
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing config file: %v", err)
	}

	// Set default values if not specified
	if config.Server.Port == 0 {
		config.Server.Port = 8080
	}
	// Stateless-only: no mode switching

	AppConfig = &config
	return &config, nil
}

// GetBBSList returns the curated/approved BBS list populated from CSV.
// Kept as a function for future flexibility.
func GetBBSList() []BBSInfo {
    // Return the approved BBS list populated from CSV (curated)
    // Maintains backward compatibility with existing handlers.
    return ApprovedBBSList
}
