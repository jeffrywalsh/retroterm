package main

// Regression tests for the decoding pipeline, mostly found by the directory
// probe (bbs_probe_test.go). These run offline and fast.

import (
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

func TestBig5HalfColoredCharacterAcrossReads(t *testing.T) {
	enc, _ := traditionalchinese.Big5.NewEncoder().Bytes([]byte("歡迎"))
	d := newDBCSDecoder("BIG5")
	// Lead byte, then a color change, then the trail byte in the next read:
	// how Taiwanese boards color the two halves of one character
	got := string(d.Decode(append([]byte{enc[0]}, "\x1b[1;33m"...))) + string(d.Decode(enc[1:]))
	if want := "\x1b[1;33m歡迎"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGBKSplitAcrossReads(t *testing.T) {
	enc, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("欢迎光临"))
	d := newDBCSDecoder("GBK")
	got := string(d.Decode(enc[:3])) + string(d.Decode(enc[3:]))
	if got != "欢迎光临" {
		t.Fatalf("got %q", got)
	}
}

func TestDBCSInputEncoding(t *testing.T) {
	want, _ := traditionalchinese.Big5.NewEncoder().Bytes([]byte("你好"))
	if got := ConvertUTF8ToDBCS("BIG5", "你好"); string(got) != string(want) {
		t.Fatalf("got %x, want %x", got, want)
	}
}

func TestHighBytesAreTextNotC1Controls(t *testing.T) {
	// CP866 Р Ы Ь Э are 0x90 0x9B 0x9C 0x9D; the ANSI processor used to turn
	// them into DCS/CSI/ST/OSC escape sequences
	p := NewANSIEnhancedProcessor(false)
	in := []byte{0x90, 0x9B, 0x9C, 0x9D}
	if got := ConvertCP866ToUTF8(p.ProcessANSIData(in)); got != "РЫЬЭ" {
		t.Fatalf("CP866 got %q", got)
	}
	// UTF-8 ═ (E2 95 90) contains 0x90
	if got := string(NewANSIEnhancedProcessor(false).ProcessANSIData([]byte("═"))); got != "═" {
		t.Fatalf("UTF-8 got %q", got)
	}
}

func TestANSISequenceSplitAcrossReadsNotDuplicated(t *testing.T) {
	p := NewANSIEnhancedProcessor(false)
	got := string(p.ProcessANSIData([]byte("A\x1b[3"))) + string(p.ProcessANSIData([]byte("1mB")))
	if got != "A\x1b[31mB" {
		t.Fatalf("got %q", got)
	}
}

func TestPETSCIIKeepsANSISequences(t *testing.T) {
	// '[' used to end the sequence, so 'm' went through the PETSCII table
	// (ESC[31m became ESC[31M, delete 31 lines, in lowercase mode)
	if got := ConvertPETSCIILToUTF8([]byte("\x1b[31m\x1b[A")); got != "\x1b[31m\x1b[A" {
		t.Fatalf("got %q", got)
	}
}

func TestPETSCIICRAndLFAcrossReads(t *testing.T) {
	c := &Client{}
	c.setCharsetAndAdjust("PETSCIIU")
	// C64 boards end lines with bare CR; a CRLF split across reads must not
	// produce two newlines
	got := string(c.translatePETSCIIToANSI([]byte("A\r"))) + string(c.translatePETSCIIToANSI([]byte("\nB\r")))
	if got != "A\r\nB\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestPETSCIIBellShiftReturnAndUndefinedCodes(t *testing.T) {
	// Image BBS rings BEL at its delete-key prompt; it used to decode as U+FFFD.
	// 0x8D is SHIFT-RETURN, 0x94 INSERT, 0x85 a function key (not displayed).
	c := &Client{}
	c.setCharsetAndAdjust("PETSCIIU")
	got := ConvertPETSCIIUToUTF8(c.translatePETSCIIToANSI([]byte{'A', 0x07, 0x8D, 0x94, 0x85, 'B'}))
	if got != "A\a\r\n\x1b[@B" {
		t.Fatalf("got %q", got)
	}
}

func TestATASCIIControlsSurviveGlyphMapping(t *testing.T) {
	// Backspace 0x7E, tab 0x7F, bell 0xFD used to render as ◢ ▗ ╲ glyphs
	got := ConvertATASCIIToUTF8(translateATASCIIToANSI([]byte{'A', 0x7E, 0x7F, 0xFD, 'B'}))
	if got != "A\x1b[D \x1b[D\t\aB" {
		t.Fatalf("got %q", got)
	}
}

func TestLegacySessionsDoNotAnswerTerminalQueries(t *testing.T) {
	c := &Client{}
	c.setCharsetAndAdjust("PETSCIIU")
	out := string(c.translateLegacyControls([]byte("X\x1b[6n\x1b[0c\x1bZY")))
	if strings.Contains(out, "\x1b[6n") || strings.Contains(out, "c") || strings.Contains(out, "\x1bZ") {
		t.Fatalf("queries reached the browser: %q", out)
	}
}

func TestTelnetSubnegotiationAnswered(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := &Client{}
	c.setCharsetAndAdjust("PETSCIIU")
	c.telnet = a
	reply := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 64)
		b.SetReadDeadline(time.Now().Add(time.Second))
		n, _ := b.Read(buf)
		reply <- buf[:n]
	}()
	// IAC SB TTYPE SEND IAC SE used to be parsed as a 3-byte command, leaking
	// 0x01 to the screen, eating the next byte and never answering
	clean := c.processTelnetData([]byte("\xff\xfa\x18\x01\xff\xf0\r\nHI"))
	if string(clean) != "\r\nHI" {
		t.Fatalf("screen got %q", clean)
	}
	if got := string(<-reply); got != "\xff\xfa\x18\x00PETSCII\xff\xf0" {
		t.Fatalf("reply got %q", got)
	}
}

// cancelSends mirrors LrzszReceiver.Cancel during a transfer: it writes the
// abort sequence through sendToRemote, which takes c.mu.
type cancelSends struct{ c *Client }

func (f *cancelSends) ProcessData(d []byte) ([]byte, bool) { return d, false }
func (f *cancelSends) Active() bool                         { return true }
func (f *cancelSends) Cancel()                              { f.c.sendToRemote("\x18\x18\x18") }

func TestDisconnectDuringTransferDoesNotDeadlock(t *testing.T) {
	c := &Client{done: make(chan bool, 1), charset: "CP437"}
	c.zmodemReceiver = &cancelSends{c}
	finished := make(chan struct{})
	go func() { c.disconnect(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect() deadlocked")
	}
}
