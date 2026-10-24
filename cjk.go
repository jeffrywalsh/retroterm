package main

// Big5 (Taiwan) and GBK (mainland China) support. Both are double-byte
// charsets that are ASCII-compatible: bytes below 0x80 are ASCII, and a lead
// byte 0x81-0xFE starts a two-byte character.
//
// Taiwanese BBSes color the two halves of one character separately, so an
// ANSI sequence can sit between the lead and trail byte (ESC[1;33m between
// them). The decoder holds the lead byte across ANSI sequences and across
// reads, passes the sequences through, and joins the character afterwards.

import (
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// ISO-8859-1 is what Amiga boards draw with (Topaz font: ¯ ¬ © ·). Each byte
// is the code point of the same value.
func ConvertLatin1ToUTF8(data []byte) []byte {
	out := make([]byte, 0, len(data)+len(data)/4)
	for _, b := range data {
		out = append(out, string(rune(b))...)
	}
	return out
}

func ConvertUTF8ToLatin1(s string) []byte {
	out, err := encoding.ReplaceUnsupported(charmap.ISO8859_1.NewEncoder()).Bytes([]byte(s))
	if err != nil {
		return []byte(s)
	}
	return out
}

func dbcsEncoding(charset string) encoding.Encoding {
	switch charset {
	case "BIG5":
		return traditionalchinese.Big5
	case "GBK":
		return simplifiedchinese.GBK
	}
	return nil
}

func isDBCSCharset(charset string) bool {
	return dbcsEncoding(charset) != nil
}

// dbcsDecoder is stateful and belongs to one connection's read loop.
type dbcsDecoder struct {
	charset string
	dec     *encoding.Decoder
	lead    byte   // pending lead byte, 0 when none
	esc     []byte // escape sequence in progress
}

func newDBCSDecoder(charset string) *dbcsDecoder {
	return &dbcsDecoder{charset: charset, dec: dbcsEncoding(charset).NewDecoder()}
}

// Decode converts one read of Big5/GBK bytes to UTF-8.
func (d *dbcsDecoder) Decode(data []byte) []byte {
	out := make([]byte, 0, len(data)*3/2)
	for _, b := range data {
		if len(d.esc) > 0 {
			d.esc = append(d.esc, b)
			if escComplete(d.esc) {
				out = append(out, d.esc...)
				d.esc = d.esc[:0]
			}
			continue
		}
		if b == 0x1B {
			d.esc = append(d.esc, b)
			continue
		}
		if d.lead != 0 {
			pair := []byte{d.lead, b}
			d.lead = 0
			if b >= 0x40 && b != 0x7F {
				if r, err := d.dec.Bytes(pair); err == nil {
					out = append(out, r...)
					continue
				}
			}
			// Invalid trail: drop the orphan lead, keep the byte if it is ASCII
			out = append(out, "�"...)
			if b < 0x80 {
				out = append(out, b)
			}
			continue
		}
		if b >= 0x81 && b <= 0xFE {
			d.lead = b
			continue
		}
		out = append(out, b)
	}
	return out
}

// escComplete reports whether seq (starting with ESC) is a complete sequence.
func escComplete(seq []byte) bool {
	if len(seq) < 2 {
		return false
	}
	if seq[1] != '[' {
		return true // two-byte ESC sequence
	}
	for _, c := range seq[2:] {
		if c >= 0x40 && c <= 0x7E {
			return true
		}
	}
	return len(seq) > 64 // runaway sequence: flush it
}

// ConvertUTF8ToDBCS encodes keyboard input for a Big5/GBK BBS; characters
// the charset cannot represent become the encoding's replacement byte.
func ConvertUTF8ToDBCS(charset string, s string) []byte {
	enc := encoding.ReplaceUnsupported(dbcsEncoding(charset).NewEncoder())
	out, err := enc.Bytes([]byte(s))
	if err != nil {
		return []byte(s)
	}
	return out
}
