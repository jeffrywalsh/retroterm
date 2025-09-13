package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"golang.org/x/net/proxy"
)

// CreateProxyDialer constructs a net.Dialer or SOCKS5 proxy dialer depending
// on configuration. When type is "tor", timeouts are extended to accommodate
// typical Tor circuit setup delays.
func CreateProxyDialer() (proxy.Dialer, error) {
	if AppConfig == nil || !AppConfig.Proxy.Enabled {
		// No proxy, use direct connection
		return &net.Dialer{
			Timeout: 10 * time.Second,
		}, nil
	}

	// Create SOCKS5 proxy dialer
	proxyAddr := fmt.Sprintf("%s:%d", AppConfig.Proxy.Host, AppConfig.Proxy.Port)

	var auth *proxy.Auth
	if AppConfig.Proxy.Username != "" {
		auth = &proxy.Auth{
			User:     AppConfig.Proxy.Username,
			Password: AppConfig.Proxy.Password,
		}
	}

	// Increase timeout for Tor connections (they're slower)
	timeout := 10 * time.Second
	if AppConfig.Proxy.Type == "tor" {
		timeout = 30 * time.Second
		log.Printf("PROXY: Using Tor SOCKS5 proxy at %s (extended timeout)", proxyAddr)
	}

	dialer, err := proxy.SOCKS5("tcp", proxyAddr, auth, &net.Dialer{
		Timeout: timeout,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create SOCKS5 dialer: %v", err)
	}

	if AppConfig.Proxy.Type != "tor" {
		log.Printf("PROXY: Using SOCKS5 proxy at %s", proxyAddr)
	}
	return dialer, nil
}

// DialWithProxy establishes a network connection, routing through a SOCKS5
// proxy if enabled in the config. Errors are wrapped with context.
func DialWithProxy(network, address string) (net.Conn, error) {
	dialer, err := CreateProxyDialer()
	if err != nil {
		return nil, err
	}

	if AppConfig != nil && AppConfig.Proxy.Enabled {
		log.Printf("PROXY: Connecting to %s via proxy %s:%d", address, AppConfig.Proxy.Host, AppConfig.Proxy.Port)
	}

	conn, err := dialer.Dial(network, address)
	if err != nil {
		return nil, fmt.Errorf("proxy dial failed: %v", err)
	}

	return conn, nil
}
