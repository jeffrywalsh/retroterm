Changelog (PETSCII effort)
--------------------------

2025-09-15 (late evening) — Fixed PETSCII Replay Issues
- Fixed color codes 0x90-0x9F now mapped in both PETSCIIU and PETSCIIL modes
- Removed double normalization in playCapture function
- Fixed syntax error (missing brace) in translatePETSCIIToANSI
- Verified all PETSCII control codes are consumed (not passed through as raw bytes)

2025-09-15 (evening) — Enhanced PETSCII Support
- Added DELETE (0x14) handling as destructive backspace
- Collapse multiple consecutive CRs to prevent extra blank lines
- Added BELL (0x07) and TAB (0x09) pass-through
- Enhanced capture system with metadata and analysis tools
- Created capture_analyzer tool for debugging PETSCII streams
- Implemented full replay system with WebSocket integration
- Fixed WebSocket auto-connection for replay functionality
- Fixed charset setting from metadata during replay

2025-09-15 — Good Baseline (branch: petscii-good-baseline)
- Translate PETSCII/ATASCII → ANSI first; post-translation normalize SGR case (…M→m, …j→J).
- CR handling (CR→CRLF when LF absent). Ignore PETSCII 0x0F.
- Debug logs: show final output to browser.
- Fonts: use local Unifont OTFs; remove non-existent .woff2.

Earlier iterations
- Experiments with final normalizer and streaming joiner; scaled back to minimal stable set.
- Added optional capture/playback hooks and TEST_MODE toggle (kept behind env flags, not required for normal runs).

