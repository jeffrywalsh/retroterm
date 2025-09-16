package main

// Server entry point and WebSocket <-> telnet/SSH bridge. This file wires the
// HTTP API, static file serving, WebSocket session lifecycle, and the
// ZMODEM/Telnet processing pipeline.

import (
    "bytes"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net"
    "net/http"
    neturl "net/url"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/gorilla/websocket"
    "golang.org/x/crypto/ssh"
)


var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Restrict to exact host match for Origin
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		// Parse origin URL
		u, err := neturl.Parse(origin)
		if err != nil {
			return false
		}
		// Allow if origin host (host[:port]) matches request host
		if u.Host == r.Host {
			return true
		}
		// Additionally allow configured ExternalBaseURL host, if provided
		if AppConfig != nil && AppConfig.Server.ExternalBaseURL != "" {
			if eu, err2 := neturl.Parse(AppConfig.Server.ExternalBaseURL); err2 == nil && eu.Host == u.Host {
				return true
			}
		}
		return false
	},
	ReadBufferSize:   4096,
	WriteBufferSize:  4096,
	HandshakeTimeout: 10 * time.Second,
}

type Message struct {
    Type     string    `json:"type"`
    Data     string    `json:"data,omitempty"`
    Host     string    `json:"host,omitempty"`
    Port     int       `json:"port,omitempty"`
    Protocol string    `json:"protocol,omitempty"`
    Username string    `json:"username,omitempty"`
    Password string    `json:"password,omitempty"`
    Cols     int       `json:"cols,omitempty"`
    Rows     int       `json:"rows,omitempty"`
    Encoding string    `json:"encoding,omitempty"`
    Charset  string    `json:"charset,omitempty"`
    Message  string    `json:"message,omitempty"`
    BBSID    string    `json:"bbsId,omitempty"`
    BBSList  []BBSInfo `json:"bbsList,omitempty"`
    Enable   bool      `json:"enable,omitempty"`
}

type BBSInfo struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Host        string `json:"host"`
    Port        int    `json:"port"`
    Protocol    string `json:"protocol"`
    Description string `json:"description"`
    Encoding    string `json:"encoding,omitempty"`
    Location    string `json:"location,omitempty"`
}

// ZmodemHandler abstracts different ZMODEM implementations (e.g., external
// lrzsz vs. potential pure-Go). Only a minimal interface is required.
type ZmodemHandler interface {
	ProcessData(data []byte) ([]byte, bool)
	Cancel()
	Active() bool
}

// Client represents one browser session bridged to a single remote BBS
// connection (telnet or SSH). It owns the ZMODEM lifecycle for that session.
type Client struct {
    ws             *websocket.Conn // WebSocket connection to browser
    telnet         net.Conn        // Telnet connection to BBS
    ssh            *ssh.Client     // SSH client (if using SSH)
    // SSH session and input pipe for writing
    sshSession     *ssh.Session    // SSH session (if using SSH)
    sshIn          io.WriteCloser  // SSH session stdin
    mu             sync.Mutex    // Protects concurrent access
    done           chan bool     // Signals connection closure
    charset        string        // Character set for conversion
    zmodemReceiver ZmodemHandler // Active Zmodem handler
    ansiEnhanced   *ANSIEnhancedProcessor // Enhanced ANSI processor
    // Pre-transfer suppression to avoid displaying binary data
    suppressZmodem bool      // Whether to suppress output
    suppressUntil  time.Time // When suppression expires
    // Telnet binary mode negotiation state
    telnetBinaryTX bool // We WILL transmit binary
    telnetBinaryRX bool // Remote WILL transmit binary

    // Telnet negotiation state
    telnetNAWS     bool // NAWS negotiated (we WILL NAWS)
    telnetTTYPE    bool // TTYPE negotiated (we WILL TTYPE)

    // Terminal dimensions (fixed BBS-friendly sizes)
    termCols int
    termRows int

    // Lightweight cursor tracking for CPR replies
    cursorRow int
    cursorCol int
    cursorSeqBuf []byte

    // ANSI music processor (CSI | sequences)
    music *AnsiMusicProcessor
    // Simple autodetect state to avoid flapping
    // Once we switch from CP437 to a legacy charset, we won't auto-switch again
    // unless explicitly changed by the user.
    // Legacy preamble suppression (buffer until first clear/home)
    preambleActive bool
    preambleBuf    []byte
    preambleStart  time.Time

    // Capture support
    captureOn        bool
    capturePath      string
    forceLegacyFull  bool
}

// Global list of approved BBSes (loaded from both config and bbs.json)
var ApprovedBBSList []BBSInfo

// loadBBSJson removed - now using database from bbs.csv

func main() {
	// Load configuration
	config, err := LoadConfig("config.json")
	if err != nil {
		log.Printf("Warning: Could not load config.json: %v", err)
		log.Println("Using default configuration")
		// Create minimal config
		config = &Config{}
		config.Server.Port = 8080
		AppConfig = config
	}

	// Populate the approved list from bbs.csv
	if err := refreshApprovedBBSList(); err != nil {
		log.Printf("Warning: Could not load approved BBS list: %v", err)
	} else {
		log.Printf("Approved BBS list loaded: %d entries", len(ApprovedBBSList))
	}

	// Setup routes
	setupRoutes(config)

	port := config.Server.Port
	fmt.Printf("Server starting on :%d\n", port)
	// Stateless mode; no registration/auth or manual connections
	fmt.Println("Manual connections: disabled (directory only)")
	if config.Proxy.Enabled {
		if config.Proxy.Type == "tor" {
			fmt.Printf("Tor Proxy: %s:%d (anonymized connections)\n", config.Proxy.Host, config.Proxy.Port)
		} else {
			fmt.Printf("SOCKS5 Proxy: %s:%d\n", config.Proxy.Host, config.Proxy.Port)
		}
	} else {
		fmt.Println("Proxy: disabled (direct connections)")
	}

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

// refreshApprovedBBSList populates the in-memory allowlist from CSV
func refreshApprovedBBSList() error {
    if entries, err := GetBBSDirectoryEntries(); err == nil && len(entries) > 0 {
        list := make([]BBSInfo, 0, len(entries))
        for _, e := range entries {
            // No BBS-specific overrides; let client auto-detect if desired
            enc := e.Encoding
            list = append(list, BBSInfo{
                ID:          e.ID,
                Name:        e.Name,
                Host:        e.Host,
                Port:        e.Port,
                Protocol:    strings.ToLower(e.Protocol),
                Description: e.Description,
                Encoding:    enc,
                Location:    e.Location,
            })
        }
        ApprovedBBSList = list
        return nil
    }
    ApprovedBBSList = []BBSInfo{}
    return nil
}

func setupRoutes(config *Config) {
	// WebSocket handler
	http.HandleFunc("/ws", handleWebSocket)

	// Config endpoint (public)
	http.HandleFunc("/api/config", handleGetConfig)
	http.HandleFunc("/api/defaultBBSList", handleGetDefaultBBSList)

	// BBS Directory endpoints (public read)
	http.HandleFunc("/api/bbs-directory", handleGetBBSDirectory)
	http.HandleFunc("/api/import-bbs-guide", handleImportBBSGuide)

	// 404 for any other /api/* paths
	http.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"success":false,"error":"not_found","path":"%s"}`, r.URL.Path)
	})

	// Static files
	http.Handle("/", http.FileServer(http.Dir("./static")))
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Configure WebSocket timeouts and keepalive (3 minutes)
	conn.SetReadDeadline(time.Now().Add(180 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(180 * time.Second))
		return nil
	})

    // Check for debug mode from environment
    debugMode := os.Getenv("ANSI_DEBUG") == "true"
    
    client := &Client{
        ws:           conn,
        done:         make(chan bool),
        charset:      "CP437",
        ansiEnhanced: NewANSIEnhancedProcessor(debugMode),
        termCols:     80,
        termRows:     25,
        cursorRow:    1,
        cursorCol:    1,
        cursorSeqBuf: make([]byte, 0, 64),
    }
    // Enable raw capture if requested (writes to CAPTURE_FILE or capture.bin)
    if os.Getenv("CAPTURE_RAW") == "true" {
        client.captureOn = true
        client.capturePath = os.Getenv("CAPTURE_FILE")
        if client.capturePath == "" { client.capturePath = "capture.bin" }
        // Truncate on new session if CAPTURE_TRUNCATE=true
        if os.Getenv("CAPTURE_TRUNCATE") == "true" {
            _ = os.WriteFile(client.capturePath, nil, 0644)
        }
    }
    // Music emitter sends a JSON message to the client; keep simple payload
    client.music = NewAnsiMusicProcessor(func(payload string) {
        client.sendJSON(Message{Type: "music", Message: payload})
    })

	// Start ping ticker for keepalive
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-client.done:
				return
			}
		}
	}()

	for {
		var msg Message
		// Reset read deadline on each message (3 minutes)
		conn.SetReadDeadline(time.Now().Add(180 * time.Second))
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket unexpected close: %v", err)
			}
			client.disconnect()
			break
		}

		switch msg.Type {
		case "connect":
			// SECURITY: Always validate connections against curated allowlist
			isApproved := false
			if len(ApprovedBBSList) == 0 {
				// Attempt a lazy refresh if list is empty
				if err := refreshApprovedBBSList(); err != nil {
					log.Printf("SECURITY: failed to refresh approved list: %v", err)
				}
			}
			for _, bbs := range ApprovedBBSList {
				// Case-insensitive host comparison and exact port/protocol match
				if strings.EqualFold(bbs.Host, msg.Host) &&
					bbs.Port == msg.Port &&
					strings.EqualFold(bbs.Protocol, msg.Protocol) {
					isApproved = true
					log.Printf("SECURITY: Approved connection to %s://%s:%d", msg.Protocol, msg.Host, msg.Port)
					break
				}
			}
			if !isApproved {
				// Log security event - attempted unauthorized connection
				log.Printf("SECURITY WARNING: Blocked unauthorized connection attempt to %s://%s:%d",
					msg.Protocol, msg.Host, msg.Port)
				client.sendMessage("error", "Connection blocked: Host not in approved list")
				continue
			}
            if msg.Charset != "" {
                client.setCharsetAndAdjust(msg.Charset)
            }
            if msg.Protocol == "telnet" {
                go client.connectTelnet(msg.Host, msg.Port)
            } else if msg.Protocol == "ssh" {
                go client.connectSSH(msg.Host, msg.Port, msg.Username, msg.Password)
            }
		case "data":
			client.sendToRemote(msg.Data)
    case "resize":
        // Update PTY size for SSH sessions if present
        client.mu.Lock()
        sshSession := client.sshSession
        client.mu.Unlock()
        if sshSession != nil && msg.Cols > 0 && msg.Rows > 0 {
            // Note: WindowChange takes rows, cols order
            _ = sshSession.WindowChange(msg.Rows, msg.Cols)
        }
        // Accept only fixed BBS-friendly sizes for telnet NAWS
        if (msg.Cols == 40 && (msg.Rows == 24 || msg.Rows == 25)) || (msg.Cols == 80 && msg.Rows == 25) || (msg.Cols == 100 && msg.Rows == 31) {
            client.mu.Lock()
            client.termCols = msg.Cols
            client.termRows = msg.Rows
            telnetConn := client.telnet
            telnetNAWS := client.telnetNAWS
            client.mu.Unlock()
            if telnetConn != nil && telnetNAWS {
                client.sendTelnetNAWS()
            }
        }
        case "setCharset":
            client.setCharsetAndAdjust(msg.Charset)
        case "getBBSList":
            client.sendBBSList()
        case "connectToBBS":
            // SECURITY: This message type only uses pre-approved BBS IDs
            log.Printf("SECURITY: BBS connection via ID: %s", msg.BBSID)
            client.connectToBBS(msg.BBSID)
        case "playCapture":
            go client.playCapture()
        case "cancelDownload":
            if client.zmodemReceiver != nil {
                client.zmodemReceiver.Cancel()
            }
        case "disconnect":
            client.disconnect()
            return
        }
	}
}

// sendBBSList sends the current curated BBS list to the browser.
func (c *Client) sendBBSList() {
    msg := Message{
        Type:    "bbsList",
        BBSList: ApprovedBBSList,
    }
    c.sendJSON(msg)
}

// connectToBBS looks up a curated BBS by ID and starts a telnet/SSH connection.
func (c *Client) connectToBBS(bbsID string) {
    for _, bbs := range ApprovedBBSList {
        if bbs.ID == bbsID {
            // Set charset from BBS config if specified
            if bbs.Encoding != "" {
                c.charset = bbs.Encoding
            }
			if bbs.Protocol == "telnet" {
				go c.connectTelnet(bbs.Host, bbs.Port)
			} else if bbs.Protocol == "ssh" {
				go c.connectSSH(bbs.Host, bbs.Port, "", "")
			}
			return
		}
	}
	c.sendMessage("error", fmt.Sprintf("BBS not found: %s", bbsID))
}

// connectTelnet dials a telnet endpoint (optionally via proxy) and starts
// the read loop. A ZMODEM receiver is lazily created for telnet sessions.
func (c *Client) connectTelnet(host string, port int) {
	address := fmt.Sprintf("%s:%d", host, port)
	log.Printf("Connecting to telnet://%s", address)

	// Use proxy if configured
	conn, err := DialWithProxy("tcp", address)
	if err != nil {
		c.sendMessage("error", err.Error())
		return
	}

	c.mu.Lock()
	c.telnet = conn
	// Initialize Zmodem receiver (lrzsz-based) for telnet connections
	c.zmodemReceiver = NewLrzszReceiver(c)
	// Get charset for capture metadata
	charset := c.charset
	c.mu.Unlock()

	// Start capture for debugging
	if captureFile, err := captureManager.StartCapture(host, port, "telnet", charset); err == nil {
		log.Printf("Started capture: %s", captureFile)
	} else {
		log.Printf("Failed to start capture: %v", err)
	}

	c.sendMessage("connected", fmt.Sprintf("Connected to %s", address))

	// Handle telnet data
	go c.readTelnet()
}

// readTelnet pumps data from the telnet connection to the browser, handling
// telnet negotiations, CP437 conversion, ANSI processing, and ZMODEM detection.
func (c *Client) readTelnet() {
    buffer := make([]byte, 8192)

	for {
		c.mu.Lock()
		conn := c.telnet
		c.mu.Unlock()

		if conn == nil {
			return
		}

		// Set read timeout to detect stale connections
		conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		n, err := conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				log.Printf("Telnet connection closed by remote host")
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("Telnet read timeout - connection may be stale")
			} else {
				log.Printf("Telnet read error: %v", err)
			}
			// Stop capture on disconnect
			if err := captureManager.StopCapture(); err != nil {
				log.Printf("Failed to stop capture: %v", err)
			}
			c.sendJSON(Message{Type: "disconnected"})
			c.disconnect()
			return
		}

        if n > 0 {
            // Check for Zmodem in raw data FIRST (before telnet processing)
            rawData := buffer[:n]

            // Write raw data to capture for debugging
            if err := captureManager.WriteCapture(rawData); err != nil {
                log.Printf("Capture write error: %v", err)
            }

            // Debug logging removed

			// Pre-suppress terminal output on first ZMODEM signature before receiver activates
			if c.hasZmodemSignature(rawData) && (c.zmodemReceiver == nil || !c.zmodemReceiver.Active()) {
				if !c.suppressZmodem {
					c.suppressZmodem = true
					c.suppressUntil = time.Now().Add(5 * time.Second)
				}
			}

			// Feed RAW data to Zmodem receiver if available (not cleaned!)
            var cleanData []byte
            if c.zmodemReceiver != nil {
                if remaining, consumed := c.zmodemReceiver.ProcessData(rawData); consumed {
					// During transfer, optionally show minimal status to terminal or suppress
					// Suppress transfer bytes from terminal output
					if len(remaining) > 0 {
						// Any non-zmodem remainder can still be shown
						cleanData = remaining
					} else {
						cleanData = nil
					}
				} else {
					// Not consumed - process telnet normally
					cleanData = c.processTelnetData(rawData)
				}
				// If receiver is active, suppress all screen output to avoid binary noise
				if c.zmodemReceiver.Active() {
					cleanData = nil
				}
			} else {
				// No Zmodem receiver or not processing - clean telnet data normally
				cleanData = c.processTelnetData(rawData)
			}

			// Check for Zmodem signatures and log them (once per transfer)
			if c.hasZmodemSignature(rawData) && (c.zmodemReceiver == nil || !c.zmodemReceiver.Active()) {
				// Log detection once per transfer to avoid spam
				if !c.suppressZmodem || time.Since(c.suppressUntil) > 0 {
					log.Println("Detected Zmodem signature in data stream")
				}
			}

			// Clear pre-suppression if it expired or transfer became active
			if c.suppressZmodem && (time.Now().After(c.suppressUntil) || (c.zmodemReceiver != nil && c.zmodemReceiver.Active())) {
				c.suppressZmodem = false
			}

            // Only send to terminal if not in active ZMODEM transfer and not in pre-suppression window
            if len(cleanData) > 0 && (c.zmodemReceiver == nil || !c.zmodemReceiver.Active()) && !c.suppressZmodem {
                // Optionally capture the stream for testing
                if c.captureOn {
                    if f, err := os.OpenFile(c.capturePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
                        _, _ = f.Write(cleanData)
                        _ = f.Close()
                    }
                }
                // ANSI Music: detect and emit events, suppressing music sequences
                if c.music != nil {
                    if remaining, consumed := c.music.Process(cleanData); consumed {
                        cleanData = remaining
                    }
                }
                // Respond to terminal queries if enabled
                if os.Getenv("TERM_ANSWERS") == "true" {
                    c.handleTerminalQueries(cleanData)
                }
                // Process ANSI sequences with enhanced processor (not for legacy charsets)
                processedData := cleanData
                if c.ansiEnhanced != nil && os.Getenv("ANSI_NORMALIZE") != "false" {
                    cs := strings.ToUpper(c.charset)
                    if cs != "PETSCIIU" && cs != "PETSCIIL" && cs != "ATASCII" {
                        processedData = c.ansiEnhanced.ProcessANSIData(cleanData)
                    }
                }
                // Optional hex dump for diagnostics
                if hexLoggingEnabled() {
                    c.debugHexDump("TELNET->CLIENT", processedData, 256)
                }
                
                // Legacy preamble suppression: buffer until clear/home or timeout
                csUp := strings.ToUpper(c.charset)
                if (csUp == "PETSCIIU" || csUp == "PETSCIIL" || csUp == "ATASCII") && c.preambleActive {
                    c.preambleBuf = append(c.preambleBuf, processedData...)
                    if c.shouldFlushLegacyPreamble(processedData) {
                        c.preambleActive = false
                        processedData = c.preambleBuf
                        // Reset buffer
                        c.preambleBuf = c.preambleBuf[:0]
                    } else {
                        // Skip sending this chunk
                        continue
                    }
                }

                // No auto-detect/switching; rely on explicit charset selection

                // Convert charset-specific bytes to UTF-8 for browser rendering
                var outputData []byte
                bypassUsed := false
                switch c.charset {
                case "CP437":
                    utf8String := ConvertCP437ToUTF8Enhanced(processedData)
                    outputData = []byte(utf8String)
                case "PETSCIIU", "PETSCIIL", "ATASCII":
                    // Smart ASCII pass-through: check BEFORE control translation
                    // to avoid bypassing when there are PETSCII control codes
                    if !c.forceLegacyFull && shouldBypassLegacyMapping(processedData) {
                        // Even in bypass mode, translate control codes to ANSI
                        processedData = c.translateLegacyControls(processedData)
                        processedData = normalizeCSISGRAny(processedData)
                        outputData = processedData
                        bypassUsed = true
                    } else {
                        // Phase 2: translate PETSCII/ATASCII control bytes into ANSI
                        processedData = c.translateLegacyControls(processedData)
                        processedData = normalizeCSISGRAny(processedData)
                        if c.charset == "PETSCIIU" {
                            utf8String := ConvertPETSCIIUToUTF8(processedData)
                            outputData = []byte(utf8String)
                        } else if c.charset == "PETSCIIL" {
                            utf8String := ConvertPETSCIILToUTF8(processedData)
                            outputData = []byte(utf8String)
                        } else {
                            utf8String := ConvertATASCIIToUTF8(processedData)
                            outputData = []byte(utf8String)
                        }
                    }
                default:
                    outputData = processedData
                }

                if hexLoggingEnabled() {
                    c.mu.Lock()
                    cols := c.termCols
                    rows := c.termRows
                    cs := c.charset
                    c.mu.Unlock()
                    log.Printf("MODE: charset=%s cols=%d rows=%d bypass=%v", cs, cols, rows, bypassUsed)
                    // Log the final bytes as the browser will see them
                    logBytes := outputData
                    if bytes.Contains(logBytes, []byte{0x1B, '['}) {
                        logBytes = normalizeCSISGRAny(append([]byte(nil), logBytes...))
                    }
                    preview := string(logBytes)
                    if len(preview) > 120 {
                        preview = preview[:120] + "…"
                    }
                    log.Printf("TEXT (final) %s: %q", cs, preview)
                }

                // Final output guard: normalize any CSI SGR with uppercase 'M' or lowercase 'j'
                if bytes.Contains(outputData, []byte{0x1B, '['}) {
                    outputData = normalizeCSISGRAny(outputData)
                }
                encoded := base64.StdEncoding.EncodeToString(outputData)
                c.sendJSON(Message{
                    Type:     "data",
                    Data:     encoded,
                    Encoding: "base64",
                })

                // Update our lightweight cursor tracker if enabled
                if os.Getenv("CURSOR_TRACK") == "true" {
                    c.updateCursorFrom(processedData)
                }
            }
		}
	}
}

// playCapture replays a previously captured clean data stream
// through the same processing pipeline used for telnet data.
// It supports both CAPTURE_FILE env var and enhanced captures with metadata.
func (c *Client) playCapture() {
    path := os.Getenv("CAPTURE_FILE")

    // If no CAPTURE_FILE, try to find the latest capture in captures/ dir
    if path == "" {
        // Try to find the latest capture file
        files, err := os.ReadDir("captures")
        if err == nil && len(files) > 0 {
            var latestCapture string
            var latestTime time.Time
            for _, file := range files {
                if strings.HasSuffix(file.Name(), ".bin") {
                    info, err := file.Info()
                    if err == nil && info.ModTime().After(latestTime) {
                        latestTime = info.ModTime()
                        latestCapture = filepath.Join("captures", file.Name())
                    }
                }
            }
            if latestCapture != "" {
                path = latestCapture
                log.Printf("Playing latest capture: %s", path)

                // Try to load metadata to set charset
                metaPath := strings.TrimSuffix(path, ".bin") + ".json"
                if metaData, err := os.ReadFile(metaPath); err == nil {
                    var meta struct {
                        Charset string `json:"charset"`
                    }
                    if err := json.Unmarshal(metaData, &meta); err == nil && meta.Charset != "" {
                        c.charset = meta.Charset
                        log.Printf("Set charset from metadata: %s", meta.Charset)
                    }
                }
            }
        }

        // Fallback to capture.bin if no captures found
        if path == "" {
            path = "capture.bin"
        }
    }

    f, err := os.Open(path)
    if err != nil {
        c.sendMessage("error", fmt.Sprintf("Failed to open capture: %v", err))
        return
    }
    defer f.Close()

    log.Printf("Playing capture from: %s (charset: %s)", path, c.charset)

    prevForce := c.forceLegacyFull
    c.forceLegacyFull = true
    defer func() { c.forceLegacyFull = prevForce }()

    buf := make([]byte, 1024)
    for {
        n, err := f.Read(buf)
        if n > 0 {
            chunk := make([]byte, n)
            copy(chunk, buf[:n])
            c.processAndSendClean(chunk)
            time.Sleep(20 * time.Millisecond)
        }
        if err == io.EOF {
            break
        }
        if err != nil {
            break
        }
    }

    log.Printf("Capture replay complete")
}

// processAndSendClean processes a "clean" (telnet-processed) byte slice through the
// same display pipeline and emits it to the browser.
func (c *Client) processAndSendClean(cleanData []byte) {
    if len(cleanData) == 0 {
        return
    }
    // ANSI music (optional)
    if c.music != nil {
        if remaining, consumed := c.music.Process(cleanData); consumed {
            cleanData = remaining
        }
    }
    if os.Getenv("TERM_ANSWERS") == "true" {
        c.handleTerminalQueries(cleanData)
    }
    processedData := cleanData
    if c.ansiEnhanced != nil && os.Getenv("ANSI_NORMALIZE") != "false" {
        cs := strings.ToUpper(c.charset)
        if cs != "PETSCIIU" && cs != "PETSCIIL" && cs != "ATASCII" {
            processedData = c.ansiEnhanced.ProcessANSIData(cleanData)
        }
    }
    // Legacy preamble suppression
    csUp := strings.ToUpper(c.charset)
    if (csUp == "PETSCIIU" || csUp == "PETSCIIL" || csUp == "ATASCII") && c.preambleActive {
        c.preambleBuf = append(c.preambleBuf, processedData...)
        if c.shouldFlushLegacyPreamble(processedData) {
            c.preambleActive = false
            processedData = c.preambleBuf
            c.preambleBuf = c.preambleBuf[:0]
        } else {
            return
        }
    }
    var outputData []byte
    bypassUsed := false
    switch c.charset {
    case "CP437":
        utf8String := ConvertCP437ToUTF8Enhanced(processedData)
        outputData = []byte(utf8String)
    case "PETSCIIU", "PETSCIIL", "ATASCII":
        if !c.forceLegacyFull && shouldBypassLegacyMapping(processedData) {
            processedData = c.translateLegacyControls(processedData)
            processedData = normalizeCSISGRAny(processedData)
            outputData = processedData
            bypassUsed = true
        } else {
            processedData = c.translateLegacyControls(processedData)
            processedData = normalizeCSISGRAny(processedData)
            if c.charset == "PETSCIIU" {
                utf8String := ConvertPETSCIIUToUTF8(processedData)
                outputData = []byte(utf8String)
            } else if c.charset == "PETSCIIL" {
                utf8String := ConvertPETSCIILToUTF8(processedData)
                outputData = []byte(utf8String)
            } else {
                utf8String := ConvertATASCIIToUTF8(processedData)
                outputData = []byte(utf8String)
            }
        }
    default:
        outputData = processedData
    }
    if os.Getenv("HEX_DUMP") == "true" {
        c.mu.Lock()
        cols := c.termCols
        rows := c.termRows
        cs := c.charset
        c.mu.Unlock()
        log.Printf("MODE: charset=%s cols=%d rows=%d bypass=%v", cs, cols, rows, bypassUsed)
        logBytes := outputData
        if bytes.Contains(logBytes, []byte{0x1B, '['}) {
            logBytes = normalizeCSISGRAny(append([]byte(nil), logBytes...))
        }
        preview := string(logBytes)
        if len(preview) > 120 { preview = preview[:120] + "…" }
        log.Printf("TEXT (final) %s: %q", cs, preview)
    }
    c.sendJSON(Message{
        Type:     "data",
        Data:     base64.StdEncoding.EncodeToString(outputData),
        Encoding: "base64",
    })
}

// hasZmodemSignature heuristically detects common ZMODEM start sequences.
func (c *Client) hasZmodemSignature(data []byte) bool {
	// Check for common Zmodem start sequences
	patterns := [][]byte{
		{0x2A, 0x2A, 0x18, 0x42, 0x30, 0x30}, // **\x18B00
		{0x2A, 0x18, 0x43},                   // *\x18C
		[]byte("rz\r"),                       // rz command
	}

	for _, pattern := range patterns {
		if bytes.Contains(data, pattern) {
			return true
		}
	}
	return false
}

// processTelnetData filters and responds to telnet negotiations and returns
// a cleaned stream suitable for terminal rendering and ZMODEM processing.
func (c *Client) processTelnetData(data []byte) []byte {
    const (
        IAC  = 255
        DONT = 254
        DO   = 253
        WONT = 252
        WILL = 251
        SB   = 250
        SE   = 240
    )

    // Telnet options
    const (
        TELOPT_TTYPE = 24
        TELOPT_NAWS  = 31
    )
    const (
        TELQUAL_IS   = 0
        TELQUAL_SEND = 1
    )

	var clean []byte
	var response []byte
	i := 0

	for i < len(data) {
        if data[i] == IAC {
            if i+1 < len(data) {
                if data[i+1] == IAC {
                    // Escaped IAC
                    clean = append(clean, IAC)
                    i += 2
                } else if i+2 < len(data) && data[i+1] >= SE && data[i+1] <= DONT {
                    cmd := data[i+1]
                    option := data[i+2]

                    // Respond to telnet negotiations
                    // Accept BINARY transmission (option 0) for reliable ZMODEM transfers
                    const BINARY = 0
                    if cmd == DO {
                        if option == BINARY {
                            response = append(response, IAC, WILL, option)
                            c.telnetBinaryTX = true
                        } else if option == TELOPT_NAWS {
                            response = append(response, IAC, WILL, option)
                            c.telnetNAWS = true
                            // Immediately send current fixed NAWS
                            // Will be written after loop
                            response = append(response, c.buildNAWSSB()...)
                        } else if option == TELOPT_TTYPE {
                            response = append(response, IAC, WILL, option)
                            c.telnetTTYPE = true
                        } else {
                            response = append(response, IAC, WONT, option)
                        }
                    } else if cmd == DONT {
                        // Acknowledge with WONT
                        response = append(response, IAC, WONT, option)
                        if option == BINARY {
                            c.telnetBinaryTX = false
                        }
                        if option == TELOPT_NAWS {
                            c.telnetNAWS = false
                        }
                    } else if cmd == WILL {
                        if option == BINARY {
                            response = append(response, IAC, DO, option)
                            c.telnetBinaryRX = true
                        } else {
                            response = append(response, IAC, DONT, option)
                        }
                    } else if cmd == WONT {
                        // Acknowledge with DONT
                        response = append(response, IAC, DONT, option)
                        if option == BINARY {
                            c.telnetBinaryRX = false
                        }
                    }
                    i += 3
                } else if data[i+1] == SB {
                    // Handle subnegotiation
                    j := i + 2
                    if j >= len(data) {
                        i += 2
                        continue
                    }
                    opt := data[j]
                    j++
                    // Capture until IAC SE
                    sbStart := j
                    for j < len(data)-1 {
                        if data[j] == IAC && data[j+1] == SE {
                            sb := data[sbStart:j]
                            // Process TTYPE SEND
                            if opt == TELOPT_TTYPE {
                                if len(sb) >= 1 && sb[0] == TELQUAL_SEND {
                                    // Reply: IAC SB TTYPE IS <type> IAC SE
                                    termType := "ansi"
                                    // Advertise PETSCII/ATASCII when selected
                                    switch strings.ToUpper(c.charset) {
                                    case "PETSCIIU", "PETSCIIL":
                                        termType = "PETSCII"
                                    case "ATASCII":
                                        termType = "ATASCII"
                                    }
                                    ttype := []byte(termType)
                                    resp := []byte{IAC, SB, TELOPT_TTYPE, TELQUAL_IS}
                                    resp = append(resp, ttype...)
                                    resp = append(resp, IAC, SE)
                                    response = append(response, resp...)
                                }
                            }
                            i = j + 2
                            break
                        }
                        j++
                    }
                    if j >= len(data)-1 {
                        // Unterminated SB, drop remainder
                        i = j
                    }
                } else {
                    i += 2
                }
            } else {
                i++
			}
		} else {
			clean = append(clean, data[i])
			i++
		}
	}

    // Send telnet negotiation responses
    if len(response) > 0 {
        c.mu.Lock()
        conn := c.telnet
        c.mu.Unlock()
        if conn != nil {
            _, _ = conn.Write(response)
        }
    }

    return clean
}

// detectLegacyCharset returns a suggested charset ("PETSCIIU" or "ATASCII") based on
// simple byte pattern heuristics in the provided data. Empty string if no suggestion.
// shouldBypassLegacyMapping returns true when the data chunk is mostly printable ASCII
// and contains no PETSCII/ATASCII legacy control bytes, indicating this should be
// displayed as plain text (e.g., busy messages, MOTD lines) even in legacy sessions.
func shouldBypassLegacyMapping(data []byte) bool {
    if len(data) == 0 {
        return false
    }
    printable := 0
    nonAscii := 0
    for _, b := range data {
        if (b >= 0x20 && b <= 0x7E) || b == '\r' || b == '\n' || b == '\t' {
            printable++
        } else if b >= 0x80 {
            // Any byte >= 0x80 is likely PETSCII graphics or control
            nonAscii++
        }
    }
    // Only bypass if it's mostly printable ASCII with no high bytes
    // This ensures PETSCII graphics (0x80-0xFF) always get converted
    return nonAscii == 0 && printable*100/len(data) >= 85
}

// joinANSIChunks concatenates carry+chunk and returns (emit, rest) where emit contains
// only complete data up to but not including any trailing incomplete ANSI escape
// sequences (CSI/OSC/DCS/incomplete ESC). The rest should be carried to the next call.
func joinANSIChunks(carry, chunk []byte) ([]byte, []byte) {
    if len(carry) == 0 {
        // Fast path: scan chunk only; if no incomplete sequences, emit as-is
        cut := completeAnsiCutIndex(chunk)
        if cut == len(chunk) {
            return chunk, nil
        }
        // Carry tail
        rest := make([]byte, len(chunk[cut:]))
        copy(rest, chunk[cut:])
        emit := make([]byte, cut)
        copy(emit, chunk[:cut])
        return emit, rest
    }
    buf := make([]byte, 0, len(carry)+len(chunk))
    buf = append(buf, carry...)
    buf = append(buf, chunk...)
    cut := completeAnsiCutIndex(buf)
    if cut == len(buf) {
        return buf, nil
    }
    emit := make([]byte, cut)
    copy(emit, buf[:cut])
    rest := make([]byte, len(buf[cut:]))
    copy(rest, buf[cut:])
    return emit, rest
}

// completeAnsiCutIndex returns the index up to which data contains only complete
// ANSI escape sequences. If an incomplete sequence is found at the end, returns
// the start index of that sequence.
func completeAnsiCutIndex(buf []byte) int {
    i := 0
    for i < len(buf) {
        b := buf[i]
        if b != 0x1B { // ESC
            i++
            continue
        }
        // Need at least one more byte to classify
        if i+1 >= len(buf) {
            return i
        }
        n := buf[i+1]
        switch n {
        case '[': // CSI: ESC [ params final
            j := i + 2
            for j < len(buf) {
                c := buf[j]
                if c >= 0x40 && c <= 0x7E { // final
                    j++
                    i = j
                    goto next
                }
                j++
            }
            return i // incomplete CSI
        case ']': // OSC: ESC ] ... (BEL or ESC \ terminator)
            j := i + 2
            for j < len(buf) {
                if buf[j] == 0x07 { // BEL
                    j++
                    i = j
                    goto next
                }
                if j+1 < len(buf) && buf[j] == 0x1B && buf[j+1] == '\\' {
                    j += 2
                    i = j
                    goto next
                }
                j++
            }
            return i // incomplete OSC
        case 'P': // DCS: ESC P ... ESC \
            j := i + 2
            for j < len(buf) {
                if j+1 < len(buf) && buf[j] == 0x1B && buf[j+1] == '\\' {
                    j += 2
                    i = j
                    goto next
                }
                j++
            }
            return i // incomplete DCS
        case '7', '8', 'c', 'D', 'M', 'E':
            // Single-char ESC sequences
            i += 2
        default:
            if n >= 0x40 && n <= 0x7F {
                // Two-char ESC <final>
                i += 2
            } else {
                // Unknown/incomplete, carry ESC and beyond
                return i
            }
        }
    next:
    }
    return i
}

// normalizeCSISGRCase fixes sequences like ESC [ 37 M -> ESC [ 37 m (SGR)
// when the parameters are digits/semicolons only. This guards against
// accidental uppercase 'M' which ANSI interprets as DL (Delete Line).
// normalizeCSISGRAny converts CSI sequences that look like SGR but end with 'M'
// into proper 'm' (7-bit ESC[ form only). Do not touch 0x9B here since in
// legacy charsets (PETSCII) 0x9B is also a color code before translation.
func normalizeCSISGRAny(in []byte) []byte {
    if len(in) < 3 {
        return in
    }
    out := make([]byte, 0, len(in))
    for i := 0; i < len(in); i++ {
        b := in[i]
        // 7-bit CSI form
        if b == 0x1B && i+1 < len(in) && in[i+1] == '[' {
            out = append(out, 0x1B, '[')
            // Collect params
            j := i + 2
            for j < len(in) {
                c := in[j]
                if c >= 0x40 && c <= 0x7E {
                    // Final byte
                    // Check if this is a private sequence (starts with ?)
                    isPrivate := false
                    if i+2 < len(in) && in[i+2] == '?' {
                        isPrivate = true
                    }

                    // Only normalize non-private sequences
                    if !isPrivate {
                        if c == 'M' { // Uppercase M
                            // Verify params are digits/semicolons only
                            valid := true
                            for k := i + 2; k < j; k++ {
                                d := in[k]
                                if !(d == ';' || (d >= '0' && d <= '9')) {
                                    valid = false
                                    break
                                }
                            }
                            if valid {
                                c = 'm'
                            }
                        } else if c == 'j' { // lowercase j -> J for ED
                            // Treat as clear screen variants when digits/semicolons
                            valid := true
                            for k := i + 2; k < j; k++ {
                                d := in[k]
                                if !(d == ';' || (d >= '0' && d <= '9')) {
                                    valid = false
                                    break
                                }
                            }
                            if valid {
                                c = 'J'
                            }
                        }
                    }
                    out = append(out, in[i+2:j]...)
                    out = append(out, c)
                    i = j
                    goto next
                }
                j++
            }
            // Incomplete CSI, copy the rest as-is and break
            out = append(out, in[i+2:]...)
            break
        }
        // Note: do not normalize 0x9B here.
        out = append(out, b)
    next:
    }
    return out
}

// buildNAWSSB constructs a NAWS SB with current fixed cols/rows
func (c *Client) buildNAWSSB() []byte {
    const (
        IAC  = 255
        SB   = 250
        SE   = 240
        TELOPT_NAWS = 31
    )
    c.mu.Lock()
    cols := c.termCols
    rows := c.termRows
    c.mu.Unlock()
    if cols == 0 || rows == 0 {
        cols = 80
        rows = 25
    }
    // 16-bit big-endian values
    widthHi := byte((cols >> 8) & 0xFF)
    widthLo := byte(cols & 0xFF)
    heightHi := byte((rows >> 8) & 0xFF)
    heightLo := byte(rows & 0xFF)
    return []byte{IAC, SB, TELOPT_NAWS, widthHi, widthLo, heightHi, heightLo, IAC, SE}
}

// sendTelnetNAWS sends the current fixed NAWS to the telnet peer
func (c *Client) sendTelnetNAWS() {
    sb := c.buildNAWSSB()
    c.mu.Lock()
    conn := c.telnet
    c.mu.Unlock()
    if conn != nil {
        _, _ = conn.Write(sb)
    }
}

// setCharsetAndAdjust sets charset and adjusts NAWS to a reasonable size.
// PETSCII/ATASCII use 40x25; ANSI/CP437/UTF-8 use 80x25.
func (c *Client) setCharsetAndAdjust(cs string) {
    c.mu.Lock()
    oldCols := c.termCols
    oldRows := c.termRows
    c.charset = cs
    switch strings.ToUpper(cs) {
    case "PETSCIIU", "PETSCIIL", "ATASCII":
        c.termCols = 40
        c.termRows = 25
        // Enable preamble suppression for legacy charsets
        c.preambleActive = true
        if cap(c.preambleBuf) < 8192 {
            c.preambleBuf = make([]byte, 0, 8192)
        } else {
            c.preambleBuf = c.preambleBuf[:0]
        }
        c.preambleStart = time.Now()
    default:
        if c.termCols < 80 {
            c.termCols = 80
        }
        if c.termRows < 25 {
            c.termRows = 25
        }
        // Disable preamble in ANSI/CP437
        c.preambleActive = false
        c.preambleBuf = c.preambleBuf[:0]
    }
    telnetConn := c.telnet
    telnetNAWS := c.telnetNAWS
    c.mu.Unlock()
    if telnetConn != nil && telnetNAWS && (oldCols != c.termCols || oldRows != c.termRows) {
        c.sendTelnetNAWS()
    }
}

// shouldFlushLegacyPreamble decides when to flush buffered legacy preamble.
// Flush triggers on PETSCII clear (0x93) or home (0x13), ATASCII FF (0x0C),
// or a short timeout/size threshold.
func (c *Client) shouldFlushLegacyPreamble(chunk []byte) bool {
    if len(chunk) == 0 {
        return false
    }
    cs := strings.ToUpper(c.charset)
    // PETSCII: clear/home
    if cs == "PETSCIIU" || cs == "PETSCIIL" {
        for _, b := range chunk {
            if b == 0x93 || b == 0x13 { // CLR or HOME
                return true
            }
        }
    }
    // ATASCII: form feed (clear)
    if cs == "ATASCII" {
        for _, b := range chunk {
            if b == 0x0C { // FF clear
                return true
            }
        }
    }
    // Fallbacks: time/size
    if time.Since(c.preambleStart) > 1500*time.Millisecond {
        return true
    }
    if len(c.preambleBuf)+len(chunk) > 8192 {
        return true
    }
    return false
}

// handleTerminalQueries detects DA/CPR requests in the data stream and replies
// with conservative answers suitable for BBS detection.
func (c *Client) handleTerminalQueries(data []byte) {
    // Patterns to detect:
    //  - ESC [ 6 n (CPR request)
    //  - ESC [ c or ESC [ 0 c (Primary DA request)
    //  - ESC Z (DECID)
    for i := 0; i < len(data); i++ {
        if data[i] != 0x1B { // ESC
            continue
        }
        // Check for CSI sequences
        if i+2 < len(data) && data[i+1] == '[' {
            // Find final byte or stop after a few bytes
            j := i + 2
            // Collect parameters up to a small cap
            for j < len(data) && j-i < 16 {
                b := data[j]
                if b >= 0x40 && b <= 0x7E { // final byte
                    // CPR: ESC [ 6 n
                    if b == 'n' {
                        // DSR/CPR requests
                        // ESC[6n -> Report cursor position
                        if bytes.Equal(data[i:j+1], []byte{0x1B, '[', '6', 'n'}) {
                            // Report tracked cursor position (only if CURSOR_TRACK is enabled)
                            if os.Getenv("CURSOR_TRACK") == "true" {
                                c.mu.Lock()
                                row := c.cursorRow
                                col := c.cursorCol
                                c.mu.Unlock()
                                if row <= 0 { row = 1 }
                                if col <= 0 { col = 1 }
                                rsp := fmt.Sprintf("\x1b[%d;%dR", row, col)
                                log.Printf("CPR requested; replying %d;%d", row, col)
                                c.sendTelnet([]byte(rsp))
                            } else if os.Getenv("CPR_REPLY") == "true" {
                                // Optional: reply 1;1 if explicitly enabled
                                log.Printf("CPR requested; replying 1;1")
                                c.sendTelnet([]byte{0x1B, '[', '1', ';', '1', 'R'})
                            } else {
                                log.Printf("CPR requested; suppressed")
                            }
                        }
                        // ESC[5n -> Device Status Report (ready); reply ESC[0n
                        if bytes.Equal(data[i:j+1], []byte{0x1B, '[', '5', 'n'}) {
                            log.Printf("DSR(5n) requested; replying 0n")
                            c.sendTelnet([]byte{0x1B, '[', '0', 'n'})
                        }
                    }
                    // DA: ESC [ c or ESC [ 0 c
                    if b == 'c' {
                        // Reply VT102: ESC[?6c
                        c.sendTelnet([]byte{0x1B, '[', '?', '6', 'c'})
                    }
                    break
                }
                j++
            }
            i = j
            continue
        }
        // DECID: ESC Z
        if i+1 < len(data) && data[i+1] == 'Z' {
            // Respond with VT102 DA as well
            c.sendTelnet([]byte{0x1B, '[', '?', '6', 'c'})
            i++
            continue
        }
    }
}

// sendTelnet writes raw bytes to the telnet connection if present
func (c *Client) sendTelnet(b []byte) {
    c.mu.Lock()
    conn := c.telnet
    c.mu.Unlock()
    if conn != nil && len(b) > 0 {
        _, _ = conn.Write(b)
    }
}

// debugHexDump logs up to max bytes of data with a simple hex+ASCII view
// hexLoggingEnabled returns true if HEX_DUMP environment variable is set
func hexLoggingEnabled() bool {
    return os.Getenv("HEX_DUMP") == "true"
}

func (c *Client) debugHexDump(label string, data []byte, max int) {
    if len(data) == 0 {
        return
    }
    if max <= 0 || max > len(data) {
        max = len(data)
    }
    const per = 16
    log.Printf("HEX %s: %d bytes (showing %d)", label, len(data), max)
    for off := 0; off < max; off += per {
        end := off + per
        if end > max {
            end = max
        }
        // hex bytes
        hex := make([]byte, 0, (end-off)*3)
        ascii := make([]byte, 0, end-off)
        for i := off; i < end; i++ {
            b := data[i]
            hex = append(hex, fmt.Sprintf("%02x ", b)...)
            if b >= 32 && b <= 126 {
                ascii = append(ascii, b)
            } else {
                ascii = append(ascii, '.')
            }
        }
        log.Printf("%04x: %-48s |%s|", off, string(hex), string(ascii))
    }
}

// updateCursorFrom parses a subset of ANSI to track cursor position
func (c *Client) updateCursorFrom(data []byte) {
    c.mu.Lock()
    cols := c.termCols
    rows := c.termRows
    row := c.cursorRow
    col := c.cursorCol
    seq := append(c.cursorSeqBuf[:0], c.cursorSeqBuf...)
    c.mu.Unlock()

    // Helper to clamp
    clamp := func() {
        if cols <= 0 { cols = 80 }
        if rows <= 0 { rows = 25 }
        if row < 1 { row = 1 }
        if col < 1 { col = 1 }
        if row > rows { row = rows }
        if col > cols { col = cols }
    }

    // Process stream with any leftover sequence start
    buf := append(seq, data...)
    i := 0
    for i < len(buf) {
        b := buf[i]
        switch b {
        case 0x0D: // CR
            col = 1
            i++
        case 0x0A: // LF
            row++
            if row > rows { row = rows }
            i++
        case 0x1B: // ESC
            if i+1 >= len(buf) {
                // Incomplete
                goto done
            }
            if buf[i+1] == '[' { // CSI
                // Find final byte
                j := i + 2
                for j < len(buf) {
                    fb := buf[j]
                    if fb >= 0x40 && fb <= 0x7E {
                        // Parse parameters
                        params := string(buf[i+2 : j])
                        // Split by ';'
                        p := []int{}
                        if len(params) > 0 {
                            parts := strings.Split(params, ";")
                            for _, s := range parts {
                                if s == "" { s = "0" }
                                if n, err := strconv.Atoi(s); err == nil { p = append(p, n) }
                            }
                        }
                        // Final
                        switch fb {
                        case 'A': // CUU
                            n := 1
                            if len(p) >= 1 && p[0] > 0 { n = p[0] }
                            row -= n
                        case 'B': // CUD
                            n := 1
                            if len(p) >= 1 && p[0] > 0 { n = p[0] }
                            row += n
                        case 'C': // CUF
                            n := 1
                            if len(p) >= 1 && p[0] > 0 { n = p[0] }
                            col += n
                        case 'D': // CUB
                            n := 1
                            if len(p) >= 1 && p[0] > 0 { n = p[0] }
                            col -= n
                        case 'H', 'f': // CUP/HVP
                            r := 1
                            c2 := 1
                            if len(p) >= 1 && p[0] > 0 { r = p[0] }
                            if len(p) >= 2 && p[1] > 0 { c2 = p[1] }
                            row = r
                            col = c2
                        case 'J': // ED (ignore position change)
                            // no-op
                        case 'K': // EL
                            // no-op
                        }
                        clamp()
                        i = j + 1
                        goto next
                    }
                    j++
                }
                // Incomplete CSI
                goto done
            } else {
                // Unsupported ESC sequence start; treat as incomplete
                goto done
            }
        default:
            // Printable?
            if b >= 0x20 {
                col++
                if col > cols { col = cols }
            }
            i++
        }
    next:
    }
done:
    // Save leftovers
    c.mu.Lock()
    c.cursorRow = row
    c.cursorCol = col
    c.cursorSeqBuf = c.cursorSeqBuf[:0]
    if i < len(buf) {
        c.cursorSeqBuf = append(c.cursorSeqBuf, buf[i:]...)
    }
    c.mu.Unlock()
}

func (c *Client) connectSSH(host string, port int, username, password string) {
	address := fmt.Sprintf("%s:%d", host, port)
	log.Printf("Connecting to ssh://%s@%s", username, address)

	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	// Use proxy if configured
	conn, err := DialWithProxy("tcp", address)
	if err != nil {
		c.sendMessage("error", fmt.Sprintf("Proxy connection failed: %v", err))
		return
	}

	// Create SSH connection over the proxy connection
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		conn.Close()
		c.sendMessage("error", err.Error())
		return
	}

	client := ssh.NewClient(sshConn, chans, reqs)

    session, err := client.NewSession()
    if err != nil {
        c.sendMessage("error", err.Error())
        client.Close()
        return
    }

	// Request pseudo terminal
	if err := session.RequestPty("xterm-256color", 25, 80, ssh.TerminalModes{}); err != nil {
		c.sendMessage("error", err.Error())
		session.Close()
		client.Close()
		return
	}

    // Set up stdin pipe before starting shell
    in, err := session.StdinPipe()
    if err != nil {
        c.sendMessage("error", err.Error())
        session.Close()
        client.Close()
        return
    }

    // Start shell
    if err := session.Shell(); err != nil {
        c.sendMessage("error", err.Error())
        session.Close()
        client.Close()
        return
    }

    c.mu.Lock()
    c.ssh = client
    c.sshSession = session
    c.sshIn = in
    c.mu.Unlock()

	c.sendMessage("connected", fmt.Sprintf("Connected to %s", address))

	// Handle SSH I/O
	go c.handleSSHSession(session)
}

func (c *Client) handleSSHSession(session *ssh.Session) {
    defer session.Close()

    stdout, err := session.StdoutPipe()
    if err != nil {
        c.sendMessage("error", err.Error())
        return
    }

    buffer := make([]byte, 8192)
    for {
        n, err := stdout.Read(buffer)
        if err != nil {
            c.sendJSON(Message{Type: "disconnected"})
            c.disconnect()
            return
        }

        if n > 0 {
            // Process ANSI normalization first
            processed := buffer[:n]
            if c.ansiEnhanced != nil {
                cs := strings.ToUpper(c.charset)
                if cs != "PETSCIIU" && cs != "PETSCIIL" && cs != "ATASCII" && os.Getenv("ANSI_NORMALIZE") != "false" {
                    processed = c.ansiEnhanced.ProcessANSIData(processed)
                }
            }
            if os.Getenv("HEX_DUMP") == "true" {
                c.debugHexDump("SSH->CLIENT", processed, 256)
            }
            // Legacy preamble suppression on SSH
            csUp := strings.ToUpper(c.charset)
            if (csUp == "PETSCIIU" || csUp == "PETSCIIL" || csUp == "ATASCII") && c.preambleActive {
                c.preambleBuf = append(c.preambleBuf, processed...)
                if c.shouldFlushLegacyPreamble(processed) {
                    c.preambleActive = false
                    processed = c.preambleBuf
                    c.preambleBuf = c.preambleBuf[:0]
                } else {
                    continue
                }
            }
            // No auto-detect/switching on SSH
            // Convert charset-specific bytes to UTF-8 for browser rendering
            var outputData []byte
            bypassUsed := false
            switch c.charset {
            case "CP437":
                utf8String := ConvertCP437ToUTF8Enhanced(processed)
                outputData = []byte(utf8String)
            case "PETSCIIU", "PETSCIIL", "ATASCII":
                // Smart ASCII pass-through: check BEFORE control translation
                // to avoid bypassing when there are PETSCII control codes
                if !c.forceLegacyFull && shouldBypassLegacyMapping(processed) {
                    // Even in bypass mode, translate control codes to ANSI
                    processed = c.translateLegacyControls(processed)
                    processed = normalizeCSISGRAny(processed)
                    outputData = processed
                    bypassUsed = true
                } else {
                    // Phase 2: translate PETSCII/ATASCII control bytes into ANSI
                    processed = c.translateLegacyControls(processed)
                    processed = normalizeCSISGRAny(processed)
                    if c.charset == "PETSCIIU" {
                        utf8String := ConvertPETSCIIUToUTF8(processed)
                        outputData = []byte(utf8String)
                    } else if c.charset == "PETSCIIL" {
                        utf8String := ConvertPETSCIILToUTF8(processed)
                        outputData = []byte(utf8String)
                    } else {
                        utf8String := ConvertATASCIIToUTF8(processed)
                        outputData = []byte(utf8String)
                    }
                }
            default:
                outputData = processed
            }

            if os.Getenv("HEX_DUMP") == "true" {
                c.mu.Lock()
                cols := c.termCols
                rows := c.termRows
                cs := c.charset
                c.mu.Unlock()
                log.Printf("MODE: charset=%s cols=%d rows=%d bypass=%v", cs, cols, rows, bypassUsed)
                // Log the final bytes as the browser will see them
                logBytes := outputData
                if bytes.Contains(logBytes, []byte{0x1B, '['}) {
                    logBytes = normalizeCSISGRAny(append([]byte(nil), logBytes...))
                }
                preview := string(logBytes)
                if len(preview) > 120 {
                    preview = preview[:120] + "…"
                }
                log.Printf("TEXT (final) %s: %q", cs, preview)
            }

            // Final output guard for SSH path as well
            if bytes.Contains(outputData, []byte{0x1B, '['}) {
                outputData = normalizeCSISGRAny(outputData)
            }
            encoded := base64.StdEncoding.EncodeToString(outputData)
            c.sendJSON(Message{
                Type:     "data",
                Data:     encoded,
                Encoding: "base64",
            })
        }
    }
}

// sendToRemote forwards user keystrokes to the active remote (telnet/SSH),
// translating DEL->BS and optionally converting UTF-8 to CP437.
func (c *Client) sendToRemote(data string) {
    // Copy refs while locked; do IO after unlocking
    c.mu.Lock()
    telnetConn := c.telnet
    sshIn := c.sshIn
    charset := c.charset
    c.mu.Unlock()

    var outputData []byte

    // Handle backspace - xterm.js sends ASCII DEL (127) for backspace
    // Most BBSes expect ASCII BS (8) instead
    dataBytes := []byte(data)
    for i, b := range dataBytes {
        if b == 127 { // ASCII DEL
            dataBytes[i] = 8 // ASCII BS
        }
        // In PETSCII, delete-left is 0x14, map ASCII BS to that
        if (c.charset == "PETSCIIU" || c.charset == "PETSCIIL") && dataBytes[i] == 8 {
            dataBytes[i] = 0x14
        }
    }

    // Translate common ANSI cursor keys and controls to legacy equivalents
    if c.charset == "PETSCIIU" || c.charset == "PETSCIIL" || c.charset == "ATASCII" {
        dataBytes = translateANSIInputToLegacy(dataBytes, c.charset)
    }

    if telnetConn != nil {
        switch charset {
        case "CP437":
            // Convert UTF-8 input to CP437 for telnet connections
            outputData = ConvertUTF8ToCP437Enhanced(string(dataBytes))
        case "PETSCIIU":
            outputData = ConvertUTF8ToPETSCIIU(string(dataBytes))
        case "PETSCIIL":
            outputData = ConvertUTF8ToPETSCIIL(string(dataBytes))
        case "ATASCII":
            outputData = ConvertUTF8ToATASCII(string(dataBytes))
        default:
            outputData = dataBytes
        }
    } else {
        outputData = dataBytes
    }

    if telnetConn != nil {
        _, _ = telnetConn.Write(outputData)
    } else if sshIn != nil {
        _, _ = sshIn.Write(outputData)
    }
}

// translateANSIInputToLegacy maps ANSI cursor/clear sequences commonly emitted by browsers
// to PETSCII/ATASCII control bytes expected by legacy BBS software.
func translateANSIInputToLegacy(in []byte, charset string) []byte {
    out := make([]byte, 0, len(in))
    for i := 0; i < len(in); i++ {
        b := in[i]
        if b == 0x1B { // ESC
            // Peek next
            if i+1 < len(in) {
                n := in[i+1]
                // CSI sequences ESC [
                if n == '[' {
                    // Try to parse simple no-parameter sequences
                    if i+2 < len(in) {
                        final := in[i+2]
                        mapped := false
                        switch final {
                        case 'A': // Up
                            if charset == "PETSCIIU" || charset == "PETSCIIL" { out = append(out, 0x91); mapped = true }
                            if charset == "ATASCII" { out = append(out, 0x1C); mapped = true }
                        case 'B': // Down
                            if charset == "PETSCIIU" || charset == "PETSCIIL" { out = append(out, 0x11); mapped = true }
                            if charset == "ATASCII" { out = append(out, 0x1D); mapped = true }
                        case 'C': // Right
                            if charset == "PETSCIIU" || charset == "PETSCIIL" { out = append(out, 0x1D); mapped = true }
                            if charset == "ATASCII" { out = append(out, 0x1F); mapped = true }
                        case 'D': // Left
                            if charset == "PETSCIIU" || charset == "PETSCIIL" { out = append(out, 0x9D); mapped = true }
                            if charset == "ATASCII" { out = append(out, 0x1E); mapped = true }
                        case 'H': // Home
                            if charset == "PETSCIIU" || charset == "PETSCIIL" {
                                out = append(out, 0x13)
                                mapped = true
                            }
                        case 'J': // Clear screen variants ESC[J or ESC[2J
                            // detect optional '2' before 'J'
                            if i+2 < len(in) && in[i+2] == '2' && i+3 < len(in) && in[i+3] == 'J' {
                                if charset == "PETSCIIU" || charset == "PETSCIIL" {
                                    out = append(out, 0x93)
                                    mapped = true
                                    i += 1 // consume extra char ('2') with 'J' handled below
                                }
                            } else {
                                if charset == "PETSCIIU" || charset == "PETSCIIL" {
                                    out = append(out, 0x93)
                                    mapped = true
                                }
                            }
                        }
                        if mapped {
                            // consume ESC [ X
                            i += 2
                            continue
                        }

                        // Try ESC [ n ~ sequences (e.g., Home/End from some keyboards)
                        // Minimal handling: skip unknown CSI
                    }
                } else if n == 'O' { // SS3 sequences (ESC O A, etc.)
                    if i+2 < len(in) {
                        final := in[i+2]
                        mapped := false
                        switch final {
                        case 'A': // Up
                            if charset == "PETSCIIU" || charset == "PETSCIIL" { out = append(out, 0x91); mapped = true }
                            if charset == "ATASCII" { out = append(out, 0x1C); mapped = true }
                        case 'B': // Down
                            if charset == "PETSCIIU" || charset == "PETSCIIL" { out = append(out, 0x11); mapped = true }
                            if charset == "ATASCII" { out = append(out, 0x1D); mapped = true }
                        case 'C': // Right
                            if charset == "PETSCIIU" || charset == "PETSCIIL" { out = append(out, 0x1D); mapped = true }
                            if charset == "ATASCII" { out = append(out, 0x1F); mapped = true }
                        case 'D': // Left
                            if charset == "PETSCIIU" || charset == "PETSCIIL" { out = append(out, 0x9D); mapped = true }
                            if charset == "ATASCII" { out = append(out, 0x1E); mapped = true }
                        case 'H': // Home
                            if charset == "PETSCIIU" || charset == "PETSCIIL" { out = append(out, 0x13); mapped = true }
                        }
                        if mapped {
                            i += 2
                            continue
                        }
                    }
                }
            }
        }
        out = append(out, b)
    }
    return out
}

// sendMessage is a convenience wrapper for emitting JSON messages.
func (c *Client) sendMessage(msgType, message string) {
	c.sendJSON(Message{
		Type:    msgType,
		Message: message,
	})
}

// sendJSON writes a JSON message to the browser with a write deadline to avoid
// stalled connections causing goroutine leaks.
func (c *Client) sendJSON(msg Message) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ws != nil {
		// Set write deadline to prevent blocking on slow proxy/clients
		c.ws.SetWriteDeadline(time.Now().Add(60 * time.Second))
		if err := c.ws.WriteJSON(msg); err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				// Expected close, don't log as error
				return
			}
			log.Printf("Write error: %v", err)
			// On write errors (e.g., i/o timeout), schedule a disconnect to clean up
			go c.disconnect()
		}
	}
}

// disconnect tears down the session: cancels ZMODEM, closes sockets/sessions,
// and signals the ping/pong loop to exit.
func (c *Client) disconnect() {
    c.mu.Lock()
    defer c.mu.Unlock()

	// Signal done channel to stop ping ticker
	select {
	case c.done <- true:
	default:
	}

	// Cancel any active ZMODEM transfer scoped to this session
	if c.zmodemReceiver != nil {
		c.zmodemReceiver.Cancel()
	}

    // Stop any active capture
    if err := captureManager.StopCapture(); err != nil && err.Error() != "no active capture" {
        log.Printf("Failed to stop capture on disconnect: %v", err)
    }

	if c.telnet != nil {
		c.telnet.Close()
		c.telnet = nil
	}

    if c.sshSession != nil {
        c.sshSession.Close()
        c.sshSession = nil
    }

    if c.ssh != nil {
        c.ssh.Close()
        c.ssh = nil
    }

    if c.sshIn != nil {
        c.sshIn.Close()
        c.sshIn = nil
    }

}
