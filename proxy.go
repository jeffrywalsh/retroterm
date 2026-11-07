package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

var (
	// Tor gets a few short tries on fresh circuits rather than one long one:
	// a bad exit usually fails or stalls, and a different exit usually works.
	torAttempts       = 3
	torAttemptTimeout = 12 * time.Second
	dialTimeout       = 10 * time.Second
)

// socksDialer builds a SOCKS5 dialer for the configured proxy. For Tor, each
// call gets a random username/password: Tor isolates streams by SOCKS auth
// (IsolateSOCKSAuth is on by default), so every attempt, and every session,
// leaves through its own circuit and exit IP.
func socksDialer() (proxy.ContextDialer, error) {
	p := AppConfig.Proxy
	var auth *proxy.Auth
	switch {
	case p.Type == "tor":
		auth = &proxy.Auth{User: randomToken(), Password: randomToken()}
	case p.Username != "":
		auth = &proxy.Auth{User: p.Username, Password: p.Password}
	}
	d, err := proxy.SOCKS5("tcp", fmt.Sprintf("%s:%d", p.Host, p.Port), auth, &net.Dialer{Timeout: dialTimeout})
	if err != nil {
		return nil, fmt.Errorf("failed to create SOCKS5 dialer: %v", err)
	}
	return d.(proxy.ContextDialer), nil
}

func randomToken() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// DialWithProxy opens a connection to address, through the SOCKS5 proxy when
// one is configured. Through Tor it retries on a new circuit after a failure
// or a stall. Every attempt is logged and counted by result and reason.
func DialWithProxy(network, address string) (net.Conn, error) {
	if AppConfig == nil || !AppConfig.Proxy.Enabled {
		conn, err := net.DialTimeout(network, address, dialTimeout)
		metricDialAttempt(dialFailReason(err))
		return conn, err
	}

	attempts, timeout := 1, dialTimeout
	if AppConfig.Proxy.Type == "tor" {
		attempts, timeout = torAttempts, torAttemptTimeout
	}

	var err error
	for i := 1; i <= attempts; i++ {
		var d proxy.ContextDialer
		if d, err = socksDialer(); err != nil {
			return nil, err
		}
		log.Printf("PROXY: Connecting to %s via %s:%d (attempt %d/%d)", address, AppConfig.Proxy.Host, AppConfig.Proxy.Port, i, attempts)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		var conn net.Conn
		conn, err = d.DialContext(ctx, network, address)
		cancel()
		reason := dialFailReason(err)
		metricDialAttempt(reason)
		if err == nil {
			return conn, nil
		}
		log.Printf("PROXY: Attempt %d/%d to %s failed (%s): %v", i, attempts, address, reason, err)
		if reason == "proxy_down" {
			break // the proxy itself is unreachable; another circuit won't help
		}
	}
	return nil, fmt.Errorf("proxy dial failed: %v", err)
}

// dialFailReason sorts a dial error into a short metric label; "ok" for nil.
// SOCKS reply strings come from golang.org/x/net/internal/socks.
func dialFailReason(err error) string {
	if err == nil {
		return "ok"
	}
	var op *net.OpError
	if errors.As(err, &op) {
		var inner *net.OpError
		if errors.As(op.Err, &inner) && inner.Op == "dial" {
			return "proxy_down" // failed reaching the proxy, not the board
		}
	}
	s := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(s, "timeout"), strings.Contains(s, "TTL expired"):
		return "timeout"
	case strings.Contains(s, "refused"):
		return "refused"
	case strings.Contains(s, "unreachable"):
		return "unreachable"
	case strings.Contains(s, "not allowed by ruleset"):
		return "exit_policy"
	case strings.Contains(s, "no such host"), strings.Contains(s, "general SOCKS server failure"):
		return "general" // Tor's catch-all, which includes failed DNS at the exit
	case strings.Contains(s, "reset"), strings.Contains(s, "EOF"):
		return "reset"
	}
	return "other"
}
