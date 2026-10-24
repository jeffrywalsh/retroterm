package main

// Regression tests for the decoding pipeline, mostly found by the directory
// probe (bbs_probe_test.go). These run offline and fast.

import (
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

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
func (f *cancelSends) Active() bool                        { return true }
func (f *cancelSends) Cancel()                             { f.c.sendToRemote("\x18\x18\x18") }

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

// decodeLegacy feeds data to a fresh session in reads of size step (0 for one
// read) and returns what the browser would get.
func decodeLegacy(charset string, data []byte, step int) string {
	c := &Client{}
	c.setCharsetAndAdjust(charset)
	c.outMu.Lock()
	c.preambleActive = false
	c.outMu.Unlock()
	if step == 0 {
		step = len(data)
	}
	var out strings.Builder
	for i := 0; i < len(data); i += step {
		end := min(i+step, len(data))
		out.Write(c.convertCharsetToOutput(data[i:end]))
	}
	return out.String()
}

func TestPETSCIIModeSwitchMidRead(t *testing.T) {
	// 0x61 is a graphic (♠) in upper/graphics mode and 'A' in lower/upper
	// mode. The whole read used to be decoded with the mode at its end.
	got := decodeLegacy("PETSCIIU", []byte{0x93, 0x61, 0x0E, 0x61, 0x8E, 0x61}, 0)
	if want := "\x1b[2J\x1b[H♠A♠"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLegacyASCIIBannerBeforeFirstLegacyByte(t *testing.T) {
	// Text before the board's first PETSCII byte stays readable ASCII (it is
	// sent before terminal detection); after it, lowercase is PETSCII graphics
	got := decodeLegacy("PETSCIIU", []byte("hi\r\x93hi"), 0)
	if want := "hi\r\n\x1b[2J\x1b[H\U0001FB74\u256e"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLegacyOutputIndependentOfReadSplits(t *testing.T) {
	streams := map[string][]byte{
		// Image BBS style: ASCII banner, a DSR query, colors, mode switches
		"PETSCIIU": []byte("13th floor bbs\r\n\x1b[6n\r\nroses are red\r\n\x07\x96 \xfb\xe4\x0eHello \x1b[1;3;5;7;46M\x05Wo\x8erld\r\n\x9b\x12\xa2\x92\r"),
		"PETSCIIL": []byte("\x93gIVE US A SECOND\r\n\x1c\x1b[31mRED\x1b[0m\r\n"),
		// ATASCII with ASCII CRLF, EOL and an ANSI sequence
		"ATASCII": []byte("Press RETURN\r\n}\x9b /\\ || \x9bline\r\n\x1b[1mbold\x1b[0m\x9b\x7e"),
	}
	for cs, data := range streams {
		whole := decodeLegacy(cs, data, 0)
		for _, step := range []int{1, 2, 3, 5, 16} {
			if got := decodeLegacy(cs, data, step); got != whole {
				t.Errorf("%s: %d-byte reads give %q, one read gives %q", cs, step, got, whole)
			}
		}
	}
}

func TestDeleteLineNotRewrittenAsSGR(t *testing.T) {
	// Only Image BBS's multi-parameter ESC[...M is a broken SGR
	got := string(normalizeCSISGRAny([]byte("\x1b[M\x1b[3M\x1b[1;3;5;7;46M")))
	if want := "\x1b[M\x1b[3M\x1b[1;3;5;7;46m"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// browserPair returns a Client whose browser socket is the server end of a
// real WebSocket, and the browser end to read from.
func browserPair(t *testing.T) (*Client, *websocket.Conn) {
	conns := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		conns <- conn
	}))
	t.Cleanup(srv.Close)
	browser, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { browser.Close() })
	return &Client{ws: <-conns}, browser
}

func TestLegacyPreambleShownWhenBoardGoesQuiet(t *testing.T) {
	// A banner with no clear/home, then silence waiting for a key, used to stay
	// buffered until the board sent something else
	c, browser := browserPair(t)
	c.setCharsetAndAdjust("PETSCIIU")
	c.outMu.Lock()
	c.renderToBrowser([]byte("PRESS RETURN"))
	c.outMu.Unlock()

	browser.SetReadDeadline(time.Now().Add(legacyPreambleTimeout + 2*time.Second))
	for {
		var msg Message
		if err := browser.ReadJSON(&msg); err != nil {
			t.Fatalf("banner never reached the browser: %v", err)
		}
		if msg.Type != "data" {
			continue
		}
		data, _ := base64.StdEncoding.DecodeString(msg.Data)
		if string(data) != "PRESS RETURN" {
			t.Fatalf("got %q", data)
		}
		return
	}
}

func TestZmodemCancelRacesCompletion(t *testing.T) {
	// Cancel (browser or disconnect), completion (rz exit) and the watchdog
	// run on different goroutines; cleanup must happen once. Run with -race.
	l := &LrzszReceiver{active: true, tempDir: t.TempDir()}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); l.Cancel() }()
	go func() { defer wg.Done(); l.completeTransfer() }()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			l.Active()
		}
	}()
	wg.Wait()
	if l.Active() {
		t.Fatal("transfer still active")
	}
}
