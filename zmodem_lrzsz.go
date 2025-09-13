// Package main - Zmodem file transfer implementation using lrzsz
//
// This module implements Zmodem file transfer protocol support for the BBS terminal
// using the external 'rz' command from the lrzsz package. It handles:
// - Automatic detection of Zmodem transfer initiation
// - Bidirectional data bridging between telnet and rz process
// - File reception and delivery to the browser client
// - Progress monitoring and status updates
//
// The implementation works by:
// 1. Monitoring incoming telnet data for Zmodem signatures
// 2. Spawning an 'rz' process when transfer is detected
// 3. Bridging raw telnet data to/from the rz process
// 4. Sending received files to the browser for download

package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// LrzszReceiver handles Zmodem transfers using the external 'rz' command.
// It manages the lifecycle of a Zmodem file transfer session including:
// process management, data routing, and file delivery.
type LrzszReceiver struct {
	client       *Client        // WebSocket client connection
	active       bool           // Whether a transfer is currently active
	tempDir      string         // Temporary directory for received files
	rzCmd        *exec.Cmd      // The rz process handle
	rzStdin      io.WriteCloser // Pipe to send data to rz
	rzStdout     io.ReadCloser  // Pipe to read responses from rz
	buffer       []byte         // Buffer for detecting Zmodem signatures
	startTime    time.Time      // When the transfer started
	lastActivity time.Time      // Last time we saw activity
}

// NewLrzszReceiver creates a new Zmodem receiver instance for the given client connection.
// The receiver starts in an inactive state and monitors for Zmodem initiation sequences.
func NewLrzszReceiver(client *Client) *LrzszReceiver {
	return &LrzszReceiver{
		client: client,
		buffer: make([]byte, 0),
	}
}

// ProcessData processes incoming telnet data and manages Zmodem transfers.
// It returns:
// - remaining: any data that should be passed through to the terminal
// - consumed: true if the data was consumed by the Zmodem handler
//
// When not in a transfer, it monitors for Zmodem initiation sequences.
// During a transfer, all raw data is piped directly to the rz process.
func (l *LrzszReceiver) ProcessData(data []byte) ([]byte, bool) {
	// Check for Zmodem start if not active
	if !l.active {
		// Buffer data to look for patterns
		l.buffer = append(l.buffer, data...)

		if startIdx, ok := l.findZmodemStartIndex(l.buffer); ok {
			if err := l.startRz(); err != nil {
				// Failed to start rz
				l.buffer = make([]byte, 0)
				return data, false
			}
			l.active = true
			l.startTime = time.Now()
			l.lastActivity = time.Now()
			// Started rz for file reception

			// Send notification to client
			l.client.sendJSON(Message{
				Type:    "zmodemStatus",
				Message: "File transfer started (using rz)...",
			})

			// Write ALL data from the ZMODEM start to rz (important!)
			if l.rzStdin != nil && len(l.buffer) > startIdx {
				// Clean the stream of Telnet IAC negotiations before feeding rz
				initial := l.buffer[startIdx:]
				clean := l.client.processTelnetData(initial)
				if len(clean) > 0 {
					if _, err := l.rzStdin.Write(clean); err != nil {
						// Error writing initial buffer
					}
				}
			}

			// Clear buffer
			l.buffer = make([]byte, 0)
			return nil, true // Consume ALL data
		}

		// Clear buffer if it gets too large
		if len(l.buffer) > 1024 {
			l.buffer = l.buffer[512:] // Keep last 512 bytes
		}

		return data, false // Pass through if no zmodem detected
	}

	// If rz is active, pipe telnet data (with IAC stripped) directly to it
	if l.active && l.rzStdin != nil {
		// Strip Telnet negotiations and unescape IAC if needed
		clean := l.client.processTelnetData(data)

		// Writing to rz stdin

		// Update activity time
		l.lastActivity = time.Now()

		// Write cleaned data to rz immediately
		if _, err := l.rzStdin.Write(clean); err != nil {
			// Error writing to rz
			l.completeTransfer()
			return nil, true // Consume data but end transfer
		}

		// Don't check for end markers - let rz handle protocol
		// rz will exit when transfer completes

		return nil, true // Consume ALL data during transfer
	}

	return data, false // Pass through
}

// Cancel aborts any active Zmodem transfer and performs cleanup.
// It sends cancel sequences to the remote, terminates the rz process,
// and removes temporary files.
func (l *LrzszReceiver) Cancel() {
	if !l.active {
		return
	}
	// Cancelling Zmodem transfer
	l.active = false
	// Attempt to signal cancel to remote
	if l.client != nil {
		cancel := []byte{0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18}
		l.client.sendToRemote(string(cancel))
	}
	// Close stdin to rz to make it exit
	if l.rzStdin != nil {
		_ = l.rzStdin.Close()
		l.rzStdin = nil
	}
	// Kill rz process if running
	if l.rzCmd != nil && l.rzCmd.Process != nil {
		_ = l.rzCmd.Process.Kill()
		l.rzCmd = nil
	}
	// Cleanup temp directory
	if l.tempDir != "" {
		_ = os.RemoveAll(l.tempDir)
		l.tempDir = ""
	}
	l.buffer = make([]byte, 0)
}

// Active returns true if a Zmodem transfer is currently in progress
func (l *LrzszReceiver) Active() bool {
	return l.active
}

// detectZmodemStart checks if the data contains Zmodem protocol initialization sequences.
// It looks for various Zmodem signatures including ZRQINIT, ZRINIT, and user commands.
func (l *LrzszReceiver) detectZmodemStart(data []byte) bool {
	// Common Zmodem protocol signatures:
	patterns := [][]byte{
		[]byte("rz\r"),                       // user-initiated rz
		{0x2A, 0x2A, 0x18, 0x42, 0x30, 0x30}, // **\x18B00 (ZRQINIT hex)
		{0x2A, 0x2A, 0x18, 0x41},             // **\x18A (ZBIN header)
		{0x2A, 0x2A, 0x18, 0x43},             // **\x18C (ZBIN32 header)
		{0x18, 0x42, 0x30, 0x30},             // \x18B00 fragment
		{0x18, 0x43, 0x04},                   // \x18C ZFILE frame indicator (approx)
	}

	for _, pattern := range patterns {
		if bytes.Contains(data, pattern) {
			return true
		}
	}
	return false
}

// findZmodemStartIndex locates the start position of Zmodem data in the buffer.
// Returns the index and true if found, or (0, false) if not found.
// This is important for correctly aligning the data stream with the rz process.
func (l *LrzszReceiver) findZmodemStartIndex(data []byte) (int, bool) {
	// Prefer the first true ZMODEM header; avoid triggering solely on "rz\r"
	headerPatterns := [][]byte{
		{0x2A, 0x2A, 0x18, 0x42, 0x30, 0x30}, // **\x18B00 (ZRQINIT hex)
		{0x2A, 0x2A, 0x18, 0x41},             // **\x18A (ZBIN)
		{0x2A, 0x2A, 0x18, 0x43},             // **\x18C (ZBIN32)
		{0x18, 0x42, 0x30, 0x30},             // \x18B00 fragment
		{0x18, 0x43, 0x04},                   // \x18C ... (approx)
	}
	first := -1
	for _, p := range headerPatterns {
		if idx := bytes.Index(data, p); idx != -1 {
			if first == -1 || idx < first {
				first = idx
			}
		}
	}
	if first >= 0 {
		return first, true
	}
	// If we only saw the typed command "rz\r" but no header yet, keep buffering
	return 0, false
}

// detectZmodemEnd checks for Zmodem transfer completion or cancellation markers.
// Note: This is currently not used as we rely on rz process exit for completion.
func (l *LrzszReceiver) detectZmodemEnd(data []byte) bool {
	// Common end-of-transfer markers:
	patterns := [][]byte{
		[]byte("OO"),                   // Normal completion
		{0x18, 0x18, 0x18, 0x18, 0x18}, // Cancel sequence (5x CAN)
	}

	for _, pattern := range patterns {
		if bytes.Contains(data, pattern) {
			return true
		}
	}

	// Also check if buffer is getting too large (timeout)
	if len(l.buffer) > 100000 {
		return true
	}

	return false
}

// startRz spawns the 'rz' process to handle Zmodem file reception.
// It creates a temporary directory for received files and sets up
// bidirectional pipes for data communication.
func (l *LrzszReceiver) startRz() error {
	// Create temp directory for received files
	tempDir, err := os.MkdirTemp("", "zmodem_*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	l.tempDir = tempDir
	// Created temp directory

	// Start rz command with appropriate options:
	// -v: verbose mode for progress reporting
	// -b: binary mode (8-bit clean)
	// Note: Removed -e flag as it can interfere with Zmodem protocol
	l.rzCmd = exec.Command("rz", "-v", "-b")
	l.rzCmd.Dir = tempDir
	// Starting rz command

	// Get stdin pipe
	stdin, err := l.rzCmd.StdinPipe()
	if err != nil {
		os.RemoveAll(tempDir)
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}
	l.rzStdin = stdin

	// Get stdout pipe
	stdout, err := l.rzCmd.StdoutPipe()
	if err != nil {
		os.RemoveAll(tempDir)
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	l.rzStdout = stdout

	// Get stderr pipe for progress information
	stderr, err := l.rzCmd.StderrPipe()
	if err != nil {
		os.RemoveAll(tempDir)
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Proactively request Telnet BINARY both ways for 8-bit clean stream
	l.requestTelnetBinary()

	// Give telnet negotiation a moment to complete
	time.Sleep(100 * time.Millisecond)

	// Start the command
	if err := l.rzCmd.Start(); err != nil {
		os.RemoveAll(tempDir)
		log.Printf("Failed to start rz command: %v", err)
		return fmt.Errorf("failed to start rz: %w", err)
	}
	// Started rz process

	// Monitor rz in background
	go l.monitorRz()

	// Monitor progress from stderr
	go l.monitorProgress(stderr)

	// Forward rz stdout (handshake/ack frames) back to remote
	go l.forwardRzStdoutToRemote()

	// Start watchdog timer
	go l.watchdogTimer()

	// Notify browser to show download UI
	if l.client != nil {
		l.client.sendJSON(Message{Type: "downloadStart", Message: "ZMODEM transfer starting..."})
	}

	return nil
}

// requestTelnetBinary sends telnet commands to enable binary mode.
// This ensures 8-bit clean data path for Zmodem transfers.
func (l *LrzszReceiver) requestTelnetBinary() {
	if l.client == nil || l.client.telnet == nil {
		return
	}

	// Send IAC WILL BINARY and IAC DO BINARY to enable binary mode both ways
	const IAC = 255
	const WILL = 251
	const DO = 253
	const BINARY = 0

	binaryRequest := []byte{
		IAC, DO, BINARY, // Request remote to transmit binary
		IAC, WILL, BINARY, // We will transmit binary
	}

	l.client.mu.Lock()
	conn := l.client.telnet
	l.client.mu.Unlock()

	if conn != nil {
		if _, err := conn.Write(binaryRequest); err != nil {
			// Error requesting binary mode
		} else {
			// Requested telnet binary mode
		}
	}
}

// monitorProgress reads and reports transfer progress from rz's stderr output.
// It sends progress updates to the browser client via WebSocket messages.
func (l *LrzszReceiver) monitorProgress(stderr io.ReadCloser) {
	defer stderr.Close()

	buf := make([]byte, 1024)
	percentRe := regexp.MustCompile(`(\d{1,3})%`)
	for {
		n, err := stderr.Read(buf)
		if err != nil {
			if err == io.EOF || errors.Is(err, os.ErrClosed) || strings.Contains(err.Error(), "file already closed") {
				// Normal on process exit; treat as completion without error noise
				break
			}
			// Error reading stderr
			break
		}

		if n > 0 {
			// rz outputs progress info to stderr
			progressText := string(buf[:n])

			// Parse filename lines like: "Receiving: <name>"
			if strings.Contains(progressText, "Receiving:") && l.client != nil {
				// find after colon and space up to newline
				idx := strings.Index(progressText, "Receiving:")
				if idx >= 0 {
					line := progressText[idx:]
					// take portion up to end of line
					if nl := strings.Index(line, "\n"); nl >= 0 {
						line = line[:nl]
					}
					// extract filename after colon
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						name := strings.TrimSpace(parts[1])
						if name != "" {
							l.client.sendJSON(Message{Type: "downloadInfo", Message: name})
						}
					}
				}
			}

			// Extract and forward percentage if present
			if m := percentRe.FindStringSubmatch(progressText); len(m) == 2 && l.client != nil {
				pct := m[1]
				// Clamp numeric sanity 0-100
				// (client expects a number-like string)
				l.client.sendJSON(Message{Type: "downloadProgress", Message: pct})
			} else if l.client != nil {
				// Fallback: send raw progress line for visibility
				l.client.sendJSON(Message{Type: "zmodemProgress", Message: progressText})
			}

			// Progress update
		}
	}
}

// monitorRz waits for the rz process to complete and triggers cleanup.
// This goroutine runs for the lifetime of the transfer.
func (l *LrzszReceiver) monitorRz() {
	// Wait for rz to complete
	err := l.rzCmd.Wait()
	if err != nil {
		log.Printf("rz exited with error: %v", err)
	} else {
		// rz completed successfully
	}

	// Trigger completion
	if l.active {
		l.completeTransfer()
	}
}

// forwardRzStdoutToRemote bridges rz's protocol responses back to the remote BBS.
// This creates the bidirectional communication needed for Zmodem handshaking.
// IAC bytes (0xFF) must be escaped when sending through telnet.
func (l *LrzszReceiver) forwardRzStdoutToRemote() {
	if l.rzStdout == nil || l.client == nil {
		return
	}
	defer l.rzStdout.Close()

	buf := make([]byte, 4096)
	totalBytes := 0
	for {
		n, err := l.rzStdout.Read(buf)
		if n > 0 {
			totalBytes += n
			// Forwarding from rz to remote

			// Telnet connection
			l.client.mu.Lock()
			conn := l.client.telnet
			l.client.mu.Unlock()

			if conn != nil {
				// Always escape IAC when sending through Telnet (RFC 854)
				escaped := make([]byte, 0, n*2)
				for _, b := range buf[:n] {
					escaped = append(escaped, b)
					if b == 255 { // IAC byte
						escaped = append(escaped, 255) // Double it to escape
					}
				}
				dataToSend := escaped
				if len(escaped) > n {
					// Escaped IAC bytes
				}

				if _, writeErr := conn.Write(dataToSend); writeErr != nil {
					log.Printf("Error writing to telnet: %v", writeErr)
					return
				}
				log.Printf("LRZSZ: Successfully forwarded %d bytes to remote", len(dataToSend))
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("LRZSZ: Error reading rz stdout: %v", err)
			}
			return
		}
	}
}

// completeTransfer performs cleanup after a transfer completes or fails.
// It processes any received files, sends them to the browser client,
// and cleans up all resources.
func (l *LrzszReceiver) completeTransfer() {
	l.active = false
	l.buffer = make([]byte, 0)

	// Close rz stdin
	if l.rzStdin != nil {
		l.rzStdin.Close()
		l.rzStdin = nil
	}

	// Give rz a moment to finish writing
	time.Sleep(500 * time.Millisecond)

	// Check for received files in temp directory
	if l.tempDir != "" {
		files, err := os.ReadDir(l.tempDir)
		if err != nil {
			log.Printf("LRZSZ: Error reading temp dir: %v", err)
		} else {
			for _, file := range files {
				if !file.IsDir() {
					l.sendFileToClient(filepath.Join(l.tempDir, file.Name()), file.Name())
				}
			}
		}

		// Clean up temp directory
		os.RemoveAll(l.tempDir)
		l.tempDir = ""
	}

	// Kill rz if still running
	if l.rzCmd != nil && l.rzCmd.Process != nil {
		l.rzCmd.Process.Kill()
		l.rzCmd = nil
	}
}

// watchdogTimer monitors the overall transfer and cancels if it takes too long
func (l *LrzszReceiver) watchdogTimer() {
	// Allow long transfers; rely primarily on inactivity detection
	maxDuration := 30 * time.Minute
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if !l.active {
			return // Transfer completed
		}

		elapsed := time.Since(l.startTime)
		if elapsed > maxDuration {
			log.Printf("LRZSZ: Transfer exceeded maximum duration of %v", maxDuration)
			l.Cancel()
			return
		}

		// Check if we're making progress
		timeSinceLastActivity := time.Since(l.lastActivity)
		if timeSinceLastActivity > 90*time.Second {
			log.Printf("LRZSZ: No activity for %v, canceling transfer", timeSinceLastActivity)
			l.Cancel()
			return
		}
	}
}

// sendFileToClient reads a received file and sends it to the browser for download.
// The file data is base64-encoded and sent via WebSocket message.
func (l *LrzszReceiver) sendFileToClient(filePath, fileName string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("LRZSZ: Error reading file %s: %v", fileName, err)
		return
	}

	log.Printf("LRZSZ: Sending file to browser: %s (%d bytes)", fileName, len(data))

	// Send file to browser for download
	l.client.sendJSON(Message{
		Type:    "fileDownload",
		Message: fileName,
		Data:    base64.StdEncoding.EncodeToString(data),
	})
}
