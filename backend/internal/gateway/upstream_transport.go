package gateway

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
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

type selectedAccountTransport struct{ standard *http.Transport }

func newSelectedAccountTransport(proxyURL *url.URL, timeouts upstreamTimeouts) *selectedAccountTransport {
	standard := newUpstreamTransport(timeouts)
	if proxyURL != nil {
		standard.Proxy = http.ProxyURL(proxyURL)
	}
	return &selectedAccountTransport{standard: standard}
}
func (t *selectedAccountTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.standard.RoundTrip(req)
}
func (t *selectedAccountTransport) CloseIdleConnections() { t.standard.CloseIdleConnections() }
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
