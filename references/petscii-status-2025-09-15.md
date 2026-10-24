PETSCII Implementation Status
============================
Date: 2025-09-15

Current Implementation
----------------------

### Working Features

1. **Control Code Translation** (legacy_processors.go)
   - 0x0E (SHIFT_OUT) → switches to PETSCIIL mode
   - 0x8E → switches to PETSCIIU mode
   - 0x12/0x92 → REVERSE_ON/OFF (ESC[7m / ESC[27m)
   - 0x11/0x91/0x1D/0x9D → cursor movement (ESC[A/B/C/D)
   - 0x13 → HOME (ESC[H)
   - 0x93 → CLEAR (ESC[2J ESC[H)
   - 0x14 → DELETE (destructive backspace: BS-space-BS)

2. **Color Mapping**
   - 0x05 → WHITE (bright)
   - 0x1C → RED
   - 0x1E → GREEN
   - 0x1F → BLUE
   - 0x90-0x9F → various colors (mapped in both PETSCIIU and PETSCIIL)
   - All colors map to ANSI SGR foreground colors

3. **Line Handling**
   - Lone CR (0x0D) → CRLF for proper line advancement
   - Consecutive CRs are collapsed to avoid multiple blank lines
   - CR+LF pairs are preserved

4. **Graphics Characters** (petscii_atascii.go)
   - 0xA0-0xBF → Unicode box drawing and block characters
   - 0xA1 → U+258C ▌ (left half block)
   - 0xA2 → U+2584 ▄ (lower half block)
   - 0xAC → U+251C ├ (box drawing)
   - 0xBB-0xBF → various quadrant blocks

5. **ANSI Normalization** (ansi_normalization.go)
   - ESC[...M → ESC[...m (SGR normalization)
   - Prevents CSI M from being interpreted as Delete Line

### Capture/Replay System

1. **Enhanced Capture Mode**
   - Captures raw telnet streams to `captures/` directory
   - Saves metadata (host, port, charset) in JSON sidecar files
   - Timestamped filenames for organization

2. **Replay Functionality**
   - Auto-detects latest capture if no CAPTURE_FILE specified
   - Loads charset from metadata
   - Processes through same pipeline as live connections
   - Forces full legacy processing during replay

3. **Analysis Tools**
   - capture_analyzer tool for hex dumps and PETSCII analysis
   - Highlights control codes and high bytes
   - Compare functionality between captures

### Testing Infrastructure

- Multiple test captures from DEAD ZONE BBS
- Verification scripts for PETSCII pipeline
- Hex dump debugging with HEX_DUMP=true

Known Issues (from references/issues.md)
-----------------------------------------

### Still Open
- Field input echo during form entry (may need terminal mode flag)
- Progress bars/animations using DELETE (0x14) may need timing adjustments

### Recently Fixed
- Replay showing replacement characters (fixed color mapping)
- Double normalization in replay (removed redundant call)
- Uppercase M in SGR sequences (normalization working)
- Multiple consecutive CRs (collapse logic added)
- DELETE not handled (implemented as destructive backspace)
- BELL and TAB support (pass-through added)
- ANSI private sequences corrupted (ESC[?1;2C becoming ESC[?1;2c)
- Characters "2c" and "2" appearing from malformed escape sequences
- DELETE operation showing replacement characters (fixed ASCII control char preservation)
- Backspace (0x08) being converted to � in PETSCII mapping

Next Steps
----------

1. Test with more PETSCII BBSes for edge cases
2. Consider timing adjustments for animation sequences
3. Potentially add local echo mode for forms
4. Continue monitoring captures for any unmapped characters

The PETSCII implementation is now largely complete and functional for standard BBS usage.