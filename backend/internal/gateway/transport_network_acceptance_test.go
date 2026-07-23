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
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
)

func TestTransportNetworkAcceptanceFourQuadrants(t *testing.T) {
	type quadrant struct {
		name        string
		fingerprint bool
		proxy       bool
	}
	quadrants := []quadrant{
		{name: "direct without fingerprint"},
		{name: "direct with fingerprint", fingerprint: true},
		{name: "proxy without fingerprint", proxy: true},
		{name: "proxy with fingerprint", fingerprint: true, proxy: true},
	}

	shapes := make(map[string]acceptanceClientHelloShape, len(quadrants))
	for _, testCase := range quadrants {
		t.Run(testCase.name, func(t *testing.T) {
			capture := &acceptanceTLSCapture{}
			upstream := newAcceptanceTLSServer(t, capture, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/quadrant" {
					http.NotFound(w, r)
					return
				}
				_, _ = io.WriteString(w, "ok")
			}))

			var proxyURL *url.URL
			var connectCount atomic.Int32
			if testCase.proxy {
				proxyServer := newConnectProxyServer(t, upstream.Listener.Addr().String(), false, "matrix-user", "matrix-password", &connectCount)
				var err error
				proxyURL, err = url.Parse(proxyServer.URL)
				if err != nil {
					t.Fatalf("parse proxy URL: %v", err)
				}
				proxyURL.User = url.UserPassword("matrix-user", "matrix-password")
			}

			client, closeIdle := newAcceptanceTransportClient(proxyURL, testCase.fingerprint, acceptanceTimeouts())
			t.Cleanup(closeIdle)
			resp, err := client.Get(upstream.URL + "/quadrant")
			if err != nil {
				t.Fatalf("GET quadrant: %v", err)
			}
			body, readErr := io.ReadAll(resp.Body)
			closeErr := resp.Body.Close()
			if readErr != nil || closeErr != nil || string(body) != "ok" {
				t.Fatalf("response body/read/close = %q/%v/%v", body, readErr, closeErr)
			}
			if testCase.proxy && connectCount.Load() != 1 {
				t.Fatalf("CONNECT count = %d, want 1", connectCount.Load())
			}

			handshake, err := extractAcceptanceClientHello(capture.Bytes())
			if err != nil {
				t.Fatalf("extract captured ClientHello: %v", err)
			}
			shape, err := parseAcceptanceClientHelloShape(handshake)
			if err != nil {
				t.Fatalf("parse captured ClientHello: %v", err)
			}
			shapes[testCase.name] = shape

			if testCase.fingerprint {
				host, _, splitErr := net.SplitHostPort(upstream.Listener.Addr().String())
				if splitErr != nil {
					t.Fatalf("split upstream address: %v", splitErr)
				}
				expected := buildAcceptanceClientHelloShape(t, utls.HelloChrome_Auto, host)
				if !reflect.DeepEqual(shape, expected) {
					t.Fatalf("captured fingerprint ClientHello shape = %+v, want Chrome shape %+v", shape, expected)
				}
			}
		})
	}

	directPlain := shapes["direct without fingerprint"]
	proxyPlain := shapes["proxy without fingerprint"]
	directFingerprint := shapes["direct with fingerprint"]
	proxyFingerprint := shapes["proxy with fingerprint"]
	if !reflect.DeepEqual(directPlain, proxyPlain) {
		t.Fatalf("proxy changed standard ClientHello shape: direct=%+v proxy=%+v", directPlain, proxyPlain)
	}
	if !reflect.DeepEqual(directFingerprint, proxyFingerprint) {
		t.Fatalf("proxy changed fingerprint ClientHello shape: direct=%+v proxy=%+v", directFingerprint, proxyFingerprint)
	}
	if reflect.DeepEqual(directPlain, directFingerprint) {
		t.Fatal("captured Chrome ClientHello shape matches the standard Go ClientHello")
	}
}

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

	client, closeIdle := newAcceptanceTransportClient(proxyURL, false, acceptanceTimeouts())
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
		client, closeIdle := newAcceptanceTransportClient(nil, true, timeouts)
		t.Cleanup(closeIdle)

		startedAt := time.Now()
		_, err := client.Get("https://" + address + "/stall")
		if err == nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("TLS stall error = %v, want deadline exceeded", err)
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
		client, closeIdle := newAcceptanceTransportClient(nil, true, upstreamTimeouts{connect: time.Second, tlsHandshake: 5 * time.Second, responseHeader: time.Second})
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
	client, closeIdle := newAcceptanceTransportClient(proxyURL, true, acceptanceTimeouts())
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

func newAcceptanceTransportClient(proxyURL *url.URL, fingerprint bool, timeouts upstreamTimeouts) (*http.Client, func()) {
	if fingerprint {
		client, transport := newTestFingerprintClientWithTimeouts(proxyURL, timeouts)
		return client, transport.CloseIdleConnections
	}
	transport := newSelectedAccountTransport(proxyURL, "", timeouts)
	transport.standard.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &http.Client{Transport: transport}, transport.CloseIdleConnections
}

type acceptanceTLSCapture struct {
	mu  sync.Mutex
	raw []byte
}

func (c *acceptanceTLSCapture) Append(data []byte) {
	c.mu.Lock()
	c.raw = append(c.raw, data...)
	c.mu.Unlock()
}

func (c *acceptanceTLSCapture) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.raw...)
}

type acceptanceCaptureListener struct {
	net.Listener
	capture *acceptanceTLSCapture
}

func (l *acceptanceCaptureListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &acceptanceCaptureConn{Conn: conn, capture: l.capture}, nil
}

type acceptanceCaptureConn struct {
	net.Conn
	capture *acceptanceTLSCapture
}

func (c *acceptanceCaptureConn) Read(data []byte) (int, error) {
	n, err := c.Conn.Read(data)
	if n > 0 {
		c.capture.Append(data[:n])
	}
	return n, err
}

func newAcceptanceTLSServer(t *testing.T, capture *acceptanceTLSCapture, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.Listener = &acceptanceCaptureListener{Listener: server.Listener, capture: capture}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

type acceptanceClientHelloShape struct {
	cipherSuites []uint16
	extensions   []uint16
}

func extractAcceptanceClientHello(records []byte) ([]byte, error) {
	var handshake []byte
	for len(records) >= 5 {
		recordLength := int(records[3])<<8 | int(records[4])
		if recordLength < 0 || len(records) < 5+recordLength {
			return nil, errors.New("truncated TLS record")
		}
		if records[0] == 22 {
			handshake = append(handshake, records[5:5+recordLength]...)
			if len(handshake) >= 4 {
				handshakeLength := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
				if handshake[0] != 1 {
					return nil, errors.New("first TLS handshake is not ClientHello")
				}
				if len(handshake) >= 4+handshakeLength {
					return append([]byte(nil), handshake[:4+handshakeLength]...), nil
				}
			}
		}
		records = records[5+recordLength:]
	}
	return nil, errors.New("ClientHello not found")
}

func parseAcceptanceClientHelloShape(handshake []byte) (acceptanceClientHelloShape, error) {
	if len(handshake) < 4 || handshake[0] != 1 {
		return acceptanceClientHelloShape{}, errors.New("invalid ClientHello handshake")
	}
	bodyLength := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
	if len(handshake) < 4+bodyLength {
		return acceptanceClientHelloShape{}, errors.New("truncated ClientHello handshake")
	}
	body := handshake[4 : 4+bodyLength]
	position := 2 + 32
	if len(body) < position+1 {
		return acceptanceClientHelloShape{}, errors.New("truncated ClientHello session")
	}
	sessionLength := int(body[position])
	position++
	if len(body) < position+sessionLength+2 {
		return acceptanceClientHelloShape{}, errors.New("truncated ClientHello session data")
	}
	position += sessionLength
	cipherLength := int(body[position])<<8 | int(body[position+1])
	position += 2
	if cipherLength%2 != 0 || len(body) < position+cipherLength+1 {
		return acceptanceClientHelloShape{}, errors.New("invalid ClientHello cipher suites")
	}
	shape := acceptanceClientHelloShape{}
	for end := position + cipherLength; position < end; position += 2 {
		value := uint16(body[position])<<8 | uint16(body[position+1])
		if !isAcceptanceGREASE(value) {
			shape.cipherSuites = append(shape.cipherSuites, value)
		}
	}
	compressionLength := int(body[position])
	position++
	if len(body) < position+compressionLength {
		return acceptanceClientHelloShape{}, errors.New("truncated ClientHello compression methods")
	}
	position += compressionLength
	if position == len(body) {
		return shape, nil
	}
	if len(body) < position+2 {
		return acceptanceClientHelloShape{}, errors.New("truncated ClientHello extensions length")
	}
	extensionLength := int(body[position])<<8 | int(body[position+1])
	position += 2
	if len(body) < position+extensionLength {
		return acceptanceClientHelloShape{}, errors.New("truncated ClientHello extensions")
	}
	for end := position + extensionLength; position < end; {
		if end-position < 4 {
			return acceptanceClientHelloShape{}, errors.New("truncated ClientHello extension")
		}
		extensionType := uint16(body[position])<<8 | uint16(body[position+1])
		valueLength := int(body[position+2])<<8 | int(body[position+3])
		position += 4
		if end-position < valueLength {
			return acceptanceClientHelloShape{}, errors.New("truncated ClientHello extension value")
		}
		if !isAcceptanceGREASE(extensionType) {
			shape.extensions = append(shape.extensions, extensionType)
		}
		position += valueLength
	}
	slices.Sort(shape.extensions)
	return shape, nil
}

func isAcceptanceGREASE(value uint16) bool {
	return value&0x0f0f == 0x0a0a
}

func buildAcceptanceClientHelloShape(t *testing.T, helloID utls.ClientHelloID, serverName string) acceptanceClientHelloShape {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})
	uconn := utls.UClient(clientConn, &utls.Config{ServerName: serverName, NextProtos: []string{"http/1.1"}}, helloID)
	if err := uconn.BuildHandshakeState(); err != nil {
		t.Fatalf("BuildHandshakeState: %v", err)
	}
	if err := uconn.MarshalClientHello(); err != nil {
		t.Fatalf("MarshalClientHello: %v", err)
	}
	shape, err := parseAcceptanceClientHelloShape(uconn.HandshakeState.Hello.Raw)
	if err != nil {
		t.Fatalf("parse built ClientHello: %v", err)
	}
	return shape
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
