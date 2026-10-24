package main

// CSV-backed directory loader and a tiny in-process cache for the curated BBS list.

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CharsetOverride represents a BBS-specific charset and window size override
type CharsetOverride struct {
	BBSName    string
	Charset    string
	WindowSize string // format: "80x25" or empty for default
}

// loadCharsetOverrides loads charset overrides from charset_bbs.csv
// Format: BBS name, charset (if not CP437), window size (if not 80x25)
func loadCharsetOverrides() []CharsetOverride {
	var overrides []CharsetOverride

	file, err := os.Open("charset_bbs.csv")
	if err != nil {
		// File doesn't exist or can't be opened, return empty list
		return overrides
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1 // Allow variable number of fields

	records, err := reader.ReadAll()
	if err != nil {
		return overrides
	}

	for _, record := range records {
		if len(record) < 1 {
			continue
		}

		override := CharsetOverride{
			BBSName: strings.TrimSpace(record[0]),
		}

		// Charset (if specified and not CP437)
		if len(record) > 1 {
			charset := strings.TrimSpace(record[1])
			if charset != "" && charset != "CP437" {
				override.Charset = charset
			}
		}

		// Window size (if specified and not 80x25)
		if len(record) > 2 {
			windowSize := strings.TrimSpace(record[2])
			if windowSize != "" && windowSize != "80x25" {
				override.WindowSize = windowSize
			}
		}

		// Only add if there's actually an override
		if override.Charset != "" || override.WindowSize != "" {
			overrides = append(overrides, override)
		}
	}

	return overrides
}

// findCharsetOverride finds a charset override for a given BBS name or host
func findCharsetOverride(overrides []CharsetOverride, name, host string) string {
	for _, override := range overrides {
		// Check if BBS name matches (case-insensitive)
		if strings.EqualFold(override.BBSName, name) {
			if override.Charset != "" {
				return override.Charset
			}
		}
		// Also check if the override name matches the host
		if strings.Contains(strings.ToLower(host), strings.ToLower(override.BBSName)) {
			if override.Charset != "" {
				return override.Charset
			}
		}
	}
	return ""
}

// BBSEntry represents a single BBS listing parsed from bbs.csv.
// Only a subset of fields are used by the UI/runtime today; others are
// reserved for future enhancements or richer directories.
type BBSEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	Description string `json:"description"`
	Encoding    string `json:"encoding"`
	Category    string `json:"category"`
	Location    string `json:"location"`
	SysOp       string `json:"sysop"`
	Software    string `json:"software"`
	Active      bool   `json:"active"`
	IsFavorite  bool   `json:"is_favorite,omitempty"`
	Slug        string `json:"slug"`
}

// LoadBBSFromCSV loads BBS entries from a CSV file with header
// [Name, Software, Telnet Server Address]. Address may be host or host:port.
// Missing ports default to 23 (telnet). Invalid rows are skipped.
func LoadBBSFromCSV(filename string) ([]BBSEntry, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read header line
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}

	// Map header names to indices (case sensitive as authored)
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}

	// Validate minimum required columns
	nameIdx, okName := idx["Name"]
	swIdx, okSw := idx["Software"]
	addrIdx, okAddr := idx["Telnet"]
	if !okAddr {
		// Fallback to old header format
		addrIdx, okAddr = idx["Telnet Server Address"]
	}
	if !(okName && okSw && okAddr) {
		return nil, fmt.Errorf("invalid CSV header format")
	}
	locIdx, hasLoc := idx["Location"]

	var entries []BBSEntry
	overrides := loadCharsetOverrides()

	// Read all records
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	for _, record := range records {
		// guard length
		if len(record) <= addrIdx || len(record) <= swIdx || len(record) <= nameIdx {
			continue
		}

		name := strings.TrimSpace(record[nameIdx])
		software := strings.TrimSpace(record[swIdx])
		address := strings.TrimSpace(record[addrIdx])
		var location string
		if hasLoc && len(record) > locIdx {
			location = strings.TrimSpace(record[locIdx])
		}

		if name == "" || address == "" {
			continue
		}

		// Parse address (host:port)
		host := address
		port := 23 // default telnet port

		if idx := strings.LastIndex(address, ":"); idx != -1 {
			host = address[:idx]
			if portStr := address[idx+1:]; portStr != "" {
				if p, err := strconv.Atoi(portStr); err == nil {
					port = p
				}
			}
		}

		// Generate ID from name (lowercase, replace spaces with underscores)
		id := strings.ToLower(name)
		id = strings.ReplaceAll(id, " ", "_")
		id = strings.ReplaceAll(id, "'", "")
		id = strings.ReplaceAll(id, ".", "")
		id = strings.ReplaceAll(id, ",", "")
		id = strings.ReplaceAll(id, "!", "")
		id = strings.ReplaceAll(id, "?", "")
		id = strings.ReplaceAll(id, "&", "and")
		id = strings.ReplaceAll(id, "(", "")
		id = strings.ReplaceAll(id, ")", "")
		id = strings.ReplaceAll(id, "[", "")
		id = strings.ReplaceAll(id, "]", "")
		id = strings.ReplaceAll(id, "/", "_")
		id = strings.ReplaceAll(id, "\\", "_")
		id = strings.ReplaceAll(id, "-", "_")
		id = strings.ReplaceAll(id, "__", "_")

		// Auto-detect encoding from software field
		encoding := "CP437"
		softwareLower := strings.ToLower(software)
		if strings.Contains(softwareLower, "petscii") ||
			strings.Contains(softwareLower, "color 64") ||
			strings.Contains(softwareLower, "c64") ||
			strings.Contains(softwareLower, "commodore") ||
			strings.Contains(softwareLower, "image bbs") ||
			strings.Contains(softwareLower, "c*base") {
			encoding = "PETSCIIU" // Default to upper/graphics mode
		} else if strings.Contains(softwareLower, "atascii") ||
			strings.Contains(softwareLower, "atari") {
			encoding = "ATASCII"
		}

		entry := BBSEntry{
			ID:          id,
			Name:        name,
			Host:        host,
			Port:        port,
			Protocol:    "telnet",
			Description: fmt.Sprintf("%s BBS", name),
			Encoding:    encoding,
			Software:    software,
			Location:    location,
			Active:      true,
			Slug:        GenerateSlug(name),
		}

		// Explicit override from charset_bbs.csv takes precedence
		if override := findCharsetOverride(overrides, name, host); override != "" {
			entry.Encoding = override
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// Simple cache for CSV to avoid re-reading on every request
var (
	bbsCache      []BBSEntry
	bbsCacheMTime time.Time
	bbsCacheMu    sync.RWMutex
)

// GetBBSDirectoryEntries returns BBS entries from bbs.csv with basic mtime
// caching. A defensive copy is returned to callers to prevent accidental
// mutation of the cached slice.
func GetBBSDirectoryEntries() ([]BBSEntry, error) {
	const file = "bbs.csv"
	fi, err := os.Stat(file)
	if err != nil {
		return nil, err
	}

	mtime := fi.ModTime()

	bbsCacheMu.RLock()
	if len(bbsCache) > 0 && mtime.Equal(bbsCacheMTime) {
		// Return a copy to avoid external mutations
		out := make([]BBSEntry, len(bbsCache))
		copy(out, bbsCache)
		bbsCacheMu.RUnlock()
		return out, nil
	}
	bbsCacheMu.RUnlock()

	// Load fresh
	entries, err := LoadBBSFromCSV(file)
	if err != nil {
		return nil, err
	}

	bbsCacheMu.Lock()
	bbsCache = make([]BBSEntry, len(entries))
	copy(bbsCache, entries)
	bbsCacheMTime = mtime
	bbsCacheMu.Unlock()

	// Return a copy
	out := make([]BBSEntry, len(entries))
	copy(out, entries)
	return out, nil
}
