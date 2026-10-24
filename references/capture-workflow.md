PETSCII Capture and Comparison Workflow
========================================

Purpose
-------
Capture raw telnet streams from PETSCII BBSes to compare RetroTerm output with SyncTERM or other terminal emulators for accuracy testing.

Setup
-----

### 1. Basic Capture (Simple)
```bash
# Capture raw stream to capture.bin
CAPTURE_RAW=true CAPTURE_FILE=petscii_capture.bin CAPTURE_TRUNCATE=true HEX_DUMP=true go run .
```

### 2. Enhanced Capture (With Metadata)
```bash
# Captures to timestamped files in captures/ directory with metadata
CAPTURE_ENHANCED=true HEX_DUMP=true go run .
```

Capture Files
-------------
- **Basic mode**: Single file specified by CAPTURE_FILE
- **Enhanced mode**:
  - Files saved as: `captures/YYYYMMDD_HHMMSS_hostname_port_charset.bin`
  - Metadata saved as: `captures/YYYYMMDD_HHMMSS_hostname_port_charset.json`

Workflow Steps
--------------

### Step 1: Capture from RetroTerm
1. Start server with capture enabled:
   ```bash
   CAPTURE_ENHANCED=true HEX_DUMP=true go run .
   ```

2. Connect to PETSCII BBS:
   - Open browser to http://localhost:8080
   - Set encoding to PETSCIIL or PETSCIIU
   - Connect to target BBS (e.g., DEAD ZONE)
   - Navigate to screens you want to test

3. Capture is automatically saved when disconnecting

### Step 2: Capture from SyncTERM (Reference)
1. Use SyncTERM's capture feature:
   - Press ALT+C to start capture
   - Navigate same screens
   - Press ALT+C to stop capture

2. Or use terminal logging:
   ```bash
   # Using script command (Linux/Mac)
   script -q -f syncterm_capture.bin syncterm
   ```

### Step 3: Analyze Captures

#### Using the Analyzer Tool
```bash
# Build the analyzer
go build -o capture_analyzer tools/capture_analyzer.go

# Show hex dump with PETSCII control highlighting
./capture_analyzer -i captures/your_capture.bin -hex -offset 0 -length 512

# Analyze PETSCII/ANSI sequences
./capture_analyzer -i captures/your_capture.bin -analyze

# Compare two captures
./capture_analyzer -i retroterm_capture.bin -compare syncterm_capture.bin -offset 0 -length 1024
```

#### Using API Endpoints (Enhanced Mode)
```bash
# List all captures
curl http://localhost:8080/api/captures

# Download a capture
curl http://localhost:8080/api/capture?filename=20250115_143022_deadzone_bbs_com_23_PETSCIIL.bin -o capture.bin

# Compare captures via API
curl -X POST http://localhost:8080/api/capture/compare \
  -H "Content-Type: application/json" \
  -d '{"file1":"capture1.bin","file2":"capture2.bin","offset":0,"length":256}'

# Delete old capture
curl -X DELETE "http://localhost:8080/api/capture/delete?filename=old_capture.bin"
```

What to Look For
----------------

### Common PETSCII Issues

1. **Control Code Translation**
   - PETSCII 0x93 (CLEAR) → ANSI ESC[2J ESC[H
   - PETSCII 0x13 (HOME) → ANSI ESC[H
   - PETSCII 0x11/0x91/0x1D/0x9D (cursor) → ANSI ESC[A/B/C/D

2. **Color Mapping**
   - PETSCII color codes (0x90-0x9F) → ANSI SGR colors
   - Check mapping accuracy in legacy_processors.go:24-41

3. **Line Handling**
   - Lone CR (0x0D) should map to CRLF for proper line advance
   - PETSCII 0x0F (SI) should be ignored (no output)

4. **SGR Normalization**
   - ESC[...M should become ESC[...m (lowercase)
   - ESC[...j should become ESC[...J (uppercase)

### Comparison Points

1. **Byte-level differences**
   - Use hex dump to see exact byte differences
   - Note: Some differences are expected (e.g., RetroTerm adds CRLF where SyncTERM might not)

2. **Visual output**
   - Compare rendered output in browser vs SyncTERM
   - Focus on: colors, box drawing, cursor positioning

3. **Sequence integrity**
   - Ensure ANSI sequences aren't split or corrupted
   - Check for stray characters from incomplete sequences

Test BBSes
----------
Good PETSCII test systems from bbs.csv:
- DEAD ZONE (deadzone-bbs.com:23) - Good PETSCII art
- 8 Bit Boyz BBS (8bitboyz.bbs.io:6502) - C64 themed
- Abacus BBS (bbs.alsgeeklab.com:2300) - Classic PETSCII

Debug Environment Variables
---------------------------
```bash
# Capture controls
CAPTURE_RAW=true         # Basic capture mode
CAPTURE_ENHANCED=true    # Enhanced capture with metadata
CAPTURE_FILE=<filename>  # Specify capture filename (basic mode)
CAPTURE_TRUNCATE=true    # Clear file on start (basic mode)

# Debug output
HEX_DUMP=true           # Show hex dumps of data flow
ANSI_DEBUG=true         # Verbose ANSI processor logging

# Test mode (if implemented)
TEST_MODE=true          # Enable test/replay features
```

Example Session
---------------
```bash
# 1. Start capture session
CAPTURE_ENHANCED=true HEX_DUMP=true go run .

# 2. Connect and capture
# Browser: Connect to DEAD ZONE, navigate to main menu

# 3. Build analyzer
go build -o capture_analyzer tools/capture_analyzer.go

# 4. Analyze the capture
./capture_analyzer -i captures/20250115_143022_deadzone_bbs_com_23_PETSCIIL.bin -analyze

# 5. Look for issues
# - Check for uppercase M in SGR sequences
# - Verify PETSCII controls are translated
# - Confirm SI (0x0F) is handled correctly

# 6. Compare with SyncTERM capture if available
./capture_analyzer -i retroterm.bin -compare syncterm.bin -length 2048
```

Notes
-----
- Captures are raw post-telnet-negotiation streams
- Enhanced captures include connection metadata for debugging
- The analyzer highlights PETSCII control codes in red, high bytes in yellow
- Comparison tool shows first 20 differences by default
