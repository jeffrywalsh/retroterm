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

	// Validate header
	if len(header) < 3 || header[0] != "Name" || header[1] != "Software" || header[2] != "Telnet Server Address" {
		return nil, fmt.Errorf("invalid CSV header format")
	}

	var entries []BBSEntry

	// Read all records
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	for _, record := range records {
		if len(record) < 3 {
			continue
		}

		name := strings.TrimSpace(record[0])
		software := strings.TrimSpace(record[1])
		address := strings.TrimSpace(record[2])

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

		entry := BBSEntry{
			ID:          id,
			Name:        name,
			Host:        host,
			Port:        port,
			Protocol:    "telnet",
			Description: fmt.Sprintf("%s BBS", name),
			Encoding:    "CP437",
			Software:    software,
			Active:      true,
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// Simple cache for CSV to avoid re-reading on every request
var (
    bbsCache       []BBSEntry
    bbsCacheMTime  time.Time
    bbsCacheMu     sync.RWMutex
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
