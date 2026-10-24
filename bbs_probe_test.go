package main

// Directory probe: connects to every BBS in bbs.csv, records the raw bytes it
// sends, and reports how cleanly the current decoders interpret them.
//
// Both tests are opt-in because they are slow and (for the first) hit the
// network:
//
//   BBS_PROBE=1 go test -run TestProbeDirectory -timeout 90m -v .
//       Connect to every BBS and write captures plus summary.csv.
//   BBS_PROBE_DIR=captures/probe-... go test -run TestProbeReport -v .
//       Rebuild summary.csv from existing captures with the current code,
//       no network. Use this to iterate on decoder changes.
//
// Knobs for TestProbeDirectory:
//   BBS_PROBE_OUT      output directory (default captures/probe-<timestamp>)
//   BBS_PROBE_FILTER   only probe entries whose name or host contains one of
//                      these comma-separated terms
//   BBS_PROBE_WORKERS  concurrent hosts (default 24; ports on one host run in turn)
//   BBS_PROBE_SECONDS  max session length per BBS (default 20)
//   BBS_PROBE_PROXY=1  dial through the proxy in config.json, like production

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// probeMeta extends the capture metadata format so the probe output works
// with the existing capture tooling (capture_analyzer, replay).
type probeMeta struct {
	CaptureMetadata
	Name     string `json:"name"`
	Software string `json:"software"`
	Location string `json:"location"`
	Error    string `json:"error,omitempty"`
	Nudges   int    `json:"nudges"`
}

func TestProbeDirectory(t *testing.T) {
	if os.Getenv("BBS_PROBE") != "1" {
		t.Skip("set BBS_PROBE=1 to connect to every BBS in bbs.csv")
	}
	entries, err := LoadBBSFromCSV("bbs.csv")
	if err != nil {
		t.Fatalf("load bbs.csv: %v", err)
	}
	if f := strings.ToLower(os.Getenv("BBS_PROBE_FILTER")); f != "" {
		var kept []BBSEntry
		for _, e := range entries {
			for _, term := range strings.Split(f, ",") {
				term = strings.TrimSpace(term)
				if term != "" && (strings.Contains(strings.ToLower(e.Name), term) || strings.Contains(strings.ToLower(e.Host), term)) {
					kept = append(kept, e)
					break
				}
			}
		}
		entries = kept
	}
	if os.Getenv("BBS_PROBE_PROXY") == "1" {
		if _, err := LoadConfig("config.json"); err != nil {
			t.Fatalf("load config.json for proxy: %v", err)
		}
	} else {
		AppConfig = nil // direct connections
	}
	out := os.Getenv("BBS_PROBE_OUT")
	if out == "" {
		out = filepath.Join("captures", "probe-"+time.Now().Format("20060102-150405"))
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	workers := envInt("BBS_PROBE_WORKERS", 24)
	session := time.Duration(envInt("BBS_PROBE_SECONDS", 20)) * time.Second

	// Answer DA/DSR/CPR like a terminal would; many BBSes use these to detect
	// ANSI, screen size and UTF-8, so CPR must report the real (tracked) cursor
	t.Setenv("TERM_ANSWERS", "true")
	t.Setenv("CURSOR_TRACK", "true")
	// Silence per-connection logging from the code under test
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	// One queue per host so multi-port hosts (single-line C64 boards) are not
	// hit with simultaneous calls
	byHost := map[string][]BBSEntry{}
	var hosts []string
	for _, e := range entries {
		h := strings.ToLower(e.Host)
		if _, ok := byHost[h]; !ok {
			hosts = append(hosts, h)
		}
		byHost[h] = append(byHost[h], e)
	}
	t.Logf("probing %d BBSes on %d hosts with %d workers, %s per session, output %s",
		len(entries), len(hosts), workers, session, out)

	jobs := make(chan string)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for h := range jobs {
				for _, e := range byHost[h] {
					meta := probeBBS(e, out, session)
					mu.Lock()
					done++
					status := "ok"
					if meta.Error != "" {
						status = meta.Error
					}
					fmt.Fprintf(os.Stderr, "[%d/%d] %s:%d %s (%d bytes)\n", done, len(entries), e.Host, e.Port, status, meta.BytesCaptured)
					mu.Unlock()
				}
			}
		}()
	}
	for _, h := range hosts {
		jobs <- h
	}
	close(jobs)
	wg.Wait()

	writeProbeReport(t, out)
}

func TestProbeReport(t *testing.T) {
	dir := os.Getenv("BBS_PROBE_DIR")
	if dir == "" {
		t.Skip("set BBS_PROBE_DIR to a probe output directory")
	}
	writeProbeReport(t, dir)
}

// probeBBS makes one session to a BBS the way the app would and saves the
// raw stream as <out>/<host>_<port>_<charset>.bin with a .json beside it.
func probeBBS(e BBSEntry, out string, session time.Duration) (meta probeMeta) {
	meta = probeMeta{
		CaptureMetadata: CaptureMetadata{
			StartTime: time.Now(),
			Host:      e.Host,
			Port:      e.Port,
			Protocol:  "telnet",
			Charset:   e.Encoding,
		},
		Name:     e.Name,
		Software: e.Software,
		Location: e.Location,
	}
	base := fmt.Sprintf("%s_%d_%s", probeFileSafe.ReplaceAllString(e.Host, "_"), e.Port, e.Encoding)
	meta.Filename = base + ".bin"
	var raw bytes.Buffer
	defer func() {
		meta.EndTime = time.Now()
		meta.BytesCaptured = int64(raw.Len())
		_ = os.WriteFile(filepath.Join(out, base+".bin"), raw.Bytes(), 0644)
		if js, err := json.MarshalIndent(meta, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(out, base+".json"), js, 0644)
		}
	}()

	conn, err := DialWithProxy("tcp", net.JoinHostPort(e.Host, strconv.Itoa(e.Port)))
	if err != nil {
		meta.Error = classifyDialError(err)
		return meta
	}
	defer conn.Close()

	c := &Client{done: make(chan bool, 1), cursorRow: 1, cursorCol: 1}
	c.setCharsetAndAdjust(e.Encoding)
	c.telnet = conn

	const maxBytes = 256 * 1024
	const idleNudge = 4 * time.Second
	// What a caller would try when the board waits: Enter, then Backspace
	// (C64 boards often detect PETSCII vs ASCII from the delete key)
	nudges := []string{"\r", "\x7f"}
	deadline := time.Now().Add(session)
	lastData := time.Now()
	buf := make([]byte, 8192)
	for time.Now().Before(deadline) && raw.Len() < maxBytes {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := conn.Read(buf)
		if n > 0 {
			raw.Write(buf[:n])
			lastData = time.Now()
			// Reply to telnet negotiation and terminal queries as readTelnet does
			if clean := c.processTelnetData(buf[:n]); len(clean) > 0 {
				answerQueriesInOrder(c, clean)
			}
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if time.Since(lastData) > idleNudge && len(nudges) > 0 {
					c.sendToRemote(nudges[0])
					nudges = nudges[1:]
					meta.Nudges++
					lastData = time.Now()
				}
				continue
			}
			if err != io.EOF && raw.Len() == 0 {
				meta.Error = "read: " + classifyDialError(err)
			}
			break
		}
	}
	if raw.Len() == 0 && meta.Error == "" {
		meta.Error = "no data"
	}
	return meta
}

// answerQueriesInOrder advances the cursor tracker up to each CPR request
// before answering it, so a board that prints and then asks (screen size and
// UTF-8 detection) gets the position a terminal would report at that moment.
func answerQueriesInOrder(c *Client, data []byte) {
	cpr := []byte{0x1B, '[', '6', 'n'}
	for len(data) > 0 {
		end := len(data)
		if k := bytes.Index(data, cpr); k >= 0 {
			end = k + len(cpr)
		}
		c.updateCursorFrom(data[:end])
		c.handleTerminalQueries(data[:end])
		data = data[end:]
	}
}

var probeFileSafe = regexp.MustCompile(`[^A-Za-z0-9-]+`)

func classifyDialError(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "no such host"):
		return "dns"
	case strings.Contains(s, "refused"):
		return "refused"
	case strings.Contains(s, "timeout"), strings.Contains(s, "i/o timeout"):
		return "timeout"
	case strings.Contains(s, "reset"):
		return "reset"
	case strings.Contains(s, "unreachable"):
		return "unreachable"
	}
	return s
}

// probeStats describes one capture: what the raw stream looks like and how
// cleanly the current pipeline decodes it with the directory's charset.
type probeStats struct {
	meta                          probeMeta
	textBytes, csi, high          int
	petscii, atasciiEOL, cyrillic int
	cyrillicWords                 int
	big5Bad, big5Distinct         int
	gbkBad, gbkDistinct           int
	crOnly, crlf                  int
	utf8Multi, utf8Bad            int
	fffd, c1, ignoredC0           int
	guess                         string
}

func analyzeProbe(meta probeMeta, raw []byte) probeStats {
	s := probeStats{meta: meta}
	c := &Client{ansiEnhanced: NewANSIEnhancedProcessor(false)}
	c.setCharsetAndAdjust(meta.Charset)
	c.outMu.Lock()
	c.preambleActive = false
	c.outMu.Unlock()
	text := c.processTelnetData(raw)
	s.textBytes = len(text)

	run := 0
	for i := 0; i < len(text); i++ {
		b := text[i]
		// CP866 letters come in words; CP437 accented letters rarely run 3+ deep
		if (b >= 0x80 && b <= 0xAF) || (b >= 0xE0 && b <= 0xF1) {
			run++
			if run == 3 {
				s.cyrillicWords++
			}
		} else {
			run = 0
		}
		switch {
		case b == 0x1B && i+1 < len(text) && text[i+1] == '[':
			// Count ANSI output, not terminal queries (ESC[6n, ESC[c): C64
			// boards like Image BBS and C-Base send those to detect callers
			j := i + 2
			for j < len(text) && (text[j] < 0x40 || text[j] > 0x7E) {
				j++
			}
			if j < len(text) && text[j] != 'n' && text[j] != 'c' {
				s.csi++
			}
		case b == 0x0D:
			if i+1 < len(text) && text[i+1] == 0x0A {
				s.crlf++
			} else {
				s.crOnly++
			}
		}
		// Strong PETSCII markers: clear, case switch, reverse, low color codes
		switch b {
		case 0x93, 0x0E, 0x8E, 0x12, 0x92, 0x05, 0x1C, 0x1E, 0x1F:
			s.petscii++
		case 0x9B:
			s.atasciiEOL++
		}
		if b >= 0x80 {
			s.high++
			// CP866 Cyrillic letters; CP437 uses these ranges for accented Latin,
			// while its art lives mostly in the shared 0xB0-0xDF box range
			if b <= 0xAF || (b >= 0xE0 && b <= 0xEF) {
				s.cyrillic++
			}
		}
	}
	utf8Seen := map[rune]bool{}
	for rest := text; len(rest) > 0; {
		r, size := utf8.DecodeRune(rest)
		if r == utf8.RuneError && size == 1 {
			if rest[0] >= 0x80 {
				s.utf8Bad++
			}
		} else if size > 1 && r != 0xFEFF { // Synchronet sends a BOM to test the terminal
			s.utf8Multi++
			utf8Seen[r] = true
		}
		rest = rest[size:]
	}
	// Big5/GBK: decode with each and count errors and distinct CJK ideographs.
	// CP437 box art often decodes "validly" as both, so only a one-sided win
	// with varied characters counts as Chinese text.
	cjkScore := func(cs string) (bad, distinct int) {
		seen := map[rune]bool{}
		for _, r := range string(newDBCSDecoder(cs).Decode(text)) {
			if r == utf8.RuneError {
				bad++
			} else if r >= 0x4E00 && r <= 0x9FFF {
				seen[r] = true
			}
		}
		return bad, len(seen)
	}
	s.big5Bad, s.big5Distinct = cjkScore("BIG5")
	s.gbkBad, s.gbkDistinct = cjkScore("GBK")
	cjkWins := func(bad, distinct, otherBad int) bool {
		return bad <= 1 && distinct >= 15 && otherBad >= 10 && otherBad > 5*(bad+1)
	}
	legacy := s.csi == 0 && (s.petscii > 5 || s.atasciiEOL > 3)

	// Decode exactly as readTelnet would for this charset
	processed := text
	cs := strings.ToUpper(c.charset)
	if cs != "PETSCIIU" && cs != "PETSCIIL" && cs != "ATASCII" {
		processed = c.ansiEnhanced.ProcessANSIData(text)
	}
	decoded := c.convertCharsetToOutput(processed)
	for _, r := range string(decoded) {
		switch {
		case r == utf8.RuneError:
			s.fffd++
		case r >= 0x80 && r <= 0x9F:
			// xterm.js acts on C1 controls, so these corrupt the screen
			s.c1++
		case r < 0x20 && r != '\r' && r != '\n' && r != '\t' && r != '\b' && r != 0x07 && r != 0x1B,
			r == 0x7F:
			// NUL (telnet CR NUL), ENQ, SI, ... are ignored by xterm.js: informational
			s.ignoredC0++
		}
	}

	switch {
	case s.textBytes < 16:
		s.guess = "unknown"
	case s.high == 0 && s.petscii == 0 && s.atasciiEOL == 0:
		// Plain ASCII (with or without ANSI) reads the same in every 8-bit charset
		s.guess = "ASCII"
	case legacy:
		// PETSCII and ATASCII art share most byte values, so bytes alone cannot
		// tell them apart reliably: keep the directory's legacy family, and
		// otherwise use ATASCII's clear (0x7D) and EOL (0x9B) as the hint
		switch {
		case isLegacyCharset(meta.Charset) && meta.Charset == "ATASCII":
			s.guess = "ATASCII"
		case isLegacyCharset(meta.Charset):
			s.guess = "PETSCII"
		case bytes.IndexByte(text, 0x7D) >= 0 && s.atasciiEOL > s.crOnly:
			s.guess = "ATASCII"
		default:
			s.guess = "PETSCII"
		}
	case cjkWins(s.big5Bad, s.big5Distinct, s.gbkBad):
		s.guess = "BIG5"
	case cjkWins(s.gbkBad, s.gbkDistinct, s.big5Bad):
		s.guess = "GBK"
	case s.utf8Bad == 0 && s.utf8Multi >= 20 && len(utf8Seen) >= 3:
		s.guess = "UTF-8"
	case s.cyrillicWords >= 5:
		s.guess = "CP866"
	default:
		s.guess = "CP437"
	}
	return s
}

func (s probeStats) mismatch() bool {
	dir := s.meta.Charset
	if s.guess == "unknown" || s.guess == "ASCII" || s.meta.Error != "" {
		return false
	}
	if s.guess == "PETSCII" {
		return dir != "PETSCIIU" && dir != "PETSCIIL"
	}
	return s.guess != dir
}

// writeProbeReport analyzes every capture in dir with the current code and
// writes summary.csv plus a decoded .out per capture (cat it in a terminal).
func writeProbeReport(t *testing.T, dir string) {
	metas, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var all []probeStats
	for _, mp := range metas {
		var meta probeMeta
		js, err := os.ReadFile(mp)
		if err != nil || json.Unmarshal(js, &meta) != nil {
			continue
		}
		raw, _ := os.ReadFile(strings.TrimSuffix(mp, ".json") + ".bin")
		s := analyzeProbe(meta, raw)
		all = append(all, s)
		if len(raw) > 0 {
			c := &Client{ansiEnhanced: NewANSIEnhancedProcessor(false)}
			c.setCharsetAndAdjust(meta.Charset)
			c.outMu.Lock()
			c.preambleActive = false
			c.outMu.Unlock()
			text := c.processTelnetData(raw)
			cs := strings.ToUpper(c.charset)
			if cs != "PETSCIIU" && cs != "PETSCIIL" && cs != "ATASCII" {
				text = c.ansiEnhanced.ProcessANSIData(text)
			}
			decoded := c.convertCharsetToOutput(text)
			_ = os.WriteFile(strings.TrimSuffix(mp, ".json")+".out", decoded, 0644)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].meta.Name < all[j].meta.Name })

	f, err := os.Create(filepath.Join(dir, "summary.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"name", "host", "port", "software", "dir_encoding", "status", "raw_bytes",
		"text_bytes", "ansi_csi", "high_bytes", "petscii_ctrl", "atascii_eol", "cyrillic_bytes", "cyrillic_words",
		"cr_only", "crlf", "utf8_multibyte", "utf8_invalid", "decoded_fffd", "decoded_c1", "decoded_ignored_c0",
		"guess", "mismatch"})
	status := map[string]int{}
	mismatches := map[string]int{}
	unclean := map[string]int{}
	for _, s := range all {
		st := "ok"
		if s.meta.Error != "" {
			st = s.meta.Error
		}
		status[st]++
		if s.mismatch() {
			mismatches[s.meta.Charset+" -> "+s.guess]++
		}
		if st == "ok" && (s.fffd > 0 || s.c1 > 0) {
			unclean[s.meta.Charset]++
		}
		_ = w.Write([]string{s.meta.Name, s.meta.Host, strconv.Itoa(s.meta.Port), s.meta.Software,
			s.meta.Charset, st, strconv.FormatInt(s.meta.BytesCaptured, 10), strconv.Itoa(s.textBytes),
			strconv.Itoa(s.csi), strconv.Itoa(s.high), strconv.Itoa(s.petscii), strconv.Itoa(s.atasciiEOL),
			strconv.Itoa(s.cyrillic), strconv.Itoa(s.cyrillicWords), strconv.Itoa(s.crOnly), strconv.Itoa(s.crlf), strconv.Itoa(s.utf8Multi),
			strconv.Itoa(s.utf8Bad), strconv.Itoa(s.fffd), strconv.Itoa(s.c1), strconv.Itoa(s.ignoredC0), s.guess,
			strconv.FormatBool(s.mismatch())})
	}
	w.Flush()

	t.Logf("%d captures analyzed, summary at %s", len(all), filepath.Join(dir, "summary.csv"))
	t.Logf("connection status: %v", status)
	t.Logf("decoded with visible damage (U+FFFD or C1 controls), by directory encoding: %v", unclean)
	t.Logf("directory encoding vs detected: %v", mismatches)
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}
