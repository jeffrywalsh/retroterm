package main

import (
	"bytes"
	"log"
)

// ANSIEnhancedProcessor provides more comprehensive ANSI processing
type ANSIEnhancedProcessor struct {
	inSequence    bool
	sequenceBuffer []byte
	debugMode     bool
}

// NewANSIEnhancedProcessor creates a new enhanced processor
func NewANSIEnhancedProcessor(debug bool) *ANSIEnhancedProcessor {
	return &ANSIEnhancedProcessor{
		sequenceBuffer: make([]byte, 0, 256),
		debugMode:      debug,
	}
}

// ProcessANSIData processes data with enhanced ANSI handling
func (p *ANSIEnhancedProcessor) ProcessANSIData(data []byte) []byte {
    result := make([]byte, 0, len(data)*2) // Extra space for expansions
    
    for i := 0; i < len(data); i++ {
        b := data[i]
        
        // Normalize 8-bit C1 control codes to 7-bit ESC-prefixed sequences
        // Common mappings: CSI (0x9B) -> ESC '[', OSC (0x9D) -> ESC ']', DCS (0x90) -> ESC 'P', ST (0x9C) -> ESC '\\'
        if b >= 0x80 && b <= 0x9F {
            switch b {
            case 0x9B: // CSI
                p.inSequence = true
                p.sequenceBuffer = p.sequenceBuffer[:0]
                p.sequenceBuffer = append(p.sequenceBuffer, 0x1B, '[')
                continue
            case 0x9D: // OSC
                p.inSequence = true
                p.sequenceBuffer = p.sequenceBuffer[:0]
                p.sequenceBuffer = append(p.sequenceBuffer, 0x1B, ']')
                continue
            case 0x90: // DCS
                p.inSequence = true
                p.sequenceBuffer = p.sequenceBuffer[:0]
                p.sequenceBuffer = append(p.sequenceBuffer, 0x1B, 'P')
                continue
            case 0x9C: // ST (String Terminator)
                if p.inSequence {
                    p.sequenceBuffer = append(p.sequenceBuffer, 0x1B, '\\')
                    // Will be recognized as complete by isSequenceComplete for OSC; pass through
                    processed := p.processCompleteSequence()
                    result = append(result, processed...)
                    p.inSequence = false
                    p.sequenceBuffer = p.sequenceBuffer[:0]
                    continue
                }
                // Not in a sequence; emit ESC \
                result = append(result, 0x1B, '\\')
                continue
            }
        }

        // Handle special control characters
        switch b {
        case 0x0C: // Form Feed - clear screen and home cursor
            if p.debugMode {
                log.Printf("ANSI: Form feed detected, converting to ESC[2J ESC[H")
			}
			// Clear screen and move cursor to home
			result = append(result, 0x1B, '[', '2', 'J')  // Clear entire screen
			result = append(result, 0x1B, '[', 'H')       // Move cursor to home
			continue
			
		case 0x0E: // Shift Out - could be used for alternate character set
			// Pass through but log
			if p.debugMode {
				log.Printf("ANSI: Shift Out (0x0E) detected")
			}
			result = append(result, b)
			continue
			
		case 0x0F: // Shift In - return to normal character set
			// Pass through but log
			if p.debugMode {
				log.Printf("ANSI: Shift In (0x0F) detected")
			}
			result = append(result, b)
			continue
		}
		
		// Handle ANSI escape sequences
		if b == 0x1B { // ESC
			p.inSequence = true
			p.sequenceBuffer = p.sequenceBuffer[:0] // Reset buffer
			p.sequenceBuffer = append(p.sequenceBuffer, b)
			continue
		}
		
		if p.inSequence {
			p.sequenceBuffer = append(p.sequenceBuffer, b)
			
			// Check if sequence is complete
			if p.isSequenceComplete() {
				// Process the complete sequence
				processed := p.processCompleteSequence()
				result = append(result, processed...)
				p.inSequence = false
				p.sequenceBuffer = p.sequenceBuffer[:0]
			}
		} else {
			// Regular character
			result = append(result, b)
		}
	}
	
	// If we have an incomplete sequence at the end, append it as-is
	if len(p.sequenceBuffer) > 0 {
		result = append(result, p.sequenceBuffer...)
	}
	
	return result
}

// isSequenceComplete checks if the current sequence buffer contains a complete ANSI sequence
func (p *ANSIEnhancedProcessor) isSequenceComplete() bool {
	if len(p.sequenceBuffer) < 2 {
		return false
	}
	
	// Check the second character to determine sequence type
	if len(p.sequenceBuffer) >= 2 {
		switch p.sequenceBuffer[1] {
		case '[': // CSI sequence
			// Look for final byte (0x40-0x7E)
			for i := 2; i < len(p.sequenceBuffer); i++ {
				if p.sequenceBuffer[i] >= 0x40 && p.sequenceBuffer[i] <= 0x7E {
					return true
				}
			}
			
		case ']': // OSC sequence
			// Look for ST (ESC \ or BEL)
			for i := 2; i < len(p.sequenceBuffer); i++ {
				if p.sequenceBuffer[i] == 0x07 { // BEL
					return true
				}
				if i > 0 && p.sequenceBuffer[i-1] == 0x1B && p.sequenceBuffer[i] == '\\' {
					return true
				}
			}
			
		case '(', ')', '*', '+': // Character set selection
			return len(p.sequenceBuffer) >= 3
			
		case '7', '8':
			// DECSC/DECRC (Save/Restore cursor): ESC 7 / ESC 8
			return true

		case 'c', 'D', 'M', 'E':
			// Common single-char ESC sequences: RIS/IND/RI/NEL
			return true
			
		default:
			// Two-character sequences
			if p.sequenceBuffer[1] >= 0x40 && p.sequenceBuffer[1] <= 0x7F {
				return true
			}
		}
	}
	
	// Prevent buffer overflow - if sequence is too long, consider it complete
	if len(p.sequenceBuffer) > 100 {
		return true
	}
	
	return false
}

// processCompleteSequence processes a complete ANSI sequence
func (p *ANSIEnhancedProcessor) processCompleteSequence() []byte {
	// Check for specific sequences that need fixing
	
	// ESC[J without parameter should be ESC[0J (clear from cursor to end)
	if bytes.Equal(p.sequenceBuffer, []byte{0x1B, '[', 'J'}) {
		if p.debugMode {
			log.Printf("ANSI: Fixed ESC[J to ESC[0J")
		}
		return []byte{0x1B, '[', '0', 'J'}
	}
	
	// ESC[K without parameter should be ESC[0K (clear from cursor to end of line)
	if bytes.Equal(p.sequenceBuffer, []byte{0x1B, '[', 'K'}) {
		if p.debugMode {
			log.Printf("ANSI: Fixed ESC[K to ESC[0K")
		}
		return []byte{0x1B, '[', '0', 'K'}
	}
	
	// ESC[m without parameter should be ESC[0m (reset)
	if bytes.Equal(p.sequenceBuffer, []byte{0x1B, '[', 'm'}) {
		if p.debugMode {
			log.Printf("ANSI: Fixed ESC[m to ESC[0m")
		}
		return []byte{0x1B, '[', '0', 'm'}
	}
	
	// ESC[H without parameters should be ESC[1;1H (home)
	if bytes.Equal(p.sequenceBuffer, []byte{0x1B, '[', 'H'}) {
		// This is actually correct, but log it
		if p.debugMode {
			log.Printf("ANSI: Home cursor ESC[H")
		}
	}
	
    // Check for clear screen variations
    if len(p.sequenceBuffer) >= 4 && p.sequenceBuffer[0] == 0x1B && p.sequenceBuffer[1] == '[' {
        // ESC[2J - clear entire screen
        if bytes.Equal(p.sequenceBuffer, []byte{0x1B, '[', '2', 'J'}) || bytes.Equal(p.sequenceBuffer, []byte{0x1B, '[', '0', ';', '2', 'J'}) {
            if p.debugMode {
                log.Printf("ANSI: Clear screen ESC[2J (homing)")
            }
            // Home cursor after clear screen for ANSI.SYS compatibility
            return []byte{0x1B, '[', '2', 'J', 0x1B, '[', 'H'}
        }
        // Generic contains '2J'
        if bytes.Contains(p.sequenceBuffer, []byte{'2', 'J'}) {
            if p.debugMode {
                log.Printf("ANSI: Clear screen ESC[2J (homing)")
            }
            out := make([]byte, 0, len(p.sequenceBuffer)+3)
            out = append(out, p.sequenceBuffer...)
            out = append(out, 0x1B, '[', 'H')
            return out
        }
    }
	
	// Log unknown or interesting sequences in debug mode
	if p.debugMode && len(p.sequenceBuffer) > 2 {
		if p.sequenceBuffer[1] == '[' {
			// Extract the command character
			cmdChar := p.sequenceBuffer[len(p.sequenceBuffer)-1]
			switch cmdChar {
			case 'A', 'B', 'C', 'D': // Cursor movement
				// Common, don't log
			case 'm': // SGR
				// Common, don't log
			default:
				log.Printf("ANSI: Sequence %q", p.sequenceBuffer)
			}
		}
	}
	
	// Return sequence as-is if no fixes needed
	return p.sequenceBuffer
}

// InjectClearScreen injects a proper clear screen sequence
func (p *ANSIEnhancedProcessor) InjectClearScreen() []byte {
	if p.debugMode {
		log.Printf("ANSI: Injecting clear screen sequence")
	}
	// Clear screen, home cursor, reset attributes
	return []byte{
		0x1B, '[', '2', 'J',  // Clear entire screen
		0x1B, '[', 'H',       // Home cursor
		0x1B, '[', '0', 'm',  // Reset attributes
	}
}
