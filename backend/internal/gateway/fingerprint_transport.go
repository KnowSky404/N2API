package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

type upstreamTimeouts struct {
	connect        time.Duration
	tlsHandshake   time.Duration
	responseHeader time.Duration
}

func newUpstreamTransport(timeouts upstreamTimeouts) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{
		Timeout:   timeouts.connect,
		KeepAlive: 30 * time.Second,
	}).DialContext
	transport.TLSHandshakeTimeout = timeouts.tlsHandshake
	transport.ResponseHeaderTimeout = timeouts.responseHeader
	return transport
}

type selectedAccountTransport struct {
	standard    *http.Transport
	fingerprint *http.Transport
}

func newSelectedAccountTransport(proxyURL *url.URL, fingerprint string, timeouts upstreamTimeouts) *selectedAccountTransport {
	standard := newUpstreamTransport(timeouts)
	if proxyURL != nil {
		standard.Proxy = http.ProxyURL(proxyURL)
	}

	transport := &selectedAccountTransport{standard: standard}
	fingerprint = normalizeTLSFingerprintName(fingerprint)
	if fingerprint == "" {
		return transport
	}

	fingerprintTransport := newUpstreamTransport(timeouts)
	fingerprintTransport.ForceAttemptHTTP2 = false
	fingerprintTransport.Proxy = nil
	dialer := newFingerprintTLSDialer(proxyURL, fingerprint, timeouts)
	fingerprintTransport.DialTLSContext = dialer.DialTLSContext
	transport.fingerprint = fingerprintTransport
	return transport
}

func (t *selectedAccountTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.fingerprint != nil && strings.EqualFold(req.URL.Scheme, "https") {
		return t.fingerprint.RoundTrip(req)
	}
	return t.standard.RoundTrip(req)
}

func (t *selectedAccountTransport) CloseIdleConnections() {
	t.standard.CloseIdleConnections()
	if t.fingerprint != nil {
		t.fingerprint.CloseIdleConnections()
	}
}

type fingerprintTLSDialer struct {
	dialer         *net.Dialer
	proxyURL       *url.URL
	fingerprint    string
	connectTimeout time.Duration
	tlsTimeout     time.Duration
	utlsConfig     *utls.Config
	proxyTLSConfig *tls.Config
}

func newFingerprintTLSDialer(proxyURL *url.URL, fingerprint string, timeouts upstreamTimeouts) *fingerprintTLSDialer {
	return &fingerprintTLSDialer{
		dialer: &net.Dialer{
			Timeout:   timeouts.connect,
			KeepAlive: 30 * time.Second,
		},
		proxyURL:       proxyURL,
		fingerprint:    normalizeTLSFingerprintName(fingerprint),
		connectTimeout: timeouts.connect,
		tlsTimeout:     timeouts.tlsHandshake,
	}
}

func (d *fingerprintTLSDialer) DialTLSContext(ctx context.Context, network, targetAddr string) (net.Conn, error) {
	dialAddr := targetAddr
	if d.proxyURL != nil {
		dialAddr = canonicalProxyAddress(d.proxyURL)
	}
	conn, err := d.dialer.DialContext(ctx, network, dialAddr)
	if err != nil {
		return nil, fmt.Errorf("dial upstream transport: %w", err)
	}
	rawConn := conn
	succeeded := false
	defer func() {
		if !succeeded {
			_ = rawConn.Close()
		}
	}()

	if d.proxyURL != nil {
		tunnelCtx := ctx
		cancelTunnel := func() {}
		if d.connectTimeout > 0 {
			tunnelCtx, cancelTunnel = context.WithTimeout(ctx, d.connectTimeout)
		}
		closeOnTunnelCancel := context.AfterFunc(tunnelCtx, func() { _ = rawConn.Close() })
		tunneledConn, tunnelErr := d.connectProxyTunnel(tunnelCtx, conn, targetAddr)
		tunnelContextErr := tunnelCtx.Err()
		closeOnTunnelCancel()
		cancelTunnel()
		if tunnelErr != nil && tunnelContextErr != nil {
			return nil, fmt.Errorf("proxy CONNECT canceled: %w", tunnelContextErr)
		}
		err = tunnelErr
		if err != nil {
			return nil, err
		}
		conn = tunneledConn
	}

	host, _, splitErr := net.SplitHostPort(targetAddr)
	if splitErr != nil {
		host = targetAddr
	}
	tlsConfig := &utls.Config{}
	if d.utlsConfig != nil {
		tlsConfig = d.utlsConfig.Clone()
	}
	tlsConfig.ServerName = host
	tlsConfig.NextProtos = []string{"http/1.1"}
	uconn := utls.UClient(conn, tlsConfig, clientHelloIDForFingerprint(d.fingerprint))
	handshakeCtx := ctx
	cancelHandshake := func() {}
	if d.tlsTimeout > 0 {
		handshakeCtx, cancelHandshake = context.WithTimeout(ctx, d.tlsTimeout)
	}
	err = uconn.HandshakeContext(handshakeCtx)
	cancelHandshake()
	if err != nil {
		return nil, fmt.Errorf("upstream TLS handshake failed: %w", err)
	}
	if negotiated := uconn.ConnectionState().NegotiatedProtocol; negotiated != "" && negotiated != "http/1.1" {
		return nil, fmt.Errorf("upstream negotiated unsupported protocol")
	}
	succeeded = true
	return uconn, nil
}

func (d *fingerprintTLSDialer) connectProxyTunnel(ctx context.Context, conn net.Conn, targetAddr string) (net.Conn, error) {
	if strings.EqualFold(d.proxyURL.Scheme, "https") {
		proxyTLSConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		if d.proxyTLSConfig != nil {
			proxyTLSConfig = d.proxyTLSConfig.Clone()
		}
		proxyTLSConfig.ServerName = d.proxyURL.Hostname()
		proxyTLSConfig.NextProtos = []string{"http/1.1"}
		tlsConn := tls.Client(conn, proxyTLSConfig)
		handshakeCtx := ctx
		cancelHandshake := func() {}
		if d.tlsTimeout > 0 {
			handshakeCtx, cancelHandshake = context.WithTimeout(ctx, d.tlsTimeout)
		}
		err := tlsConn.HandshakeContext(handshakeCtx)
		cancelHandshake()
		if err != nil {
			return nil, fmt.Errorf("proxy TLS handshake failed")
		}
		conn = tlsConn
	}

	connectReq := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: targetAddr},
		Host:   targetAddr,
		Header: make(http.Header),
	}
	if d.proxyURL.User != nil {
		username := d.proxyURL.User.Username()
		password, _ := d.proxyURL.User.Password()
		credentials := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		connectReq.Header.Set("Proxy-Authorization", "Basic "+credentials)
	}
	if err := connectReq.Write(conn); err != nil {
		return nil, fmt.Errorf("write proxy CONNECT request")
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, connectReq)
	if err != nil {
		return nil, fmt.Errorf("read proxy CONNECT response")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy CONNECT failed with status %d", resp.StatusCode)
	}
	if reader.Buffered() > 0 {
		return &bufferedConn{Conn: conn, reader: reader}, nil
	}
	return conn, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func canonicalProxyAddress(proxyURL *url.URL) string {
	port := proxyURL.Port()
	if port == "" {
		if strings.EqualFold(proxyURL.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(strings.ToLower(proxyURL.Hostname()), port)
}

func clientHelloIDForFingerprint(fingerprint string) utls.ClientHelloID {
	switch normalizeTLSFingerprintName(fingerprint) {
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
