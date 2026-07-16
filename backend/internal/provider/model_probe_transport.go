package provider

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
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
	base   http.RoundTripper
	dialer *net.Dialer
}

func newModelProbeTLSFingerprintTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &modelProbeTLSFingerprintTransport{
		base: base,
		dialer: &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		},
	}
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
	conn, err := t.dialer.DialContext(req.Context(), "tcp", addr)
	if err != nil {
		return nil, err
	}
	uconn := utls.UClient(conn, &utls.Config{
		ServerName: host,
		NextProtos: []string{"http/1.1"},
	}, modelProbeClientHelloID(fingerprint))
	if err := uconn.HandshakeContext(req.Context()); err != nil {
		_ = uconn.Close()
		return nil, err
	}
	if negotiated := uconn.ConnectionState().NegotiatedProtocol; negotiated != "" && negotiated != "http/1.1" {
		_ = uconn.Close()
		return nil, fmt.Errorf("unsupported negotiated protocol %q for fingerprint transport", negotiated)
	}
	if err := req.Write(uconn); err != nil {
		_ = uconn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(uconn), req)
	if err != nil {
		_ = uconn.Close()
		return nil, err
	}
	resp.Body = &modelProbeCloseWithConn{ReadCloser: resp.Body, conn: uconn}
	return resp, nil
}

type modelProbeCloseWithConn struct {
	io.ReadCloser
	conn net.Conn
}

func (b *modelProbeCloseWithConn) Close() error {
	bodyErr := b.ReadCloser.Close()
	connErr := b.conn.Close()
	if bodyErr != nil {
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
