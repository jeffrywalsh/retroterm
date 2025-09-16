Usage and Debug Notes
---------------------

Run (typical)
- HEX_DUMP=true go run .
- Optional: ANSI_DEBUG=true for extra ANSI processor logs.

Encoding
- Set in the UI selector (PETSCIIL or PETSCIIU).
- Runtime switch by BBS: PETSCII 0x0E (Shift Out) → PETSCIIL; 0x8E → PETSCIIU.

Heuristic Bypass
- We bypass full conversion for chunks that are ≥85% printable ASCII and have no bytes ≥0x80; we still translate legacy controls first. This lets “connecting…” style lines render simply while preserving PETSCII art.

PETSCII/ATASCII Controls → ANSI
- Cursor: 0x11 down, 0x91 up, 0x1D right, 0x9D left
- Home: 0x13 → ESC[H
- Clear: 0x93 → ESC[2J ESC[H
- Reverse on/off: 0x12 → ESC[7m, 0x92 → ESC[27m
- Colors: mapped to reasonable SGR parameters (e.g., 0x9B → 37).

Post-Translation Normalization
- ESC[…M → ESC[…m (SGR; digits/semicolons only)
- ESC[…j → ESC[…J (clear; digits/semicolons only)

Debugging
- Enable HEX_DUMP=true to see:
  - HEX TELNET->CLIENT: sanitized telnet stream
  - MODE + TEXT (final) lines — what the browser receives
- Fonts must load: check Network tab for 200 on /fonts/unifont-17.0.01.otf and /fonts/unifont_upper-17.0.01.otf

Environment flags (summary)
- HEX_DUMP=true — show hexdump and TEXT logs
- ANSI_DEBUG=true — verbose enhanced ANSI logging
- TERM_ANSWERS=true — enable answers to terminal queries (CPR/DA)

Quick repro steps (2 minutes)
1) Start server: HEX_DUMP=true go run .
2) Open UI, set Encoding to PETSCIIL (or PETSCIIU), connect to target BBS.
3) Watch logs for:
   - TEXT (final): uses lowercase 'm' in SGR, 'J' in clears.
   - No ‘M’ at end of SGR sequences.
4) Verify Unifont loads (Network tab shows 200 for both OTFs), PETSCII art renders.

Optional: Capture/Playback (dev only, behind flags)
- To capture the clean stream (post telnet negotiations):
  - CAPTURE_RAW=true CAPTURE_TRUNCATE=true CAPTURE_FILE=capture.bin HEX_DUMP=true go run .
  - Connect and navigate to the problematic screen; capture.bin will be written.
- To replay in TEST_MODE (if enabled in this build):
  - TEST_MODE=true HEX_DUMP=true go run .
  - Click “Fake Connect” in the header to replay capture.bin.
  - Note: This is for development only; core flow does not depend on playback.

