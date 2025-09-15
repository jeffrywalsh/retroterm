package main

// Minimal ANSI music detector for CSI |/M/N sequences.
// Detects sequences beginning with ESC [ ( '|' | 'M' | 'N' ) and consumes
// until a terminator: BEL (0x07), SO (0x0E), SI (0x0F), ST (ESC \), or the
// next ESC (which is presumed to start a new sequence). If a sequence spans
// chunks, it is buffered until the terminator arrives.

type AnsiMusicEmitter func(payload string)

type AnsiMusicProcessor struct {
    emit   AnsiMusicEmitter
    inSeq  bool
    buffer []byte // from ESC [ X ... (intro included)
}

func NewAnsiMusicProcessor(emit AnsiMusicEmitter) *AnsiMusicProcessor {
    return &AnsiMusicProcessor{emit: emit, buffer: make([]byte, 0, 256)}
}

// Process returns the input with any detected music sequences removed.
// The returned bool indicates whether any sequence was consumed.
func (p *AnsiMusicProcessor) Process(data []byte) ([]byte, bool) {
    if p == nil || len(data) == 0 {
        return data, false
    }

    consumed := false

    // If in the middle of a buffered sequence, append and try to finish
    if p.inSeq {
        p.buffer = append(p.buffer, data...)
        done, tail := p.tryEmitFromBuffer()
        if done {
            consumed = true
            // Process any trailing bytes recursively
            rem, more := p.Process(tail)
            return rem, consumed || more
        }
        // Still incomplete: suppress all
        return []byte{}, true
    }

    out := make([]byte, 0, len(data))
    i := 0
    for i < len(data) {
        b := data[i]
        if b == 0x1B && i+2 < len(data) && data[i+1] == '[' { // ESC [
            intro := data[i+2]
            if intro == '|' || intro == 'M' || intro == 'N' {
                // Flush non-music bytes before the introducer
                out = append(out, data[:i]...)
                // Search for terminator in remaining data
                j := i + 3
                term := -1
                termEsc := false
                for j < len(data) {
                    if data[j] == 0x07 || data[j] == 0x0E || data[j] == 0x0F { // BEL/SO/SI
                        term = j
                        break
                    }
                    if data[j] == 0x1B { // ESC
                        if j+1 < len(data) && data[j+1] == '\\' { // ST
                            term = j
                            termEsc = true
                            break
                        }
                        term = j // leave ESC for next parser
                        break
                    }
                    j++
                }
                if term != -1 {
                    payload := string(data[i+3 : term])
                    if p.emit != nil && len(payload) > 0 {
                        p.emit(payload)
                    }
                    // Continue parsing tail
                    var tail []byte
                    if termEsc {
                        if term+2 <= len(data) {
                            tail = data[term+2:]
                        }
                    } else if data[term] == 0x07 || data[term] == 0x0E || data[term] == 0x0F {
                        if term+1 <= len(data) {
                            tail = data[term+1:]
                        }
                    } else {
                        tail = data[term:]
                    }
                    data = tail
                    i = 0
                    consumed = true
                    continue
                }
                // No terminator found: buffer from introducer and mark inSeq
                p.buffer = p.buffer[:0]
                p.buffer = append(p.buffer, data[i:]...)
                p.inSeq = true
                consumed = true
                return out, consumed
            }
        }
        i++
    }
    // No music introducer found; pass through
    out = append(out, data...)
    return out, consumed
}

// tryEmitFromBuffer searches buffer for a terminator, emits payload if found,
// and returns (done, tail) where tail are bytes after the terminator.
func (p *AnsiMusicProcessor) tryEmitFromBuffer() (bool, []byte) {
    if !p.inSeq || len(p.buffer) < 3 {
        return false, nil
    }
    j := 3
    term := -1
    termEsc := false
    for j < len(p.buffer) {
        if p.buffer[j] == 0x07 || p.buffer[j] == 0x0E || p.buffer[j] == 0x0F {
            term = j
            break
        }
        if p.buffer[j] == 0x1B {
            if j+1 < len(p.buffer) && p.buffer[j+1] == '\\' {
                term = j
                termEsc = true
                break
            }
            term = j
            break
        }
        j++
    }
    if term == -1 {
        return false, nil
    }
    payload := string(p.buffer[3:term])
    if p.emit != nil && len(payload) > 0 {
        p.emit(payload)
    }
    var tail []byte
    if termEsc {
        if term+2 < len(p.buffer) {
            tail = append(tail, p.buffer[term+2:]...)
        }
    } else if p.buffer[term] == 0x07 || p.buffer[term] == 0x0E || p.buffer[term] == 0x0F {
        if term+1 < len(p.buffer) {
            tail = append(tail, p.buffer[term+1:]...)
        }
    } else {
        tail = append(tail, p.buffer[term:]...)
    }
    p.inSeq = false
    p.buffer = p.buffer[:0]
    return true, tail
}
