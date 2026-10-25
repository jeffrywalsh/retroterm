// Package main - metrics.go
// Prometheus metrics, written in its text format by hand so the server needs
// no client library. They're served on their own listener (METRICS_ADDR), not
// the public port, so they never reach the internet through the tunnel.
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// histogram is a cumulative Prometheus histogram.
type histogram struct {
	bounds []float64
	counts []uint64 // per bucket, not cumulative; the last is +Inf
	sum    float64
	count  uint64
}

func newHistogram(bounds ...float64) *histogram {
	return &histogram{bounds: bounds, counts: make([]uint64, len(bounds)+1)}
}

func (h *histogram) observe(v float64) {
	i := sort.SearchFloat64s(h.bounds, v)
	h.counts[i]++
	h.sum += v
	h.count++
}

func (h *histogram) write(w io.Writer, name string) {
	var cum uint64
	for i, b := range h.bounds {
		cum += h.counts[i]
		fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", name, b, cum)
	}
	fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", name, h.count)
	fmt.Fprintf(w, "%s_sum %g\n%s_count %d\n", name, h.sum, name, h.count)
}

// boardKey labels per-board counters.
type boardKey struct{ board, charset string }

var metrics = struct {
	mu             sync.Mutex
	browsers       int
	sessions       int
	connects       map[string]uint64   // by result: ok, failed, blocked
	boardConnects  map[boardKey]uint64 // successful connects
	bytesFromBBS   uint64
	bytesToBBS     uint64
	transfers      map[string]uint64 // ZMODEM, by result: ok, cancelled
	transferBytes  uint64
	connectSeconds *histogram
	sessionSeconds *histogram
}{
	connects:       map[string]uint64{},
	boardConnects:  map[boardKey]uint64{},
	transfers:      map[string]uint64{},
	connectSeconds: newHistogram(0.25, 0.5, 1, 2, 5, 10, 20, 30, 45, 60, 90),
	sessionSeconds: newHistogram(10, 30, 60, 300, 600, 1800, 3600, 7200, 14400),
}

func metricBrowser(delta int) {
	metrics.mu.Lock()
	metrics.browsers += delta
	metrics.mu.Unlock()
}

// metricConnect records a connection attempt; took, board and charset only
// matter for "ok".
func metricConnect(result string, took time.Duration, board, charset string) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.connects[result]++
	if result == "ok" {
		metrics.sessions++
		metrics.boardConnects[boardKey{board, charset}]++
		metrics.connectSeconds.observe(took.Seconds())
	}
}

func metricSessionEnd(lasted time.Duration) {
	metrics.mu.Lock()
	metrics.sessions--
	metrics.sessionSeconds.observe(lasted.Seconds())
	metrics.mu.Unlock()
}

func metricBytes(fromBBS, toBBS int) {
	metrics.mu.Lock()
	metrics.bytesFromBBS += uint64(fromBBS)
	metrics.bytesToBBS += uint64(toBBS)
	metrics.mu.Unlock()
}

func metricTransfer(result string, bytes int) {
	metrics.mu.Lock()
	metrics.transfers[result]++
	metrics.transferBytes += uint64(bytes)
	metrics.mu.Unlock()
}

// boardName returns the directory name of the board at host:port, or the
// address itself if it isn't listed.
func boardName(host string, port int) string {
	for _, b := range ApprovedBBSList {
		if strings.EqualFold(b.Host, host) && b.Port == port {
			return b.Name
		}
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// labelValue escapes a Prometheus label value.
func labelValue(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	help := func(name, typ, text string) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, text, name, typ)
	}
	help("retroterm_build_info", "gauge", "The running build; its date is the footer's \"Updated\".")
	fmt.Fprintf(w, "retroterm_build_info{date=\"%s\"} 1\n", labelValue(siteUpdated()))
	help("retroterm_directory_boards", "gauge", "Boards in the directory (bbs.csv).")
	fmt.Fprintf(w, "retroterm_directory_boards %d\n", len(ApprovedBBSList))
	help("retroterm_browsers", "gauge", "Open browser WebSockets.")
	fmt.Fprintf(w, "retroterm_browsers %d\n", metrics.browsers)
	help("retroterm_sessions", "gauge", "Connected BBS sessions.")
	fmt.Fprintf(w, "retroterm_sessions %d\n", metrics.sessions)

	help("retroterm_connects_total", "counter", "BBS connection attempts by result: ok, failed (dial or proxy error) or blocked (not in the directory).")
	for _, res := range []string{"ok", "failed", "blocked"} {
		fmt.Fprintf(w, "retroterm_connects_total{result=\"%s\"} %d\n", res, metrics.connects[res])
	}
	help("retroterm_board_connects_total", "counter", "Successful connections per board.")
	keys := make([]boardKey, 0, len(metrics.boardConnects))
	for k := range metrics.boardConnects {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].board < keys[j].board })
	for _, k := range keys {
		fmt.Fprintf(w, "retroterm_board_connects_total{board=\"%s\",charset=\"%s\"} %d\n",
			labelValue(k.board), labelValue(k.charset), metrics.boardConnects[k])
	}
	help("retroterm_connect_seconds", "histogram", "Time to open a successful BBS connection (through Tor when the proxy is on).")
	metrics.connectSeconds.write(w, "retroterm_connect_seconds")
	help("retroterm_session_seconds", "histogram", "How long BBS sessions lasted.")
	metrics.sessionSeconds.write(w, "retroterm_session_seconds")

	help("retroterm_bbs_bytes_total", "counter", "Bytes between the server and BBSes.")
	fmt.Fprintf(w, "retroterm_bbs_bytes_total{direction=\"from_bbs\"} %d\n", metrics.bytesFromBBS)
	fmt.Fprintf(w, "retroterm_bbs_bytes_total{direction=\"to_bbs\"} %d\n", metrics.bytesToBBS)

	help("retroterm_zmodem_transfers_total", "counter", "ZMODEM downloads by result.")
	for _, res := range []string{"ok", "cancelled"} {
		fmt.Fprintf(w, "retroterm_zmodem_transfers_total{result=\"%s\"} %d\n", res, metrics.transfers[res])
	}
	help("retroterm_zmodem_bytes_total", "counter", "Bytes of files delivered to browsers by ZMODEM.")
	fmt.Fprintf(w, "retroterm_zmodem_bytes_total %d\n", metrics.transferBytes)

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	help("go_goroutines", "gauge", "Goroutines.")
	fmt.Fprintf(w, "go_goroutines %d\n", runtime.NumGoroutine())
	help("go_memstats_heap_alloc_bytes", "gauge", "Heap bytes in use.")
	fmt.Fprintf(w, "go_memstats_heap_alloc_bytes %d\n", ms.HeapAlloc)
	help("go_memstats_sys_bytes", "gauge", "Bytes obtained from the OS.")
	fmt.Fprintf(w, "go_memstats_sys_bytes %d\n", ms.Sys)
}

// serveMetrics serves /metrics on addr (from METRICS_ADDR) if it's set.
func serveMetrics(addr string) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", handleMetrics)
	fmt.Printf("Metrics: http://%s/metrics\n", addr)
	go func() { log.Printf("metrics listener: %v", http.ListenAndServe(addr, mux)) }()
}
