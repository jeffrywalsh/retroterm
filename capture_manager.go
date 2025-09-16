package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"
)

// CaptureManager handles capture file operations with metadata
type CaptureManager struct {
    mu         sync.RWMutex
    baseDir    string
    activePath string
    metadata   *CaptureMetadata
}

// CaptureMetadata stores information about a capture session
type CaptureMetadata struct {
    Filename    string    `json:"filename"`
    StartTime   time.Time `json:"startTime"`
    EndTime     time.Time `json:"endTime,omitempty"`
    Host        string    `json:"host"`
    Port        int       `json:"port"`
    Protocol    string    `json:"protocol"`
    Charset     string    `json:"charset"`
    BytesCaptured int64   `json:"bytesCaptured"`
    Description string    `json:"description,omitempty"`
}

// CaptureInfo provides capture file information for API responses
type CaptureInfo struct {
    Filename    string    `json:"filename"`
    Size        int64     `json:"size"`
    ModTime     time.Time `json:"modTime"`
    Metadata    *CaptureMetadata `json:"metadata,omitempty"`
}

var captureManager *CaptureManager

func init() {
    captureManager = &CaptureManager{
        baseDir: "captures",
    }
    // Create captures directory if it doesn't exist
    os.MkdirAll(captureManager.baseDir, 0755)
}

// StartCapture begins a new capture session with metadata
func (cm *CaptureManager) StartCapture(host string, port int, protocol, charset string) (string, error) {
    cm.mu.Lock()
    defer cm.mu.Unlock()

    // Generate timestamped filename
    timestamp := time.Now().Format("20060102_150405")
    sanitizedHost := strings.ReplaceAll(host, ".", "_")
    filename := fmt.Sprintf("%s_%s_%d_%s.bin", timestamp, sanitizedHost, port, charset)
    fullPath := filepath.Join(cm.baseDir, filename)

    // Create metadata
    cm.metadata = &CaptureMetadata{
        Filename:  filename,
        StartTime: time.Now(),
        Host:      host,
        Port:      port,
        Protocol:  protocol,
        Charset:   charset,
    }

    // Save metadata file
    metaPath := strings.TrimSuffix(fullPath, ".bin") + ".json"
    if data, err := json.MarshalIndent(cm.metadata, "", "  "); err == nil {
        os.WriteFile(metaPath, data, 0644)
    }

    cm.activePath = fullPath

    // Create empty capture file
    os.WriteFile(fullPath, nil, 0644)

    return filename, nil
}

// WriteCapture appends data to the active capture file
func (cm *CaptureManager) WriteCapture(data []byte) error {
    cm.mu.RLock()
    path := cm.activePath
    meta := cm.metadata
    cm.mu.RUnlock()

    if path == "" {
        return fmt.Errorf("no active capture")
    }

    f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    if err != nil {
        return err
    }
    defer f.Close()

    n, err := f.Write(data)
    if err == nil && meta != nil {
        cm.mu.Lock()
        cm.metadata.BytesCaptured += int64(n)
        cm.mu.Unlock()
    }

    return err
}

// StopCapture ends the current capture session
func (cm *CaptureManager) StopCapture() error {
    cm.mu.Lock()
    defer cm.mu.Unlock()

    if cm.activePath == "" {
        return fmt.Errorf("no active capture")
    }

    // Update metadata with end time
    if cm.metadata != nil {
        cm.metadata.EndTime = time.Now()
        metaPath := strings.TrimSuffix(cm.activePath, ".bin") + ".json"
        if data, err := json.MarshalIndent(cm.metadata, "", "  "); err == nil {
            os.WriteFile(metaPath, data, 0644)
        }
    }

    cm.activePath = ""
    cm.metadata = nil

    return nil
}

// ListCaptures returns all capture files with their metadata
func (cm *CaptureManager) ListCaptures() ([]CaptureInfo, error) {
    cm.mu.RLock()
    defer cm.mu.RUnlock()

    files, err := os.ReadDir(cm.baseDir)
    if err != nil {
        return nil, err
    }

    var captures []CaptureInfo
    for _, f := range files {
        if !strings.HasSuffix(f.Name(), ".bin") {
            continue
        }

        info, err := f.Info()
        if err != nil {
            continue
        }

        capture := CaptureInfo{
            Filename: f.Name(),
            Size:     info.Size(),
            ModTime:  info.ModTime(),
        }

        // Try to load metadata
        metaPath := filepath.Join(cm.baseDir, strings.TrimSuffix(f.Name(), ".bin")+".json")
        if data, err := os.ReadFile(metaPath); err == nil {
            var meta CaptureMetadata
            if json.Unmarshal(data, &meta) == nil {
                capture.Metadata = &meta
            }
        }

        captures = append(captures, capture)
    }

    return captures, nil
}

// GetCapture returns a specific capture file's data
func (cm *CaptureManager) GetCapture(filename string) ([]byte, error) {
    // Sanitize filename to prevent directory traversal
    if strings.Contains(filename, "..") || strings.Contains(filename, "/") {
        return nil, fmt.Errorf("invalid filename")
    }

    path := filepath.Join(cm.baseDir, filename)
    return os.ReadFile(path)
}

// DeleteCapture removes a capture file and its metadata
func (cm *CaptureManager) DeleteCapture(filename string) error {
    // Sanitize filename
    if strings.Contains(filename, "..") || strings.Contains(filename, "/") {
        return fmt.Errorf("invalid filename")
    }

    binPath := filepath.Join(cm.baseDir, filename)
    metaPath := strings.TrimSuffix(binPath, ".bin") + ".json"

    os.Remove(binPath)
    os.Remove(metaPath)

    return nil
}

// HTTP Handlers for capture management

func handleListCaptures(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    captures, err := captureManager.ListCaptures()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "success": true,
        "captures": captures,
    })
}

func handleGetCapture(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    filename := r.URL.Query().Get("filename")
    if filename == "" {
        http.Error(w, "filename required", http.StatusBadRequest)
        return
    }

    data, err := captureManager.GetCapture(filename)
    if err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "application/octet-stream")
    w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
    w.Write(data)
}

func handleDeleteCapture(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodDelete {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    filename := r.URL.Query().Get("filename")
    if filename == "" {
        http.Error(w, "filename required", http.StatusBadRequest)
        return
    }

    err := captureManager.DeleteCapture(filename)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "success": true,
        "message": "Capture deleted",
    })
}

// handleCompareCaptures provides side-by-side hex comparison
func handleCompareCaptures(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var req struct {
        File1  string `json:"file1"`
        File2  string `json:"file2"`
        Offset int    `json:"offset"`
        Length int    `json:"length"`
    }

    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }

    data1, err1 := captureManager.GetCapture(req.File1)
    data2, err2 := captureManager.GetCapture(req.File2)

    if err1 != nil || err2 != nil {
        http.Error(w, "File not found", http.StatusNotFound)
        return
    }

    // Default comparison length
    if req.Length == 0 {
        req.Length = 256
    }

    // Build comparison result
    result := compareBytes(data1, data2, req.Offset, req.Length)

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(result)
}

// compareBytes creates a detailed byte comparison
func compareBytes(data1, data2 []byte, offset, length int) map[string]interface{} {
    end1 := offset + length
    if end1 > len(data1) {
        end1 = len(data1)
    }
    end2 := offset + length
    if end2 > len(data2) {
        end2 = len(data2)
    }

    slice1 := data1[offset:end1]
    slice2 := data2[offset:end2]

    var differences []map[string]interface{}
    maxLen := len(slice1)
    if len(slice2) > maxLen {
        maxLen = len(slice2)
    }

    for i := 0; i < maxLen; i++ {
        var b1, b2 byte
        var has1, has2 bool

        if i < len(slice1) {
            b1 = slice1[i]
            has1 = true
        }
        if i < len(slice2) {
            b2 = slice2[i]
            has2 = true
        }

        if has1 != has2 || b1 != b2 {
            diff := map[string]interface{}{
                "offset": offset + i,
                "index":  i,
            }
            if has1 {
                diff["file1"] = fmt.Sprintf("0x%02X", b1)
                if b1 >= 32 && b1 < 127 {
                    diff["char1"] = string(b1)
                }
            }
            if has2 {
                diff["file2"] = fmt.Sprintf("0x%02X", b2)
                if b2 >= 32 && b2 < 127 {
                    diff["char2"] = string(b2)
                }
            }
            differences = append(differences, diff)
        }
    }

    return map[string]interface{}{
        "offset":      offset,
        "length":      length,
        "file1_bytes": len(slice1),
        "file2_bytes": len(slice2),
        "differences": differences,
        "identical":   len(differences) == 0,
    }
}