RetroTerm PETSCII Enhancement Session
======================================
Date: 2025-09-15 (Evening)
Branch: petscii-good-baseline

Session Summary
---------------
Enhanced PETSCII/ATASCII support in RetroTerm with improved debugging, capture/replay functionality, and character conversion fixes.

Major Accomplishments
---------------------

### 1. PETSCII Display Improvements
- **Fixed CR handling**: Multiple consecutive CRs now collapse to single CRLF, preventing extra blank lines
- **Added DELETE (0x14) support**: Implemented as destructive backspace (BS-space-BS sequence)
- **Added BELL (0x07) and TAB (0x09)**: Pass-through support for audio alerts and spacing
- **Fixed color mapping**: Color codes 0x05/0x1C/0x1E/0x1F and 0x90-0x9F now translate to ANSI SGR in both PETSCIIU and PETSCIIL modes

### 2. Enhanced Capture System
- **Created capture_manager.go**:
  - Timestamped captures with metadata (host, port, charset, protocol)
  - Organized storage in `captures/` directory
  - JSON metadata files alongside binary captures

- **API Endpoints** (when CAPTURE_ENHANCED=true):
  - `/api/captures` - List all captures with metadata
  - `/api/capture` - Download specific capture
  - `/api/capture/delete` - Remove capture
  - `/api/capture/compare` - Compare two captures

- **Capture Analyzer Tool** (`tools/capture_analyzer.go`):
  - Hex dump with PETSCII control highlighting
  - PETSCII/ANSI sequence analysis
  - Side-by-side capture comparison
  - Detection of common issues (CR handling, SI bytes, uppercase M in SGR)

### 3. Capture Replay System
- **Browser UI**:
  - "Replay Capture" menu item
  - Modal showing all captures with metadata
  - One-click replay functionality

- **Server-side replay**:
  - Replays captures through same PETSCII processing pipeline
  - Automatically sets correct charset from metadata
  - Processes raw PETSCII bytes through proper conversion

- **JavaScript Integration** (`static/replay.js`):
  - WebSocket message handling for capture lists
  - Auto-connect WebSocket on page load for replay features

### 4. Bug Fixes
- **PETSCII graphics preservation**: PETSCIIU/PETSCIIL conversion tables now map the glyph ranges while the shared control translator still handles color/control bytes
- **WebSocket auto-connection**: Ensures replay functionality works without manual connection
- **Preamble suppression**: Disabled for replay to prevent data loss

Current Issues Being Addressed
------------------------------

### Replay Display Problems
1. **Partial corruption**: Some PETSCII art displays with � replacement characters
2. **Uppercase M in SGR**: ESC[7M appearing instead of ESC[7m despite normalization
3. **Extra characters**: Text like "H�i�t� Delete" showing control codes between letters

### Root Causes Identified
1. **Order of operations**: Control translation → normalization → Unicode conversion
2. **Double processing**: Some bytes being both translated AND passed through
3. **Normalization timing**: May be happening at wrong stage in pipeline

Files Modified
-------------
- `main.go`: Capture replay, WebSocket messages, enhanced capture integration
- `legacy_processors.go`: DELETE handling, CR collapsing, BELL/TAB support, color mapping fixes
- `capture_manager.go`: NEW - Complete capture management system
- `static/replay.js`: NEW - Browser-side replay UI functionality
- `static/index.html`: Added replay UI elements
- `static/app.js`: WebSocket auto-connect, replay message handling
- `tools/capture_analyzer.go`: NEW - Capture analysis tool

Reference Documentation Updated
-------------------------------
- `references/baseline-2025-09-15.md`: Updated with new behaviors
- `references/issues.md`: Current issues and recent fixes
- `references/changelog.md`: Complete change history
- `references/capture-workflow.md`: NEW - Comprehensive capture/replay guide
- `references/session-2025-09-15-evening.md`: THIS FILE - Session summary

Test Captures Available
-----------------------
- `20250915_194100_dzbbs_hopto_org_64128_PETSCIIL.bin` (821 bytes)
- `20250915_193310_dzbbs_hopto_org_64128_PETSCIIL.bin` (2308 bytes)
- `20250915_192946_dzbbs_hopto_org_64128_PETSCIIL.bin` (795 bytes)
- `20250915_192722_dzbbs_hopto_org_64128_PETSCIIL.bin` (1327 bytes)

Environment Variables
--------------------
- `CAPTURE_ENHANCED=true` - Enable enhanced capture with metadata
- `CAPTURE_RAW=true` - Basic capture mode
- `CAPTURE_FILE=<filename>` - Specify capture filename (basic mode)
- `CAPTURE_TRUNCATE=true` - Clear file on start (basic mode)
- `HEX_DUMP=true` - Show hex dumps of data flow
- `ANSI_DEBUG=true` - Verbose ANSI processor logging

FIXES APPLIED (Latest)
---------------------
1. **Fixed PETSCII color codes in both modes**: Previously only mapped colors for PETSCIIU mode, but captures show color codes (0x90-0x9F) are used in PETSCIIL mode too
2. **Removed double normalization**: playCapture was calling normalizeCSISGRAny twice, now only once
3. **Fixed missing brace syntax error**: translatePETSCIIToANSI was missing closing brace

Next Steps
----------
1. Test replay with actual browser interface to verify fixes
2. Ensure all PETSCII control codes are properly consumed (not passed through)
3. Test with more BBSes for edge cases
4. Consider creating automated tests for PETSCII translation

Key Learnings
------------
- PETSCII captures contain raw bytes that need full processing pipeline for replay
- Color codes in 0x80-0x9F range have different meanings in PETSCIIU vs PETSCIIL
- Order of operations critical: telnet processing → control translation → normalization → Unicode conversion
- Preamble suppression can interfere with replay if not handled carefully
