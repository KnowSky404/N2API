package httpapi

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestParseRequestAddrNormalizesSupportedForms(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{name: "IPv4", raw: "192.0.2.8", want: "192.0.2.8", ok: true},
		{name: "IPv4 with port", raw: "192.0.2.8:4321", want: "192.0.2.8", ok: true},
		{name: "mapped IPv4", raw: "::ffff:192.0.2.8", want: "192.0.2.8", ok: true},
		{name: "mapped IPv4 with port", raw: "[::ffff:192.0.2.8]:4321", want: "192.0.2.8", ok: true},
		{name: "IPv6", raw: "2001:db8::8", want: "2001:db8::8", ok: true},
		{name: "bracketed IPv6", raw: "[2001:db8::8]", want: "2001:db8::8", ok: true},
		{name: "IPv6 with port", raw: "[2001:db8::8]:4321", want: "2001:db8::8", ok: true},
		{name: "zone rejected", raw: "[fe80::1%eth0]:4321", ok: false},
		{name: "malformed", raw: "not-an-address", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseRequestAddr(tt.raw)
			if ok != tt.ok {
				t.Fatalf("parseRequestAddr(%q) ok = %t, want %t", tt.raw, ok, tt.ok)
			}
			if ok && got.String() != tt.want {
				t.Fatalf("parseRequestAddr(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestResolveRequestInfoEnforcesTrustedProxyBoundary(t *testing.T) {
	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("2001:db8:1::/48"),
	}
	tests := []struct {
		name       string
		remoteAddr string
		trusted    []netip.Prefix
		tls        bool
		host       string
		headers    http.Header
		want       requestInfo
	}{
		{
			name:       "forwarded values ignored by default",
			remoteAddr: "192.0.2.10:1234",
			host:       "direct.example:3000",
			headers: http.Header{
				"X-Forwarded-For":   {"203.0.113.8"},
				"X-Forwarded-Proto": {"https"},
				"X-Forwarded-Host":  {"public.example"},
			},
			want: requestInfo{ClientIP: "192.0.2.10", Scheme: "http", Host: "direct.example:3000"},
		},
		{
			name:       "single trusted proxy",
			remoteAddr: "10.0.0.2:1234",
			trusted:    trusted,
			host:       "direct.example:3000",
			headers: http.Header{
				"X-Forwarded-For":   {"203.0.113.8"},
				"X-Forwarded-Proto": {"HTTPS"},
				"X-Forwarded-Host":  {"public.example:8443"},
			},
			want: requestInfo{ClientIP: "203.0.113.8", Scheme: "https", Host: "public.example:8443"},
		},
		{
			name:       "multiple trusted proxies walked from right",
			remoteAddr: "10.0.0.3:1234",
			trusted:    trusted,
			tls:        true,
			host:       "direct.example",
			headers:    http.Header{"X-Forwarded-For": {"203.0.113.9, 10.0.0.1, 10.0.0.2"}},
			want:       requestInfo{ClientIP: "203.0.113.9", Scheme: "https", Host: "direct.example"},
		},
		{
			name:       "first untrusted hop stops spoofed prefix",
			remoteAddr: "10.0.0.3:1234",
			trusted:    trusted,
			host:       "direct.example",
			headers:    http.Header{"X-Forwarded-For": {"192.0.2.200, 198.51.100.7, 10.0.0.2"}},
			want:       requestInfo{ClientIP: "198.51.100.7", Scheme: "http", Host: "direct.example"},
		},
		{
			name:       "multiple XFF fields form one chain",
			remoteAddr: "10.0.0.3:1234",
			trusted:    trusted,
			host:       "direct.example",
			headers:    http.Header{"X-Forwarded-For": {"203.0.113.9", "10.0.0.1, 10.0.0.2"}},
			want:       requestInfo{ClientIP: "203.0.113.9", Scheme: "http", Host: "direct.example"},
		},
		{
			name:       "malformed XFF fails closed",
			remoteAddr: "10.0.0.3:1234",
			trusted:    trusted,
			host:       "direct.example",
			headers: http.Header{
				"X-Forwarded-For":   {"not-an-ip, 10.0.0.2"},
				"X-Real-Ip":         {"203.0.113.11"},
				"X-Forwarded-Proto": {"https"},
				"X-Forwarded-Host":  {"public.example"},
			},
			want: requestInfo{ClientIP: "10.0.0.3", Scheme: "http", Host: "direct.example"},
		},
		{
			name:       "real IP accepted only without XFF",
			remoteAddr: "[2001:db8:1::2]:1234",
			trusted:    trusted,
			host:       "[2001:db8::20]:3000",
			headers:    http.Header{"X-Real-Ip": {"[::ffff:203.0.113.12]:4567"}},
			want:       requestInfo{ClientIP: "203.0.113.12", Scheme: "http", Host: "[2001:db8::20]:3000"},
		},
		{
			name:       "mapped direct peer matches IPv4 trusted prefix",
			remoteAddr: "[::ffff:10.0.0.2]:1234",
			trusted:    trusted,
			host:       "direct.example",
			headers:    http.Header{"X-Forwarded-For": {"203.0.113.13"}},
			want:       requestInfo{ClientIP: "203.0.113.13", Scheme: "http", Host: "direct.example"},
		},
		{
			name:       "all trusted chain returns farthest visible hop",
			remoteAddr: "10.0.0.3:1234",
			trusted:    trusted,
			host:       "direct.example",
			headers:    http.Header{"X-Forwarded-For": {"10.0.0.1, 10.0.0.2"}},
			want:       requestInfo{ClientIP: "10.0.0.1", Scheme: "http", Host: "direct.example"},
		},
		{
			name:       "invalid forwarded proto and host fall back",
			remoteAddr: "10.0.0.2:1234",
			trusted:    trusted,
			tls:        true,
			host:       "direct.example",
			headers: http.Header{
				"X-Forwarded-Proto": {"javascript"},
				"X-Forwarded-Host":  {"public.example, attacker.example"},
			},
			want: requestInfo{ClientIP: "10.0.0.2", Scheme: "https", Host: "direct.example"},
		},
		{
			name:       "unbracketed IPv6 forwarded host rejected",
			remoteAddr: "10.0.0.2:1234",
			trusted:    trusted,
			host:       "direct.example",
			headers:    http.Header{"X-Forwarded-Host": {"2001:db8::20"}},
			want:       requestInfo{ClientIP: "10.0.0.2", Scheme: "http", Host: "direct.example"},
		},
		{
			name:       "multiple forwarded metadata fields rejected",
			remoteAddr: "10.0.0.2:1234",
			trusted:    trusted,
			host:       "direct.example",
			headers: http.Header{
				"X-Forwarded-Proto": {"https", "http"},
				"X-Forwarded-Host":  {"public.example", "attacker.example"},
			},
			want: requestInfo{ClientIP: "10.0.0.2", Scheme: "http", Host: "direct.example"},
		},
		{
			name:       "malformed direct peer cannot become trusted",
			remoteAddr: "bad-peer",
			trusted:    trusted,
			host:       "direct.example",
			headers: http.Header{
				"X-Forwarded-For":   {"203.0.113.14"},
				"X-Forwarded-Proto": {"https"},
				"X-Forwarded-Host":  {"public.example"},
			},
			want: requestInfo{Scheme: "http", Host: "direct.example"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://ignored.example/test", nil)
			req.RemoteAddr = tt.remoteAddr
			req.Host = tt.host
			req.Header = tt.headers
			if tt.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if got := resolveRequestInfo(req, tt.trusted, ""); got != tt.want {
				t.Fatalf("resolveRequestInfo() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRequestInfoMiddlewareStoresResolvedContext(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	var captured requestInfo
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = requestInfoForRequest(r)
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "http://direct.example/test", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.8")
	res := httptest.NewRecorder()

	withRequestInfo(next, trusted, "").ServeHTTP(res, req)

	want := requestInfo{ClientIP: "203.0.113.8", Scheme: "http", Host: "direct.example"}
	if captured != want {
		t.Fatalf("requestInfoForRequest() = %+v, want %+v", captured, want)
	}
	if _, ok := requestInfoFromContext(req.Context()); ok {
		t.Fatal("middleware mutated the original request context")
	}
}

func TestResolveRequestInfoUsesCanonicalPublicOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://attacker.example/test", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Host = "attacker.example"
	req.Header.Set("X-Forwarded-Proto", "http")
	req.Header.Set("X-Forwarded-Host", "forwarded-attacker.example")

	want := requestInfo{ClientIP: "192.0.2.10", Scheme: "https", Host: "n2api.example"}
	if got := resolveRequestInfo(req, nil, "https://n2api.example"); got != want {
		t.Fatalf("resolveRequestInfo() = %+v, want %+v", got, want)
	}
}

func TestRequestInfoForRequestFallsBackToDirectValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://direct.example/test", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.8")

	want := requestInfo{ClientIP: "192.0.2.10", Scheme: "https", Host: "direct.example"}
	if got := requestInfoForRequest(req); got != want {
		t.Fatalf("requestInfoForRequest() = %+v, want %+v", got, want)
	}
}
