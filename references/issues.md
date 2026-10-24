Known Issues and Next Steps
---------------------------

Open
- Field input echo during form entry:
  - Some BBS systems expect local echo; may need terminal mode flag.
- Progress bars/animations:
  - Long sequences of DELETE (0x14) used for visual effects may need timing adjustments.

Recently Fixed (2025-09-15 late evening)
- Replay showing replacement characters (�):
  - Fixed by mapping color codes 0x90-0x9F in both PETSCIIU and PETSCIIL modes.
  - Previously only mapped for PETSCIIU, but captures showed they're used in both.
- Double normalization in replay:
  - Removed redundant normalizeCSISGRAny call in playCapture function.
- Uppercase M in SGR sequences:
  - Normalization now correctly converts ESC[7M to ESC[7m.

Recently Fixed (2025-09-15 evening)
- Multiple consecutive CRs causing extra blank lines:
  - Fixed by collapsing consecutive CRs into single CRLF.
- DELETE (0x14) not handled:
  - Implemented as destructive backspace (BS-space-BS).
- BELL and TAB support:
  - Added pass-through for 0x07 (BELL) and 0x09 (TAB).
- Replay not working:
  - Implemented full capture replay system with metadata.
  - Fixed WebSocket auto-connection on page load.
  - Fixed charset setting from capture metadata.

Recently Fixed (2025-09-15 baseline)
- Line clears while drawing (CSI … M → Delete Line):
  - Fixed via post-translation normalization to SGR 'm'.
- Lone CR collapsing lines:
  - PETSCII CR now emits CRLF when not followed by LF.
- PETSCII SI (0x0F) replacement glyphs:
  - Treated as no-op.
- Font missing warnings (.woff2):
  - Switched to shipped OTFs only.

Potential Enhancements
- Capture/Playback (dev-only): keep behind env flags for reproducible tests.
- Optional streaming joiner (CSI/OSC/DCS) as a guarded feature if we observe split-sequence leaks.
- UI sync for charset when BBS flips at runtime (status bubble only).
- Add “Clear capture” WS route to truncate CAPTURE_FILE safely from UI when TEST_MODE is active.

