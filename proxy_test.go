package main

import (
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSOCKS is a minimal SOCKS5 server with username/password auth. Its
// handle func sees the username and returns the reply code (0 = success,
// -1 = never answer, to simulate a stalled circuit).
func fakeSOCKS(t *testing.T, handle func(user string) int) (addr string, users func() []string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	var seen []string
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 512)
				// greeting: VER NMETHODS METHODS...
				if _, err := io.ReadFull(c, buf[:2]); err != nil {
					return
				}
				io.ReadFull(c, buf[:buf[1]])
				c.Write([]byte{5, 2}) // username/password
				// auth: VER ULEN UNAME PLEN PASSWD
				io.ReadFull(c, buf[:2])
				ulen := int(buf[1])
				io.ReadFull(c, buf[:ulen])
				user := string(buf[:ulen])
				io.ReadFull(c, buf[:1])
				io.ReadFull(c, buf[:buf[0]])
				c.Write([]byte{1, 0})
				// request: VER CMD RSV ATYP(3) LEN HOST PORT
				io.ReadFull(c, buf[:5])
				io.ReadFull(c, buf[:int(buf[4])+2])
				mu.Lock()
				seen = append(seen, user)
				mu.Unlock()
				code := handle(user)
				if code < 0 {
					time.Sleep(2 * time.Second)
					return
				}
				c.Write([]byte{5, byte(code), 0, 1, 0, 0, 0, 0, 0, 0})
				if code == 0 {
					c.Write([]byte("hello"))
					time.Sleep(100 * time.Millisecond)
				}
			}(c)
		}
	}()
	return ln.Addr().String(), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func useTorProxy(t *testing.T, addr string) {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	cfg := &Config{}
	cfg.Proxy.Enabled = true
	cfg.Proxy.Type = "tor"
	cfg.Proxy.Host = host
	var p int
	for _, ch := range port {
		p = p*10 + int(ch-'0')
	}
	cfg.Proxy.Port = p
	old, oldTimeout := AppConfig, torAttemptTimeout
	AppConfig, torAttemptTimeout = cfg, 300*time.Millisecond
	t.Cleanup(func() { AppConfig, torAttemptTimeout = old, oldTimeout })
}

func TestTorRetriesOnNewCircuit(t *testing.T) {
	calls := 0
	addr, users := fakeSOCKS(t, func(string) int {
		calls++
		switch calls {
		case 1:
			return 4 // host unreachable
		case 2:
			return -1 // stall
		}
		return 0
	})
	useTorProxy(t, addr)

	conn, err := DialWithProxy("tcp", "board.example:23")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	got := make([]byte, 5)
	if _, err := io.ReadFull(conn, got); err != nil || string(got) != "hello" {
		t.Fatalf("read %q, %v", got, err)
	}
	u := users()
	if len(u) != 3 {
		t.Fatalf("want 3 attempts, got %d", len(u))
	}
	if u[0] == u[1] || u[1] == u[2] || u[0] == u[2] {
		t.Errorf("attempts reused a SOCKS username (same circuit): %v", u)
	}
}

func TestTorGivesUpAfterLimit(t *testing.T) {
	addr, users := fakeSOCKS(t, func(string) int { return 5 }) // refused
	useTorProxy(t, addr)
	if _, err := DialWithProxy("tcp", "board.example:23"); err == nil {
		t.Fatal("expected failure")
	}
	if n := len(users()); n != torAttempts {
		t.Errorf("want %d attempts, got %d", torAttempts, n)
	}
}

func TestTorProxyDownNoRetry(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close() // nothing listens here now
	useTorProxy(t, addr)
	start := time.Now()
	_, err := DialWithProxy("tcp", "board.example:23")
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Errorf("unexpected error %v", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("retried against a dead proxy")
	}
}

func TestDialFailReason(t *testing.T) {
	cases := map[string]string{
		"socks connect tcp x->y: unknown error host unreachable":                  "unreachable",
		"socks connect tcp x->y: unknown error connection refused":                "refused",
		"socks connect tcp x->y: unknown error general SOCKS server failure":      "general",
		"socks connect tcp x->y: unknown error connection not allowed by ruleset": "exit_policy",
		"socks connect tcp x->y: unknown error TTL expired":                       "timeout",
		"socks connect tcp x->y: read tcp a->b: i/o timeout":                      "timeout",
	}
	for msg, want := range cases {
		if got := dialFailReason(errors.New(msg)); got != want {
			t.Errorf("%q: got %s, want %s", msg, got, want)
		}
	}
	if got := dialFailReason(nil); got != "ok" {
		t.Errorf("nil: got %s", got)
	}
	down := &net.OpError{Op: "connect", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}
	if got := dialFailReason(down); got != "proxy_down" {
		t.Errorf("proxy down: got %s", got)
	}
	stalled := &net.OpError{Op: "connect", Err: &net.OpError{Op: "read", Err: errors.New("i/o timeout")}}
	if got := dialFailReason(stalled); got != "timeout" {
		t.Errorf("stalled handshake: got %s", got)
	}
}
