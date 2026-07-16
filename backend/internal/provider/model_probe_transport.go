package provider

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
)

type modelProbeTLSFingerprintContextKey struct{}

func contextWithModelProbeTLSFingerprint(ctx context.Context, fingerprint string) context.Context {
	fingerprint = normalizeModelProbeTLSFingerprint(fingerprint)
	if fingerprint == "" {
		return ctx
	}
	return context.WithValue(ctx, modelProbeTLSFingerprintContextKey{}, fingerprint)
}

type modelProbeTLSFingerprintTransport struct {
	base           http.RoundTripper
	dialer         *net.Dialer
	proxyURL       *url.URL
	tlsConfig      *utls.Config
	proxyTLSConfig *tls.Config
}

func newModelProbeTLSFingerprintTransport(base http.RoundTripper, proxyURL string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	transport := &modelProbeTLSFingerprintTransport{
		base: base,
		dialer: &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		},
	}
	if parsed, err := url.Parse(strings.TrimSpace(proxyURL)); err == nil && parsed.IsAbs() && parsed.Host != "" {
		transport.proxyURL = parsed
	}
	return transport
}

func (t *modelProbeTLSFingerprintTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	fingerprint, _ := req.Context().Value(modelProbeTLSFingerprintContextKey{}).(string)
	fingerprint = normalizeModelProbeTLSFingerprint(fingerprint)
	if fingerprint == "" || !strings.EqualFold(req.URL.Scheme, "https") {
		return t.base.RoundTrip(req)
	}
	host := req.URL.Hostname()
	if host == "" {
		return nil, fmt.Errorf("missing upstream hostname")
	}
	addr := req.URL.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "443")
	}
	conn, err := t.dialTarget(req.Context(), addr)
	if err != nil {
		return nil, err
	}
	lifecycle := newModelProbeConnLifecycle(req.Context(), conn)
	if t.proxyURL != nil {
		conn, err = t.connectProxyTunnel(req.Context(), conn, addr)
		if err != nil {
			_ = lifecycle.Close()
			return nil, err
		}
	}
	tlsConfig := &utls.Config{}
	if t.tlsConfig != nil {
		tlsConfig = t.tlsConfig.Clone()
	}
	tlsConfig.ServerName = host
	tlsConfig.NextProtos = []string{"http/1.1"}
	uconn := utls.UClient(conn, tlsConfig, modelProbeClientHelloID(fingerprint))
	if err := uconn.HandshakeContext(req.Context()); err != nil {
		_ = lifecycle.Close()
		return nil, err
	}
	if negotiated := uconn.ConnectionState().NegotiatedProtocol; negotiated != "" && negotiated != "http/1.1" {
		_ = lifecycle.Close()
		return nil, fmt.Errorf("unsupported negotiated protocol %q for fingerprint transport", negotiated)
	}
	if err := req.Write(uconn); err != nil {
		_ = lifecycle.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(uconn), req)
	if err != nil {
		_ = lifecycle.Close()
		return nil, err
	}
	resp.Body = &modelProbeCloseWithConn{
		ReadCloser:  resp.Body,
		lifecycle:   lifecycle,
		watcherDone: lifecycle.watcherDone,
	}
	return resp, nil
}

func (t *modelProbeTLSFingerprintTransport) dialTarget(ctx context.Context, targetAddr string) (net.Conn, error) {
	addr := targetAddr
	if t.proxyURL != nil {
		addr = t.proxyURL.Host
		if _, _, err := net.SplitHostPort(addr); err != nil {
			port := "80"
			if strings.EqualFold(t.proxyURL.Scheme, "https") {
				port = "443"
			}
			addr = net.JoinHostPort(t.proxyURL.Hostname(), port)
		}
	}
	return t.dialer.DialContext(ctx, "tcp", addr)
}

func (t *modelProbeTLSFingerprintTransport) connectProxyTunnel(ctx context.Context, conn net.Conn, targetAddr string) (net.Conn, error) {
	if strings.EqualFold(t.proxyURL.Scheme, "https") {
		proxyTLSConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		if t.proxyTLSConfig != nil {
			proxyTLSConfig = t.proxyTLSConfig.Clone()
		}
		proxyTLSConfig.ServerName = t.proxyURL.Hostname()
		proxyTLSConfig.NextProtos = []string{"http/1.1"}
		tlsConn := tls.Client(conn, proxyTLSConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("TLS handshake with proxy: %w", err)
		}
		conn = tlsConn
	} else if !strings.EqualFold(t.proxyURL.Scheme, "http") {
		return nil, fmt.Errorf("unsupported proxy scheme %q", t.proxyURL.Scheme)
	}

	connectReq := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: targetAddr},
		Host:   targetAddr,
		Header: make(http.Header),
	}
	if t.proxyURL.User != nil {
		username := t.proxyURL.User.Username()
		password, _ := t.proxyURL.User.Password()
		credentials := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		connectReq.Header.Set("Proxy-Authorization", "Basic "+credentials)
	}
	if err := connectReq.Write(conn); err != nil {
		return nil, fmt.Errorf("write proxy CONNECT request: %w", err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, connectReq)
	if err != nil {
		return nil, fmt.Errorf("read proxy CONNECT response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy CONNECT returned %s", resp.Status)
	}
	if reader.Buffered() == 0 {
		return conn, nil
	}
	return &modelProbeBufferedConn{Conn: conn, reader: reader}, nil
}

type modelProbeBufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *modelProbeBufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

type modelProbeConnLifecycle struct {
	conn        net.Conn
	stop        chan struct{}
	watcherDone chan struct{}
	once        sync.Once
	closeErr    error
}

func newModelProbeConnLifecycle(ctx context.Context, conn net.Conn) *modelProbeConnLifecycle {
	lifecycle := &modelProbeConnLifecycle{
		conn:        conn,
		stop:        make(chan struct{}),
		watcherDone: make(chan struct{}),
	}
	go func() {
		defer close(lifecycle.watcherDone)
		select {
		case <-ctx.Done():
			_ = lifecycle.Close()
		case <-lifecycle.stop:
		}
	}()
	return lifecycle
}

func (l *modelProbeConnLifecycle) Close() error {
	l.once.Do(func() {
		close(l.stop)
		l.closeErr = l.conn.Close()
	})
	return l.closeErr
}

type modelProbeCloseWithConn struct {
	io.ReadCloser
	lifecycle   *modelProbeConnLifecycle
	watcherDone <-chan struct{}
}

func (b *modelProbeCloseWithConn) Close() error {
	connErr := b.lifecycle.Close()
	bodyErr := b.ReadCloser.Close()
	if bodyErr != nil && !errors.Is(bodyErr, net.ErrClosed) {
		return bodyErr
	}
	return connErr
}

func normalizeModelProbeTLSFingerprint(fingerprint string) string {
	return strings.ToLower(strings.TrimSpace(fingerprint))
}

func modelProbeClientHelloID(fingerprint string) utls.ClientHelloID {
	switch normalizeModelProbeTLSFingerprint(fingerprint) {
	case "chrome":
		return utls.HelloChrome_Auto
	case "firefox":
		return utls.HelloFirefox_Auto
	case "safari":
		return utls.HelloSafari_Auto
	case "ios":
		return utls.HelloIOS_Auto
	case "android":
		return utls.HelloAndroid_11_OkHttp
	case "edge":
		return utls.HelloEdge_Auto
	case "randomized":
		return utls.HelloRandomized
	case "randomized-alpn":
		return utls.HelloRandomizedALPN
	case "randomized-no-alpn":
		return utls.HelloRandomizedNoALPN
	default:
		return utls.HelloGolang
	}
}
