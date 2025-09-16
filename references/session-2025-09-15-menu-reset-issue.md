# Session: Menu Reset Issue Investigation
Date: 2025-09-15 (Evening Session #2)
Branch: petscii-good-baseline

## Current Issue: Menu Resetting in PETSCII Mode

### Problem Description
When connecting to DEAD ZONE BBS (dzbbs.hopto.org:64128) using PETSCII mode, the menu appears to be resetting or not displaying correctly. The captured output shows garbled PETSCII graphics followed by "PRESS DELETE!" message with improper spacing.

### Sample Output Observed
```
�▗▄▄▖▗▄▄▄▗▄▗▄ ▗▄▄▄▗▄▗▄  ▗▄▄▄ ▄▄▖
▌ ▌ ▌ ▄ ▌ ▌  ▌ ▄ ▌ ▌   ▌ ▄ ▝ ▄▖▝
▌ ▌ ▌ ▄▄▌ ▖  ▌   ▌ ▌ ▄▄▌ ▄▄▗▄▘▗▝

              3
```

## Investigation Findings

### 1. Capture Analysis (20250915_194100)
From hex dump analysis at position 0x300-0x310:
- Three consecutive CR (0x0D) characters at position 0x307-0x309
- Followed by spaces for positioning
- Then control codes including "PRESS DELETE!" message
- Two more CRs at the end (0x0D 0x0D)
- SHIFT_OUT (0x0E) to lowercase mode
- Light gray color (0x9B)

### 2. CR Handling Issue Identified
**Location**: `legacy_processors.go` lines 100-111

Current implementation collapses ALL consecutive CRs to just one:
```go
case 0x0D:
    // Skip all consecutive CRs first, keeping only the first one
    for i+1 < len(data) && data[i+1] == 0x0D {
        i++
    }
    out = append(out, '\r')
    // If next byte is not LF, add LF so lines advance
    if i+1 >= len(data) || data[i+1] != '\n' {
        out = append(out, '\n')
    }
```

**Problem**: The BBS is using multiple CRs intentionally for vertical spacing in its menu layout. When we collapse them, we're destroying the intended formatting.

### 3. Proposed Fix
Instead of collapsing ALL consecutive CRs, limit to maximum 2 CRs to preserve some spacing while avoiding excessive blank lines:

```go
case 0x0D:
    // Limit consecutive CRs to max 2 (preserve some spacing but avoid excessive blanks)
    crCount := 1
    for i+1 < len(data) && data[i+1] == 0x0D && crCount < 2 {
        i++
        crCount++
    }
    // Skip any additional CRs beyond 2
    for i+1 < len(data) && data[i+1] == 0x0D {
        i++
    }
    // Output the CRs we're keeping (max 2)
    for j := 0; j < crCount; j++ {
        out = append(out, '\r')
        // If next byte is not LF, add LF so lines advance
        if i+1 >= len(data) || data[i+1] != '\n' {
            out = append(out, '\n')
        }
    }
```

## Work Completed This Session

### 1. Capture System Enhancement
**Status**: ✅ COMPLETED

Added automatic capture functionality for debugging BBS connections:
- Modified `connectTelnet()` in main.go to start captures
- Added `captureManager.WriteCapture()` in `readTelnet()` to save raw data
- Added `captureManager.StopCapture()` in disconnect and error handlers
- Captures now automatically save to `captures/` directory with metadata

**Files Modified**:
- `main.go`: Added capture start/write/stop calls in telnet handling

### 2. Analysis Tools Used
- `./tools/capture_analyzer` - For analyzing PETSCII sequences
- Hex dump analysis to identify control codes
- Capture comparison functionality

## Current State of PETSCII Implementation

### Working Features
1. **Control Code Translation** (legacy_processors.go)
   - SHIFT_OUT/SHIFT_IN modes (0x0E, 0x8E)
   - REVERSE_ON/OFF (0x12/0x92)
   - Cursor movement (0x11/0x91/0x1D/0x9D)
   - HOME (0x13) and CLEAR (0x93)
   - DELETE as destructive backspace (0x14)

2. **Color Mapping**
   - Full PETSCII color set mapped to ANSI SGR codes
   - Both uppercase and lowercase mode colors

3. **Graphics Characters** (petscii_atascii.go)
   - Box drawing and block characters mapped to Unicode
   - Using standard Unicode box-drawing (U+25xx) instead of legacy symbols

4. **ANSI Normalization** (ansi_normalization.go)
   - ESC[...M → ESC[...m normalization
   - Private sequence preservation (ESC[?)

### Known Issues
1. **Menu Reset Issue** (CURRENT FOCUS)
   - Multiple CRs being collapsed breaks menu layout
   - Need to preserve intentional vertical spacing

2. **Other Open Issues** (from references/issues.md)
   - Field input echo during form entry
   - Progress bars/animations using DELETE may need timing adjustments

## Next Steps (TODO for next session)

1. **Fix CR Handling**
   - Implement the proposed fix to preserve up to 2 consecutive CRs
   - Test with DEAD ZONE BBS menu
   - Verify it doesn't break other BBSes

2. **Test Menu Stability**
   - Connect to DEAD ZONE BBS with fixed CR handling
   - Verify menu displays correctly
   - Check that navigation works properly

3. **Capture Review**
   - Analyze any new captures generated
   - Compare before/after CR fix
   - Document any new issues discovered

4. **Extended Testing**
   - Test with other PETSCII BBSes if available
   - Verify animations and progress bars still work
   - Check form input behavior

## Files to Review Next Session

1. `/home/jeffryw/workspace/retroterm/legacy_processors.go` - CR handling fix needed
2. `/home/jeffryw/workspace/retroterm/captures/` - New captures to analyze
3. `/home/jeffryw/workspace/retroterm/references/petscii-status-2025-09-15.md` - Overall status
4. `/home/jeffryw/workspace/retroterm/references/issues.md` - Open issues list

## Commands for Testing

```bash
# Build the project
go build -o retroterm .

# Analyze latest capture
./tools/capture_analyzer -i captures/[latest].bin -analyze

# View hex dump of capture
./tools/capture_analyzer -i captures/[latest].bin -hex -length 1000

# Run retroterm and connect to DEAD ZONE BBS
./retroterm
# Then connect to dzbbs.hopto.org:64128 with PETSCIIL charset
```

## Git Status
- Branch: petscii-good-baseline
- Latest commit: 8d48a73 Add PETSCII documentation and update .gitignore
- Working tree: Modified (capture functionality added, not committed)

## Key Insights
The menu reset issue appears to be caused by our aggressive CR collapsing logic. PETSCII BBSes use multiple CRs for layout control, and we need to preserve some of that spacing while still preventing excessive blank lines. The proposed fix limits consecutive CRs to 2, which should maintain layout without creating too much vertical space.