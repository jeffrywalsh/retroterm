package main

import (
    "encoding/csv"
    "encoding/json"
    "io"
    "net/http"
    "os"
    "regexp"
    "strconv"
    "strings"
)

// Get the full BBS directory (CSV is the single source of truth)
func handleGetBBSDirectory(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    entries, err := GetBBSDirectoryEntries()
    if err != nil {
        // Return empty list on error to keep UI functional
        // while preserving CSV as the only source of truth.
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode([]BBSEntry{})
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(entries)
}

// Import Telnet BBS Guide raw text and regenerate bbs.csv
func handleImportBBSGuide(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		http.Error(w, "No data provided", http.StatusBadRequest)
		return
	}

    entries := parseBBSGuide(string(body))
    if len(entries) == 0 {
        http.Error(w, "No entries parsed", http.StatusBadRequest)
        return
    }

    // Write to bbs.csv (single source of truth)
    f, err := os.Create("bbs.csv")
    if err != nil {
        http.Error(w, "Failed to write bbs.csv", http.StatusInternalServerError)
        return
    }
    defer f.Close()

    cw := csv.NewWriter(f)
    // Header must match LoadBBSFromCSV expectations
    if err := cw.Write([]string{"Name", "Software", "Telnet Server Address"}); err != nil {
        http.Error(w, "Failed to write CSV header", http.StatusInternalServerError)
        return
    }
    for _, e := range entries {
        addr := e.Host
        if e.Port > 0 {
            addr = addr + ":" + strconv.Itoa(e.Port)
        }
        if err := cw.Write([]string{e.Name, e.Software, addr}); err != nil {
            http.Error(w, "Failed to write CSV row", http.StatusInternalServerError)
            return
        }
    }
    cw.Flush()
    if err := cw.Error(); err != nil {
        http.Error(w, "Failed to finalize CSV", http.StatusInternalServerError)
        return
    }

    // Refresh approved list from CSV
    _ = refreshApprovedBBSList()

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]any{"success": true, "count": len(entries)})
}

// Minimal parser for Telnet BBS Guide text -> BBSEntry slice
func parseBBSGuide(text string) []BBSEntry {
    lines := strings.Split(text, "\n")
    var entries []BBSEntry
    var cur *BBSEntry
    nameRe := regexp.MustCompile(`^\s{2,}([\w\*\-\'\!\?\&\./\\,:;\(\)\[\]#@\+ ]{3,})$`)
    // Match 'Software: Foo'
    softwareRe := regexp.MustCompile(`^Software:\s*([^\t\r\n]+)$`)
    // Match 'Telnet: host[:port]' and allow optional user@
    telnetRe := regexp.MustCompile(`^Telnet:\s*([^\s]+)`) // take token until whitespace

    finalize := func() {
        if cur == nil {
            return
        }
        // Only include if we have host
        if cur.Host != "" {
            if cur.Port == 0 {
                cur.Port = 23
            }
            if cur.Protocol == "" {
                cur.Protocol = "telnet"
            }
            // ensure defaults
            if cur.Encoding == "" {
                cur.Encoding = "CP437"
            }
            if cur.Description == "" {
                cur.Description = cur.Name + " BBS"
            }
            cur.Active = true
            entries = append(entries, *cur)
        }
        cur = nil
    }

	for _, raw := range lines {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "---") { // divider
			finalize()
			continue
		}
		if strings.HasPrefix(t, "Last Updated:") {
			continue
		}
		if strings.HasPrefix(t, "SSH:") || strings.HasPrefix(t, "WEB:") || strings.HasPrefix(t, "Email:") || strings.HasPrefix(t, "Location:") || strings.HasPrefix(t, "Dial-Up:") || strings.HasPrefix(t, "BBS:") {
			// not used for now
			continue
		}

        if m := telnetRe.FindStringSubmatch(t); m != nil {
            if cur == nil {
                cur = &BBSEntry{}
            }
            addr := strings.TrimSpace(m[1])
            // strip protocol-like prefixes
            if i := strings.Index(addr, "//"); i != -1 {
                addr = addr[i+2:]
            }
            // remove username@ if present
            if i := strings.LastIndex(addr, "@"); i != -1 {
                addr = addr[i+1:]
            }
            host := addr
            port := 23
            if i := strings.LastIndex(addr, ":"); i != -1 {
                host = addr[:i]
                p := addr[i+1:]
                if p != "" {
                    if v, err := strconv.Atoi(p); err == nil {
                        port = v
                    }
                }
            }
            cur.Host = host
            cur.Port = port
            if cur.Name == "" {
                cur.Name = host
            }
            cur.Protocol = "telnet"
            continue
        }

        if m := softwareRe.FindStringSubmatch(t); m != nil {
            if cur == nil {
                cur = &BBSEntry{}
            }
            // Trim any trailing fields like 'Total Nodes:' or 'Login:'
            val := strings.TrimSpace(m[1])
            if i := strings.Index(val, "  "); i != -1 {
                val = strings.TrimSpace(val[:i])
            }
            if i := strings.Index(val, "\t"); i != -1 {
                val = strings.TrimSpace(val[:i])
            }
            if i := strings.Index(val, "Total Nodes:"); i != -1 {
                val = strings.TrimSpace(val[:i])
            }
            if i := strings.Index(val, "Login:"); i != -1 {
                val = strings.TrimSpace(val[:i])
            }
            cur.Software = val
            continue
        }

        if m := nameRe.FindStringSubmatch(raw); m != nil {
            // Heuristic: if line contains ':' it's not a name
            if strings.Contains(m[1], ":") {
                continue
            }
            // finalize previous
            finalize()
            nm := strings.TrimSpace(m[1])
            // Remove leading '*' markers
            nm = strings.TrimPrefix(nm, "*")
            nm = strings.TrimSpace(nm)
            cur = &BBSEntry{Name: nm}
            continue
        }
    }
    finalize()
    return entries
}

// Favorites endpoint intentionally omitted; single source is bbs.csv
