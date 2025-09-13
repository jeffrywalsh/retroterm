package main

import (
    "bytes"
    "encoding/base64"
    "fmt"
    "io"
    "log"
    "net"
    "net/http"
    neturl "net/url"
    "os"
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
}

// ZmodemHandler interface for different Zmodem implementations
type ZmodemHandler interface {
	ProcessData(data []byte) ([]byte, bool)
	Cancel()
	Active() bool
}

// Client represents a WebSocket client connection that bridges to telnet/SSH
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
            list = append(list, BBSInfo{
                ID:          e.ID,
                Name:        e.Name,
                Host:        e.Host,
                Port:        e.Port,
                Protocol:    strings.ToLower(e.Protocol),
                Description: e.Description,
                Encoding:    e.Encoding,
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
    }

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
				client.charset = msg.Charset
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
		case "setCharset":
			client.charset = msg.Charset
		case "getBBSList":
			client.sendBBSList()
		case "connectToBBS":
			// SECURITY: This message type only uses pre-approved BBS IDs
			log.Printf("SECURITY: BBS connection via ID: %s", msg.BBSID)
			client.connectToBBS(msg.BBSID)
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

func (c *Client) sendBBSList() {
    msg := Message{
        Type:    "bbsList",
        BBSList: ApprovedBBSList,
    }
    c.sendJSON(msg)
}

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
	c.mu.Unlock()

	c.sendMessage("connected", fmt.Sprintf("Connected to %s", address))

	// Handle telnet data
	go c.readTelnet()
}

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
			c.sendJSON(Message{Type: "disconnected"})
			c.disconnect()
			return
		}

        if n > 0 {
            // Check for Zmodem in raw data FIRST (before telnet processing)
            rawData := buffer[:n]

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
                // Process ANSI sequences with enhanced processor
                processedData := cleanData
                if c.ansiEnhanced != nil {
                    processedData = c.ansiEnhanced.ProcessANSIData(cleanData)
                }
                
                // Convert CP437 to UTF-8 if needed
                var outputData []byte
                if c.charset == "CP437" {
                    utf8String := ConvertCP437ToUTF8Enhanced(processedData)
                    outputData = []byte(utf8String)
                } else {
                    outputData = processedData
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
}

// hasZmodemSignature checks for Zmodem start signatures
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
						} else {
							response = append(response, IAC, WONT, option)
						}
					} else if cmd == DONT {
						// Acknowledge with WONT
						response = append(response, IAC, WONT, option)
						if option == BINARY {
							c.telnetBinaryTX = false
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
					// Skip subnegotiation
					j := i + 2
					for j < len(data)-1 {
						if data[j] == IAC && data[j+1] == SE {
							i = j + 2
							break
						}
						j++
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
            // Convert CP437 to UTF-8 if needed
            var outputData []byte
            if c.charset == "CP437" {
                utf8String := ConvertCP437ToUTF8Enhanced(buffer[:n])
                outputData = []byte(utf8String)
            } else {
                outputData = buffer[:n]
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
	}

    if charset == "CP437" && telnetConn != nil {
        // Convert UTF-8 input to CP437 for telnet connections
        outputData = ConvertUTF8ToCP437Enhanced(string(dataBytes))
    } else {
        outputData = dataBytes
    }

    if telnetConn != nil {
        _, _ = telnetConn.Write(outputData)
    } else if sshIn != nil {
        _, _ = sshIn.Write(outputData)
    }
}

func (c *Client) sendMessage(msgType, message string) {
	c.sendJSON(Message{
		Type:    msgType,
		Message: message,
	})
}

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
	
    // Hex debugger removed

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
