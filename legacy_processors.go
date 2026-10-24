package main

// Minimal PETSCII/ATASCII control translation to ANSI/UTF-8 friendly bytes.
// This is not a full terminal emulator; it handles common controls so that
// xterm.js can render cursor movement, screen clear, reverse video and colors.

// translateLegacyControls translates PETSCII/ATASCII control bytes to ANSI.
func (c *Client) translateLegacyControls(data []byte) []byte {
    // Read charset under lock to avoid race conditions
    c.mu.Lock()
    charset := c.charset
    c.mu.Unlock()

    switch charset {
    case "PETSCIIU", "PETSCIIL":
        return c.translatePETSCIIToANSI(data)
    case "ATASCII":
        return translateATASCIIToANSI(data)
    default:
        return data
    }
}

func (c *Client) translatePETSCIIToANSI(data []byte) []byte {
    out := make([]byte, 0, len(data)*2)

    // PETSCII color/control code to SGR mapping (foreground)
    // Note: Some control codes in 0x80-0x9F range are used in both modes
    colorMap := map[byte]string{
        0x05: "97", // White (bright)
        0x1C: "31", // Red
        0x1E: "32", // Green
        0x1F: "34", // Blue
        0x90: "30", // Black
        0x81: "33", // Orange -> Yellow
        0x95: "33", // Brown -> Yellow
        0x96: "91", // Light red (bright)
        0x97: "90", // Dark gray
        0x98: "37", // Medium gray
        0x99: "92", // Light green (bright)
        0x9A: "94", // Light blue (bright)
        0x9B: "37", // Light gray
        0x9C: "35", // Purple (magenta)
        0x9E: "93", // Yellow (bright)
        0x9F: "96", // Cyan (bright)
    }

    for i := 0; i < len(data); i++ {
        b := data[i]
        prevCR := c.petsciiLastCR
        c.petsciiLastCR = false
        switch b {
        // Ignore undefined/unused low control codes to prevent replacement chars
        case 0x00, 0x01, 0x02, 0x03, 0x04, 0x06, 0x08, 0x10, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1A:
            // These PETSCII codes are undefined or not displayable - skip them
            // Note: 0x1B (ESC) is passed through for any direct ANSI from BBS
            continue
        // Mode switches (runtime)
        // Tab, Bell, and Form Feed
        case 0x07: // BELL
            out = append(out, 0x07) // Pass through ASCII bell
            continue
        case 0x09: // TAB
            out = append(out, 0x09) // Pass through ASCII tab
            continue
        case 0x0C: // Form Feed - treat as clear screen (some BBSes send this)
            out = append(out, 0x1B, '[', '2', 'J', 0x1B, '[', 'H')
            continue
        case 0x0E: // Shift out: switch to lower/uppercase
            c.mu.Lock()
            c.charset = "PETSCIIL"
            c.mu.Unlock()
            continue
        case 0x0F: // Shift in: ignore output, keep current charset
            // Some servers send 0x0F; treat as a no-op to avoid U+FFFD
            continue
        case 0x8E: // Switch to upper/graphics
            c.mu.Lock()
            c.charset = "PETSCIIU"
            c.mu.Unlock()
            continue
        // Cursor movement
        case 0x11: // Down
            out = append(out, 0x1B, '[', 'B')
            continue
        case 0x91: // Up
            out = append(out, 0x1B, '[', 'A')
            continue
        case 0x1D: // Right
            out = append(out, 0x1B, '[', 'C')
            continue
        case 0x9D: // Left
            out = append(out, 0x1B, '[', 'D')
            continue
        // DELETE (destructive backspace)
        case 0x14: // DELETE
            // PETSCII DELETE moves left and erases
            // Use backspace, space, backspace sequence for destructive delete
            out = append(out, 0x08, ' ', 0x08)
            continue
        // Home and clear
        case 0x13: // HOME
            out = append(out, 0x1B, '[', 'H')
            continue
        case 0x93: // CLR
            out = append(out, 0x1B, '[', '2', 'J', 0x1B, '[', 'H')
            continue
        // Reverse video
        case 0x12: // Reverse on
            out = append(out, 0x1B, '[', '7', 'm')
            continue
        case 0x92: // Reverse off
            out = append(out, 0x1B, '[', '2', '7', 'm')
            continue
        // LF: skip when it completes a CRLF, since the CR already emitted one.
        // The CR may have arrived at the end of the previous read.
        case 0x0A:
            if !prevCR {
                out = append(out, '\n')
            }
            continue
        // CR in PETSCII moves to beginning of NEXT line (like CRLF)
        case 0x0D:
            out = append(out, '\r', '\n')
            c.petsciiLastCR = true
            continue
        default:
            if sgr, ok := colorMap[b]; ok {
                out = append(out, 0x1B, '[')
                out = append(out, []byte(sgr)...)
                out = append(out, 'm')
                continue
            }
            // Pass through all other bytes unchanged
            // The PETSCII graphics bytes will be converted to Unicode later
            out = append(out, b)
        }
    }
    return out
}

func translateATASCIIToANSI(data []byte) []byte {
    out := make([]byte, 0, len(data)*2)
    for i := 0; i < len(data); i++ {
        b := data[i]
        switch b {
        // ATASCII EOL (0x9B) -> CRLF for terminal
        case 0x9B:
            out = append(out, '\r', '\n')
            continue
        // Clear screen (0x7D in ATASCII)
        case 0x7D:
            out = append(out, 0x1B, '[', '2', 'J', 0x1B, '[', 'H')
            continue
        // Note: 0x0C is a graphics character in ATASCII (▘), NOT form feed
        // Cursor movement (0x1C..0x1F Up/Down/Left/Right)
        case 0x1C: // Up
            out = append(out, 0x1B, '[', 'A')
            continue
        case 0x1D: // Down
            out = append(out, 0x1B, '[', 'B')
            continue
        case 0x1E: // Left
            out = append(out, 0x1B, '[', 'D')
            continue
        case 0x1F: // Right
            out = append(out, 0x1B, '[', 'C')
            continue
        // Backspace (0x7E in ATASCII is destructive backspace). Raw 0x08 would
        // be mapped to a glyph later, so move the cursor with CSI instead.
        case 0x7E:
            out = append(out, 0x1B, '[', 'D', ' ', 0x1B, '[', 'D')
            continue
        // Tab (0x7F) and Bell (0xFD) pass through untouched: the ATASCII glyph
        // table maps them to U+0009 and U+0007
        // Delete line (0x9C)
        case 0x9C:
            out = append(out, 0x1B, '[', '2', 'K')
            continue
        // Insert line (0x9D)
        case 0x9D:
            out = append(out, 0x1B, '[', 'L')
            continue
        // Escape (0x1B) - pass through for any ANSI
        case 0x1B:
            out = append(out, b)
            continue
        // Note: 0x08 and 0x09 are graphics characters in ATASCII (◢ and ▗),
        // NOT ASCII backspace/tab. ATASCII uses 0x7E for backspace and 0x7F for tab.
        default:
            // Pass through all other bytes for glyph mapping
        }
        out = append(out, b)
    }
    return out
}
