PETSCII Implementation Fixes Summary
=====================================
Date: 2025-09-15
Branch: petscii-good-baseline

## Major Issues Fixed

### 1. Display Alignment Issues
**Problem**: Vertical and horizontal misalignment in C64 BBS displays
**Solution**:
- Fixed multiple CR handling by collapsing consecutive CRs
- Replaced problematic Unicode "Symbols for Legacy Computing" (U+1FB8x) with standard box-drawing characters (U+25xx)
- These legacy symbols weren't rendering as monospace in many terminal fonts

### 2. ANSI Private Sequences Corruption
**Problem**: ESC[?1;2C was being incorrectly normalized to ESC[?1;2c, causing "2c" to appear as literal text
**Solution**: Modified normalizeCSISGRAny() to skip normalization for private sequences (those with '?')

### 3. DELETE Key Handling
**Problem**: DELETE (0x14) operations showing replacement characters (�) instead of erasing text
**Root Cause**: Backspace (0x08) and space (0x20) weren't preserved through PETSCII to UTF-8 conversion
**Solution**: Added preservation of ASCII control characters (BS, TAB, LF, CR) and space in ConvertPETSCII functions

## Technical Details

### Files Modified
1. **main.go**
   - Added hexLoggingEnabled() function for debug support
   - Enhanced playCapture() for auto-detection and metadata loading
   - Fixed normalizeCSISGRAny() to handle private sequences correctly

2. **legacy_processors.go**
   - Improved CR handling to collapse consecutive CRs
   - Enhanced PETSCII control code translation

3. **petscii_atascii.go**
   - Replaced problematic Unicode characters with standard box-drawing
   - Added ASCII control character preservation
   - Fixed backspace and space handling in DELETE sequences

## Verification Tools
- verify_ansi_fix.sh - Tests ANSI private sequence handling
- verify_petscii_fix.sh - Tests DELETE operation and replacement characters

## Results
- C64 BBS displays now render correctly with proper alignment
- No more "2c" or "2" appearing from malformed escape sequences
- DELETE key properly erases characters without showing replacement symbols
- PETSCII graphics display correctly as monospace box-drawing characters

## Commits
1. bdc46a0 - Improve capture/replay system and fix compilation error
2. b171f30 - Fix PETSCII display issues for C64 BBSes
3. 1f82720 - Fix ANSI private sequences being corrupted
4. 48f25d0 - Fix PETSCII DELETE key handling

## Testing
All fixes verified with:
- Live connections to DEAD ZONE BBS (dzbbs.hopto.org:64128)
- Capture replay testing with enhanced capture mode
- Hex dump analysis for byte-level verification