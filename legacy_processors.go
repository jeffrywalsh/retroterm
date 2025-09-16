package main

// Minimal PETSCII/ATASCII control translation to ANSI/UTF-8 friendly bytes.
// This is not a full terminal emulator; it handles common controls so that
// xterm.js can render cursor movement, screen clear, reverse video and colors.

// translateLegacyControls translates PETSCII/ATASCII control bytes to ANSI.
func (c *Client) translateLegacyControls(data []byte) []byte {
    switch c.charset {
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
        switch b {
        // Mode switches (runtime)
        // Tab and Bell
        case 0x07: // BELL
            out = append(out, 0x07) // Pass through ASCII bell
            continue
        case 0x09: // TAB
            out = append(out, 0x09) // Pass through ASCII tab
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
        // Return handling: map PETSCII CR to CRLF when lone CR (xterm needs LF to advance)
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
        case 0x9B: // ATASCII EOL -> CRLF for terminal friendliness
            out = append(out, '\r', '\n')
            continue
        case 0x0C: // Form Feed as clear screen + home
            out = append(out, 0x1B, '[', '2', 'J', 0x1B, '[', 'H')
            continue
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
        // Backspace and Tab pass-through
        case 0x08, 0x09:
            out = append(out, b)
            continue
        default:
            // TODO: Map additional ATASCII controls (cursor, clear, inverse)
        }
        out = append(out, b)
    }
    return out
}
