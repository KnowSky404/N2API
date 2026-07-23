package gateway

import (
	"net/http"
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

	wrapped, ok := newTLSFingerprintTransport(base, timeouts).(*tlsFingerprintTransport)
	if !ok {
		t.Fatal("fingerprint transport has unexpected type")
	}
	if wrapped.base != http.RoundTripper(base) || wrapped.dialer.Timeout != timeouts.connect || wrapped.tlsHandshakeTimeout != timeouts.tlsHandshake || wrapped.responseHeaderTimeout != timeouts.responseHeader {
		t.Fatalf("fingerprint transport timeouts = connect:%s tls:%s header:%s", wrapped.dialer.Timeout, wrapped.tlsHandshakeTimeout, wrapped.responseHeaderTimeout)
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
