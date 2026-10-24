package main

import (
	"bytes"
)

// ANSIEnhancedProcessor provides more comprehensive ANSI processing
type ANSIEnhancedProcessor struct {
	inSequence    bool
	sequenceBuffer []byte
}

// NewANSIEnhancedProcessor creates a new enhanced processor
func NewANSIEnhancedProcessor(_ bool) *ANSIEnhancedProcessor {
	return &ANSIEnhancedProcessor{
		sequenceBuffer: make([]byte, 0, 256),
	}
}

// ProcessANSIData processes data with enhanced ANSI handling
func (p *ANSIEnhancedProcessor) ProcessANSIData(data []byte) []byte {
    result := make([]byte, 0, len(data)*2) // Extra space for expansions
    
    for i := 0; i < len(data); i++ {
        b := data[i]
        
        // Bytes 0x80-0x9F are NOT treated as 8-bit C1 controls: they are text in
        // every charset this runs for (CP437 É¢£¥, CP866 Р-Я, UTF-8 continuation
        // bytes, Big5/GBK). A probe of the whole directory found no BBS sending
        // 8-bit CSI, but 70 whose text contains these bytes.

        // Handle special control characters
        switch b {
        case 0x0C: // Form Feed - clear screen and home cursor
			// Clear screen and move cursor to home
			result = append(result, 0x1B, '[', '2', 'J')  // Clear entire screen
			result = append(result, 0x1B, '[', 'H')       // Move cursor to home
			continue
			
		case 0x0E: // Shift Out - could be used for alternate character set
			// Pass through but log
			result = append(result, b)
			continue
			
		case 0x0F: // Shift In - return to normal character set
			// Pass through but log
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
	
	// An incomplete sequence at the end stays buffered (inSequence) and is
	// emitted whole once the next read completes it. Emitting it here as well
	// sent its prefix twice.
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
		return []byte{0x1B, '[', '0', 'J'}
	}
	
	// ESC[K without parameter should be ESC[0K (clear from cursor to end of line)
	if bytes.Equal(p.sequenceBuffer, []byte{0x1B, '[', 'K'}) {
		return []byte{0x1B, '[', '0', 'K'}
	}
	
	// ESC[m without parameter should be ESC[0m (reset)
	if bytes.Equal(p.sequenceBuffer, []byte{0x1B, '[', 'm'}) {
		return []byte{0x1B, '[', '0', 'm'}
	}
	
	// ESC[H without parameters should be ESC[1;1H (home)
	if bytes.Equal(p.sequenceBuffer, []byte{0x1B, '[', 'H'}) {
		// This is actually correct, but log it
	}
	
    // Check for clear screen variations
    if len(p.sequenceBuffer) >= 4 && p.sequenceBuffer[0] == 0x1B && p.sequenceBuffer[1] == '[' {
        // ESC[2J - clear entire screen
        if bytes.Equal(p.sequenceBuffer, []byte{0x1B, '[', '2', 'J'}) || bytes.Equal(p.sequenceBuffer, []byte{0x1B, '[', '0', ';', '2', 'J'}) {
            // Home cursor after clear screen for ANSI.SYS compatibility
            return []byte{0x1B, '[', '2', 'J', 0x1B, '[', 'H'}
        }
        // Generic contains '2J'
        if bytes.Contains(p.sequenceBuffer, []byte{'2', 'J'}) {
            out := make([]byte, 0, len(p.sequenceBuffer)+3)
            out = append(out, p.sequenceBuffer...)
            out = append(out, 0x1B, '[', 'H')
            return out
        }
    }
	
	// Return sequence as-is if no fixes needed
	return p.sequenceBuffer
}

// InjectClearScreen injects a proper clear screen sequence
func (p *ANSIEnhancedProcessor) InjectClearScreen() []byte {
	// Clear screen, home cursor, reset attributes
	return []byte{
		0x1B, '[', '2', 'J',  // Clear entire screen
		0x1B, '[', 'H',       // Home cursor
		0x1B, '[', '0', 'm',  // Reset attributes
	}
}
