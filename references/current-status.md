# Current Status - RetroTerm PETSCII Development
Date: 2025-09-15 (End of Day)
Branch: petscii-good-baseline

## 🔴 ACTIVE ISSUE: Menu Reset/Display Problem

### What's Happening
- DEAD ZONE BBS menu not displaying correctly in PETSCII mode
- Menu appears to reset or show garbled output
- "PRESS DELETE!" message appears with wrong spacing

### Root Cause Identified
- **File**: `legacy_processors.go` lines 103-105
- **Problem**: Collapsing ALL consecutive CR (0x0D) characters to just one
- **Impact**: Destroys intentional vertical spacing in BBS menus

### Proposed Solution
```go
// Instead of collapsing all CRs, limit to max 2
// This preserves layout while avoiding excessive blanks
```

## ✅ What's Working

### PETSCII Core Features
- Character set switching (SHIFT_OUT/IN)
- Color codes (all 16 colors mapped)
- Cursor control (up/down/left/right)
- Screen control (HOME, CLEAR)
- DELETE key (destructive backspace)
- Graphics characters (box drawing)
- ANSI normalization (fixes ESC[M issues)

### Infrastructure
- **Capture System**: NOW ACTIVE! Automatically captures all telnet sessions
- **Analysis Tools**: capture_analyzer for debugging
- **Replay System**: Can replay captures for testing

## 📁 Key Files Modified Today

1. **main.go**
   - Added automatic capture on telnet connect
   - Captures raw data stream for debugging
   - Stops capture on disconnect

2. **References Created**
   - `session-2025-09-15-menu-reset-issue.md` - Detailed investigation notes
   - `petscii-fixes-summary.md` - Summary of all fixes
   - `petscii-status-2025-09-15.md` - Implementation status

## 🔧 Development Environment

### Build & Run
```bash
go build -o retroterm .
./retroterm
```

### Debug Captures
```bash
# List recent captures
ls -la captures/*.bin | tail -5

# Analyze PETSCII sequences
./tools/capture_analyzer -i captures/[filename].bin -analyze

# View hex dump
./tools/capture_analyzer -i captures/[filename].bin -hex -length 1000
```

### Test Connection
- Host: dzbbs.hopto.org
- Port: 64128
- Charset: PETSCIIL (lowercase mode)

## 📝 Tomorrow's Priority Tasks

1. **Fix CR Handling**
   - Implement max-2-CR preservation
   - Test with DEAD ZONE BBS
   - Verify menu displays correctly

2. **Validate Fix**
   - Check other PETSCII BBSes
   - Ensure animations still work
   - Test form input behavior

3. **Document Results**
   - Update issues.md with resolution
   - Create test cases for regression
   - Update PETSCII documentation

## 🎯 Quick Start for Next Session

```bash
# 1. Check git status
git status

# 2. Review the CR handling code
vim legacy_processors.go +103

# 3. Check latest captures
ls -lt captures/*.bin | head -5

# 4. Build and test
go build -o retroterm .
./retroterm

# 5. Connect to DEAD ZONE BBS
# Use web UI to connect to dzbbs.hopto.org:64128 with PETSCIIL
```

## 📊 Progress Summary

### Completed
- ✅ PETSCII basic implementation
- ✅ DELETE key handling
- ✅ ANSI private sequence fix
- ✅ Graphics character mapping
- ✅ Capture system activation

### In Progress
- 🔄 Menu display CR handling
- 🔄 Form input echo behavior

### Pending
- ⏳ Animation timing adjustments
- ⏳ Extended BBS testing
- ⏳ Performance optimization

## 💡 Key Insight
The PETSCII implementation is mostly complete. The main remaining issue is proper handling of multiple consecutive CR characters, which BBSes use for layout control. The solution is to preserve up to 2 CRs instead of collapsing all of them.

---
*Last updated: 2025-09-15 Evening*
*Next session: Continue with CR handling fix*