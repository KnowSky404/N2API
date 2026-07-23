package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/config"
)

func TestHTTPServerClosesIdleKeepAliveConnection(t *testing.T) {
	const idleTimeout = 50 * time.Millisecond
	var handled atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handled.Add(1)
		w.Header().Set("Content-Length", "2")
		_, _ = io.WriteString(w, "ok")
	})
	server := newHTTPServer(config.Config{HTTPIdleTimeout: idleTimeout}, handler, context.Background())
	address := startHTTPServerLimitTest(t, server)

	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: "+address+"\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}

	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("ReadResponse returned error: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("response = status:%d body:%q readErr:%v closeErr:%v", response.StatusCode, body, readErr, closeErr)
	}
	if response.Close {
		t.Fatal("server disabled keep-alive before applying IdleTimeout")
	}

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline returned error: %v", err)
	}
	_, err = reader.ReadByte()
	if err == nil {
		t.Fatal("idle keep-alive connection remained readable")
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		t.Fatalf("idle keep-alive connection was not closed within the deadline: %v", err)
	}
	if handled.Load() != 1 {
		t.Fatalf("handled requests = %d, want 1", handled.Load())
	}
}

func TestHTTPServerRejectsRequestHeadersAboveConfiguredLimit(t *testing.T) {
	const maxHeaderBytes = 8 << 10
	var handled atomic.Int32
	server := newHTTPServer(config.Config{
		HTTPIdleTimeout:    time.Second,
		HTTPMaxHeaderBytes: maxHeaderBytes,
	}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		handled.Add(1)
	}), context.Background())
	address := startHTTPServerLimitTest(t, server)

	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	request := "GET / HTTP/1.1\r\nHost: " + address + "\r\nX-Oversized: " +
		strings.Repeat("a", maxHeaderBytes+(8<<10)) + "\r\n\r\n"
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatalf("write oversized request: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline returned error: %v", err)
	}

	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("ReadResponse returned error: %v", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusRequestHeaderFieldsTooLarge)
	}
	if !response.Close {
		t.Fatal("oversized-header response did not close the connection")
	}
	if handled.Load() != 0 {
		t.Fatalf("handler calls = %d, want 0", handled.Load())
	}
}

func startHTTPServerLimitTest(t *testing.T, server *http.Server) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("Close returned error: %v", err)
		}
		select {
		case err := <-serveErrors:
			if !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("Serve returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Serve did not stop after Close")
		}
	})
	return listener.Addr().String()
}
