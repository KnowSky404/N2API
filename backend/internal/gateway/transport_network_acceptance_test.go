package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTransportNetworkAcceptanceStandardProxyAuthAndRedaction(t *testing.T) {
	const (
		username = "standard-secret-user"
		password = "standard-secret-password"
	)
	authorizationSeen := make(chan string, 1)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizationSeen <- r.Header.Get("Proxy-Authorization")
		http.Error(w, "proxy authentication rejected", http.StatusProxyAuthRequired)
	}))
	t.Cleanup(proxyServer.Close)
	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	proxyURL.User = url.UserPassword(username, password)

	client, closeIdle := newAcceptanceTransportClient(proxyURL, acceptanceTimeouts())
	t.Cleanup(closeIdle)
	_, requestErr := client.Get("https://upstream.example.test/v1/responses")
	if requestErr == nil {
		t.Fatal("rejected standard proxy CONNECT returned no error")
	}
	expectedAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
	select {
	case got := <-authorizationSeen:
		if got != expectedAuthorization {
			t.Fatalf("Proxy-Authorization = %q, want Basic credentials", got)
		}
	case <-time.After(time.Second):
		t.Fatal("standard proxy did not receive CONNECT")
	}
	for _, secret := range []string{username, password, expectedAuthorization} {
		if strings.Contains(requestErr.Error(), secret) {
			t.Fatalf("standard proxy error leaked credential %q: %v", secret, requestErr)
		}
	}
}

func TestTransportNetworkAcceptanceTLSHandshakeStall(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		address, accepted := newAcceptanceStalledTLSListener(t)
		timeouts := upstreamTimeouts{connect: time.Second, tlsHandshake: 50 * time.Millisecond, responseHeader: time.Second}
		client, closeIdle := newAcceptanceTransportClient(nil, timeouts)
		t.Cleanup(closeIdle)

		startedAt := time.Now()
		_, err := client.Get("https://" + address + "/stall")
		if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
			t.Fatalf("TLS stall error = %v, want timeout", err)
		}
		if elapsed := time.Since(startedAt); elapsed > time.Second {
			t.Fatalf("TLS stall timeout returned after %s", elapsed)
		}
		select {
		case <-accepted:
		case <-time.After(time.Second):
			t.Fatal("TLS stall listener did not accept connection")
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		address, accepted := newAcceptanceStalledTLSListener(t)
		client, closeIdle := newAcceptanceTransportClient(nil, upstreamTimeouts{connect: time.Second, tlsHandshake: 5 * time.Second, responseHeader: time.Second})
		t.Cleanup(closeIdle)
		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+address+"/stall", nil)
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		done := make(chan error, 1)
		go func() {
			_, requestErr := client.Do(req)
			done <- requestErr
		}()
		select {
		case <-accepted:
		case <-time.After(time.Second):
			t.Fatal("TLS stall listener did not accept connection")
		}
		cancel()
		select {
		case requestErr := <-done:
			if requestErr == nil || !errors.Is(requestErr, context.Canceled) {
				t.Fatalf("canceled TLS handshake error = %v, want context canceled", requestErr)
			}
		case <-time.After(time.Second):
			t.Fatal("canceled TLS handshake did not return")
		}
	})
}

func TestTransportNetworkAcceptanceTunnelSSECancellation(t *testing.T) {
	firstEventSent := make(chan struct{})
	upstreamCanceled := make(chan struct{})
	releaseUpstream := make(chan struct{})
	var releaseOnce sync.Once
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"response\":{\"id\":\"resp_cancel\"}}\n\n")
		w.(http.Flusher).Flush()
		close(firstEventSent)
		select {
		case <-r.Context().Done():
			close(upstreamCanceled)
		case <-releaseUpstream:
		}
	}))
	upstream.StartTLS()
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseUpstream) })
		upstream.Close()
	})

	var connectCount atomic.Int32
	proxyServer := newConnectProxyServer(t, upstream.Listener.Addr().String(), false, "sse-user", "sse-password", &connectCount)
	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	proxyURL.User = url.UserPassword("sse-user", "sse-password")
	client, closeIdle := newAcceptanceTransportClient(proxyURL, acceptanceTimeouts())
	t.Cleanup(closeIdle)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL+"/stream", nil)
	if err != nil {
		t.Fatalf("create SSE request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("start SSE request: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	reader := bufio.NewReader(resp.Body)
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatalf("read first SSE event: %v", readErr)
		}
		if line == "\n" {
			break
		}
	}
	select {
	case <-firstEventSent:
	case <-time.After(time.Second):
		t.Fatal("upstream did not flush first SSE event")
	}
	cancel()

	readDone := make(chan error, 1)
	go func() {
		_, readErr := reader.ReadByte()
		readDone <- readErr
	}()
	select {
	case readErr := <-readDone:
		if readErr == nil {
			t.Fatal("SSE body remained readable after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled tunnel SSE read did not return")
	}
	select {
	case <-upstreamCanceled:
	case <-time.After(time.Second):
		t.Fatal("upstream SSE request context was not canceled")
	}
	if connectCount.Load() != 1 {
		t.Fatalf("CONNECT count = %d, want 1", connectCount.Load())
	}
}

func acceptanceTimeouts() upstreamTimeouts {
	return upstreamTimeouts{connect: 2 * time.Second, tlsHandshake: 2 * time.Second, responseHeader: 2 * time.Second}
}

func newAcceptanceTransportClient(proxyURL *url.URL, timeouts upstreamTimeouts) (*http.Client, func()) {
	transport := newSelectedAccountTransport(proxyURL, timeouts)
	transport.standard.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &http.Client{Transport: transport}, transport.CloseIdleConnections
}

func newAcceptanceStalledTLSListener(t *testing.T) (string, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for stalled TLS: %v", err)
	}
	accepted := make(chan struct{})
	release := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		close(accepted)
		<-release
		_ = conn.Close()
	}()
	var cleanupOnce sync.Once
	t.Cleanup(func() {
		cleanupOnce.Do(func() {
			close(release)
			_ = listener.Close()
		})
	})
	return listener.Addr().String(), accepted
}

func newConnectProxyServer(t *testing.T, allowedTarget string, secure bool, username, password string, connectCount *atomic.Int32) *httptest.Server {
	t.Helper()
	expectedAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != allowedTarget || r.Header.Get("Proxy-Authorization") != expectedAuthorization {
			http.Error(w, "proxy request rejected", http.StatusProxyAuthRequired)
			return
		}
		upstreamConn, err := net.DialTimeout("tcp", allowedTarget, 2*time.Second)
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			_ = upstreamConn.Close()
			http.Error(w, "hijacking unavailable", http.StatusInternalServerError)
			return
		}
		clientConn, buffered, err := hijacker.Hijack()
		if err != nil {
			_ = upstreamConn.Close()
			return
		}
		connectCount.Add(1)
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffered.Flush()
		defer clientConn.Close()
		defer upstreamConn.Close()
		copyDone := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstreamConn, clientConn)
			_ = upstreamConn.(*net.TCPConn).CloseWrite()
			close(copyDone)
		}()
		_, _ = io.Copy(clientConn, upstreamConn)
		<-copyDone
	})
	server := httptest.NewUnstartedServer(handler)
	if secure {
		server.StartTLS()
	} else {
		server.Start()
	}
	t.Cleanup(server.Close)
	return server
}
