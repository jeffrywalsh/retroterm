package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsExposition(t *testing.T) {
	board := boardKey{`Board "Q"`, "CP437"}
	metricConnect("ok", 3*time.Second, board)
	metricConnect("failed", 0, boardKey{})
	metricConnect("blocked", 0, boardKey{})
	metricBytes(100, 7)
	metricTransfer("ok", 4096)
	metricSessionEnd(90*time.Second, board)
	metricSessionEnd(30*time.Second, board)

	rec := httptest.NewRecorder()
	handleMetrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	out := rec.Body.String()
	for _, want := range []string{
		`retroterm_board_connects_total{board="Board \"Q\"",charset="CP437"} 1`,
		`retroterm_connect_seconds_bucket{le="2"} 0`,
		`retroterm_connect_seconds_bucket{le="5"} 1`,
		`retroterm_connect_seconds_bucket{le="+Inf"} 1`,
		`retroterm_session_seconds_bucket{le="60"} 1`,
		`retroterm_session_seconds_bucket{le="300"} 2`,
		`retroterm_board_session_seconds_sum{board="Board \"Q\"",charset="CP437"} 120`,
		`retroterm_board_session_seconds_count{board="Board \"Q\"",charset="CP437"} 2`,
		`retroterm_board_last_session_seconds{board="Board \"Q\"",charset="CP437"} 30`,
		`retroterm_zmodem_bytes_total 4096`,
		"# TYPE retroterm_sessions gauge",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Every sample line is "name{labels} value" or "name value"
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "# ") {
			continue
		}
		if i := strings.LastIndexByte(line, ' '); i <= 0 || i == len(line)-1 {
			t.Errorf("malformed sample %q", line)
		}
	}
}
