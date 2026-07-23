package gateway

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
)

func TestUpstreamTransportsApplyConfiguredTimeouts(t *testing.T) {
	timeouts := upstreamTimeouts{
		connect:        7 * time.Second,
		tlsHandshake:   8 * time.Second,
		responseHeader: 9 * time.Second,
	}
	base := newUpstreamTransport(timeouts)
	if base.DialContext == nil || base.TLSHandshakeTimeout != timeouts.tlsHandshake || base.ResponseHeaderTimeout != timeouts.responseHeader {
		t.Fatalf("base transport timeouts = dial:%v tls:%s header:%s", base.DialContext != nil, base.TLSHandshakeTimeout, base.ResponseHeaderTimeout)
	}

	selected := newSelectedAccountTransport(nil, "chrome", timeouts)
	if selected.standard.ResponseHeaderTimeout != timeouts.responseHeader || selected.fingerprint == nil || selected.fingerprint.DialTLSContext == nil || selected.fingerprint.ResponseHeaderTimeout != timeouts.responseHeader {
		t.Fatalf("selected transport timeouts = standard header:%s fingerprint dial:%v header:%s", selected.standard.ResponseHeaderTimeout, selected.fingerprint != nil && selected.fingerprint.DialTLSContext != nil, selected.fingerprint.ResponseHeaderTimeout)
	}
}

func TestClientHelloIDForFingerprint(t *testing.T) {
	tests := []struct {
		input string
		want  utls.ClientHelloID
	}{
		{input: "chrome", want: utls.HelloChrome_Auto},
		{input: "Firefox Auto", want: utls.HelloFirefox_Auto},
		{input: "safari", want: utls.HelloSafari_Auto},
		{input: "ios", want: utls.HelloIOS_Auto},
		{input: "android", want: utls.HelloAndroid_11_OkHttp},
		{input: "edge", want: utls.HelloEdge_Auto},
		{input: "randomized", want: utls.HelloRandomized},
		{input: "randomized_alpn", want: utls.HelloRandomizedALPN},
		{input: "randomized-no-alpn", want: utls.HelloRandomizedNoALPN},
		{input: "golang", want: utls.HelloGolang},
		{input: "t13d1516h2_8daaf6152771_e4107deab09e", want: utls.HelloGolang},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := clientHelloIDForFingerprint(tt.input); got != tt.want {
				t.Fatalf("clientHelloIDForFingerprint(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestChromeFingerprintBuildsDifferentClientHelloFromGo(t *testing.T) {
	chromeHello := buildClientHelloBytes(t, utls.HelloChrome_Auto)
	goHello := buildClientHelloBytes(t, utls.HelloGolang)
	if len(chromeHello) == 0 || len(goHello) == 0 {
		t.Fatal("built ClientHello is empty")
	}
	if reflect.DeepEqual(chromeHello, goHello) {
		t.Fatal("Chrome fingerprint built the Go ClientHello bytes")
	}
}

func TestFingerprintTransportThroughConnectProxy(t *testing.T) {
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: response.created\ndata: {\"response\":{\"id\":\"resp_test\"}}\n\n")
			w.(http.Flusher).Flush()
			_, _ = io.WriteString(w, "event: response.completed\ndata: {\"response\":{\"id\":\"resp_test\"}}\n\n")
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	upstream.StartTLS()
	t.Cleanup(upstream.Close)

	for _, secureProxy := range []bool{false, true} {
		name := "http proxy"
		if secureProxy {
			name = "https proxy"
		}
		t.Run(name, func(t *testing.T) {
			var connectCount atomic.Int32
			proxyServer := newConnectProxyServer(t, upstream.Listener.Addr().String(), secureProxy, "proxy-user", "proxy-password", &connectCount)
			proxyURL, err := url.Parse(proxyServer.URL)
			if err != nil {
				t.Fatalf("parse proxy URL: %v", err)
			}
			proxyURL.User = url.UserPassword("proxy-user", "proxy-password")
			client, transport := newTestFingerprintClient(proxyURL)
			t.Cleanup(transport.CloseIdleConnections)

			for _, path := range []string{"/first", "/stream"} {
				resp, err := client.Get(upstream.URL + path)
				if err != nil {
					t.Fatalf("GET %s through proxy: %v", path, err)
				}
				body, readErr := io.ReadAll(resp.Body)
				closeErr := resp.Body.Close()
				if readErr != nil || closeErr != nil {
					t.Fatalf("read/close %s response: %v/%v", path, readErr, closeErr)
				}
				if path == "/stream" && (!strings.Contains(string(body), "response.created") || !strings.Contains(string(body), "response.completed")) {
					t.Fatalf("SSE response = %q", body)
				}
			}
			if got := connectCount.Load(); got != 1 {
				t.Fatalf("CONNECT count = %d, want 1 reused tunnel", got)
			}
		})
	}
}

func TestFingerprintProxyFailuresRespectCancellationAndRedactCredentials(t *testing.T) {
	t.Run("CONNECT rejection", func(t *testing.T) {
		proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "denied", http.StatusProxyAuthRequired)
		}))
		t.Cleanup(proxyServer.Close)
		proxyURL, _ := url.Parse(proxyServer.URL)
		proxyURL.User = url.UserPassword("secret-user", "secret-password")
		client, transport := newTestFingerprintClient(proxyURL)
		t.Cleanup(transport.CloseIdleConnections)

		_, err := client.Get("https://upstream.example.test/v1/responses")
		if err == nil {
			t.Fatal("CONNECT rejection returned no error")
		}
		if strings.Contains(err.Error(), "secret-user") || strings.Contains(err.Error(), "secret-password") {
			t.Fatalf("CONNECT error leaked proxy credentials: %v", err)
		}
	})

	t.Run("upstream TLS failure", func(t *testing.T) {
		plainUpstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		t.Cleanup(plainUpstream.Close)
		client, transport := newTestFingerprintClient(nil)
		t.Cleanup(transport.CloseIdleConnections)

		_, err := client.Get("https" + strings.TrimPrefix(plainUpstream.URL, "http"))
		if err == nil {
			t.Fatal("plain upstream accepted a fingerprint TLS request")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		requestStarted := make(chan struct{})
		proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(requestStarted)
			<-r.Context().Done()
		}))
		t.Cleanup(proxyServer.Close)
		proxyURL, _ := url.Parse(proxyServer.URL)
		client, transport := newTestFingerprintClient(proxyURL)
		t.Cleanup(transport.CloseIdleConnections)

		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://upstream.example.test/v1/responses", nil)
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		done := make(chan error, 1)
		go func() {
			_, requestErr := client.Do(req)
			done <- requestErr
		}()
		select {
		case <-requestStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("proxy did not receive CONNECT request")
		}
		cancel()
		select {
		case requestErr := <-done:
			if requestErr == nil || !strings.Contains(requestErr.Error(), context.Canceled.Error()) {
				t.Fatalf("request error = %v, want context canceled", requestErr)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("canceled CONNECT request did not return")
		}
	})

	t.Run("CONNECT timeout", func(t *testing.T) {
		proxyServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		t.Cleanup(proxyServer.Close)
		proxyURL, _ := url.Parse(proxyServer.URL)
		timeouts := upstreamTimeouts{connect: 50 * time.Millisecond, tlsHandshake: time.Second, responseHeader: time.Second}
		client, transport := newTestFingerprintClientWithTimeouts(proxyURL, timeouts)
		t.Cleanup(transport.CloseIdleConnections)

		startedAt := time.Now()
		_, err := client.Get("https://upstream.example.test/v1/responses")
		if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
			t.Fatalf("request error = %v, want deadline exceeded", err)
		}
		if elapsed := time.Since(startedAt); elapsed > time.Second {
			t.Fatalf("CONNECT timeout returned after %s, want under 1s", elapsed)
		}
	})
}

func buildClientHelloBytes(t *testing.T, helloID utls.ClientHelloID) []byte {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})
	uconn := utls.UClient(clientConn, &utls.Config{ServerName: "example.test"}, helloID)
	if err := uconn.BuildHandshakeState(); err != nil {
		t.Fatalf("BuildHandshakeState: %v", err)
	}
	if err := uconn.MarshalClientHello(); err != nil {
		t.Fatalf("MarshalClientHello: %v", err)
	}
	return append([]byte(nil), uconn.HandshakeState.Hello.Raw...)
}

func newTestFingerprintClient(proxyURL *url.URL) (*http.Client, *http.Transport) {
	timeouts := upstreamTimeouts{connect: 2 * time.Second, tlsHandshake: 2 * time.Second, responseHeader: 2 * time.Second}
	return newTestFingerprintClientWithTimeouts(proxyURL, timeouts)
}

func newTestFingerprintClientWithTimeouts(proxyURL *url.URL, timeouts upstreamTimeouts) (*http.Client, *http.Transport) {
	dialer := newFingerprintTLSDialer(proxyURL, "chrome", timeouts)
	dialer.utlsConfig = &utls.Config{InsecureSkipVerify: true}
	if proxyURL != nil && strings.EqualFold(proxyURL.Scheme, "https") {
		dialer.proxyTLSConfig = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}
	}
	transport := newUpstreamTransport(timeouts)
	transport.ForceAttemptHTTP2 = false
	transport.Proxy = nil
	transport.DialTLSContext = dialer.DialTLSContext
	return &http.Client{Transport: transport}, transport
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
