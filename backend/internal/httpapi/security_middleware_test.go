package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/KnowSky404/N2API/backend/internal/config"
)

func TestBrowserSecurityHeadersCoverStaticAdminOAuthAndHTTPS(t *testing.T) {
	web := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><script>import('/app.js')</script><main>N2API</main>")},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService(), nil, web)

	tests := []struct {
		name, target string
		wantNoStore  bool
		wantHSTS     bool
		wantCSP      string
	}{
		{name: "static HTTP", target: "http://n2api.example/", wantCSP: browserAppContentSecurityPolicy},
		{name: "static HTTPS", target: "https://n2api.example/", wantHSTS: true, wantCSP: browserAppContentSecurityPolicy},
		{name: "admin API", target: "http://n2api.example/api/admin/bootstrap", wantNoStore: true, wantCSP: browserDataContentSecurityPolicy},
		{name: "OAuth callback", target: "http://n2api.example/oauth/openai/callback?code=secret", wantNoStore: true, wantCSP: browserOAuthContentSecurityPolicy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.target, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
			}
			assertBrowserSecurityHeaders(t, recorder.Header(), tt.wantCSP)
			if got := recorder.Header().Get("Cache-Control"); (got == "no-store") != tt.wantNoStore {
				t.Fatalf("Cache-Control = %q, want no-store %t", got, tt.wantNoStore)
			}
			if got := recorder.Header().Get("Strict-Transport-Security"); (got != "") != tt.wantHSTS {
				t.Fatalf("Strict-Transport-Security = %q, want present %t", got, tt.wantHSTS)
			}
		})
	}
}

func TestBrowserSecurityUsesConfiguredPublicHTTPSOrigin(t *testing.T) {
	server := NewServer(config.Config{PublicURL: "https://public.example"}, staticHealth{}, newFakeAdminService(), nil)
	req := httptest.NewRequest(http.MethodPost, "http://internal:3000/api/admin/logout", nil)
	req.Header.Set("Origin", "https://public.example")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s, want 204", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("Strict-Transport-Security = %q", got)
	}
}

func TestBrowserSecurityUsesTrustedProxyOriginOnly(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       int
		wantHSTS   bool
	}{
		{name: "trusted proxy", remoteAddr: "10.0.0.2:1234", want: http.StatusNoContent, wantHSTS: true},
		{name: "untrusted proxy", remoteAddr: "192.0.2.2:1234", want: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(config.Config{TrustedProxyCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}, staticHealth{}, newFakeAdminService(), nil)
			req := httptest.NewRequest(http.MethodPost, "http://internal:3000/api/admin/logout", nil)
			req.RemoteAddr = tt.remoteAddr
			req.Header.Set("X-Forwarded-Proto", "https")
			req.Header.Set("X-Forwarded-Host", "public.example")
			req.Header.Set("Origin", "https://public.example")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), tt.want)
			}
			if got := recorder.Header().Get("Strict-Transport-Security"); (got != "") != tt.wantHSTS {
				t.Fatalf("Strict-Transport-Security = %q, want present %t", got, tt.wantHSTS)
			}
		})
	}
}

func TestBrowserSecurityRejectsCrossOriginCookieMutationsBeforeHandler(t *testing.T) {
	tests := []struct {
		name        string
		origin      string
		extraOrigin string
		fetchSite   string
		want        int
	}{
		{name: "matching origin", origin: "http://n2api.example", fetchSite: "same-origin", want: http.StatusNoContent},
		{name: "matching default port", origin: "http://n2api.example", fetchSite: "same-origin", want: http.StatusNoContent},
		{name: "matching origin without fetch metadata", origin: "http://n2api.example", want: http.StatusNoContent},
		{name: "same-origin fetch metadata without origin", fetchSite: "same-origin", want: http.StatusNoContent},
		{name: "CLI without browser headers", want: http.StatusNoContent},
		{name: "cross origin", origin: "https://attacker.example", fetchSite: "cross-site", want: http.StatusForbidden},
		{name: "origin mismatch despite same-origin metadata", origin: "https://attacker.example", fetchSite: "same-origin", want: http.StatusForbidden},
		{name: "empty origin", origin: " ", fetchSite: "same-origin", want: http.StatusForbidden},
		{name: "duplicate origin", origin: "http://n2api.example", extraOrigin: "https://attacker.example", fetchSite: "same-origin", want: http.StatusForbidden},
		{name: "null origin", origin: "null", fetchSite: "cross-site", want: http.StatusForbidden},
		{name: "same site is not same origin", fetchSite: "same-site", want: http.StatusForbidden},
		{name: "conflicting fetch metadata", origin: "http://n2api.example", fetchSite: "cross-site", want: http.StatusForbidden},
		{name: "malformed fetch metadata", origin: "http://n2api.example", fetchSite: "same-origin, cross-site", want: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			admins := newFakeAdminService()
			server := NewServer(config.Config{}, staticHealth{}, admins, nil)
			target := "http://n2api.example/api/admin/logout"
			if tt.name == "matching default port" {
				target = "http://n2api.example:80/api/admin/logout"
			}
			req := httptest.NewRequest(http.MethodPost, target, nil)
			req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.extraOrigin != "" {
				req.Header.Add("Origin", tt.extraOrigin)
			}
			if tt.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tt.fetchSite)
			}
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), tt.want)
			}
			wantCalls := 0
			if tt.want == http.StatusNoContent {
				wantCalls = 1
			}
			if len(admins.logoutTokens) != wantCalls {
				t.Fatalf("logout calls = %d, want %d", len(admins.logoutTokens), wantCalls)
			}
			if tt.want == http.StatusForbidden && strings.TrimSpace(recorder.Body.String()) != `{"error":"forbidden"}` {
				t.Fatalf("body = %q, want uniform forbidden error", recorder.Body.String())
			}
			if recorder.Header().Get("X-Request-ID") == "" {
				t.Fatal("X-Request-ID is empty")
			}
		})
	}
}

func TestBrowserSecurityProtectsCookielessLogoutBrowserState(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		fetchSite  string
		wantStatus int
		wantClear  bool
	}{
		{
			name:       "cross-site browser",
			origin:     "https://attacker.example",
			fetchSite:  "cross-site",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "same-origin browser",
			origin:     "http://n2api.example",
			fetchSite:  "same-origin",
			wantStatus: http.StatusNoContent,
			wantClear:  true,
		},
		{
			name:       "CLI without browser headers",
			wantStatus: http.StatusNoContent,
			wantClear:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			admins := newFakeAdminService()
			server := NewServer(config.Config{}, staticHealth{}, admins, nil)
			req := httptest.NewRequest(http.MethodPost, "http://n2api.example/api/admin/logout", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tt.fetchSite)
			}
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), tt.wantStatus)
			}
			if got := recorder.Header().Get("Set-Cookie"); (got != "") != tt.wantClear {
				t.Fatalf("Set-Cookie = %q, want clearing cookie %t", got, tt.wantClear)
			}
			if len(admins.logoutTokens) != 0 {
				t.Fatalf("logout tokens = %+v, want no service call without session", admins.logoutTokens)
			}
		})
	}
}

func TestSameRequestOriginUsesSchemeHostAndEffectivePort(t *testing.T) {
	tests := []struct {
		name, origin string
		info         requestInfo
		want         bool
	}{
		{name: "host case and HTTP default port", origin: "http://N2API.EXAMPLE", info: requestInfo{Scheme: "http", Host: "n2api.example:80"}, want: true},
		{name: "HTTPS default port", origin: "https://n2api.example", info: requestInfo{Scheme: "https", Host: "n2api.example:443"}, want: true},
		{name: "IPv6", origin: "https://[2001:db8::1]:3000", info: requestInfo{Scheme: "https", Host: "[2001:db8::1]:3000"}, want: true},
		{name: "scheme mismatch", origin: "http://n2api.example", info: requestInfo{Scheme: "https", Host: "n2api.example"}},
		{name: "port mismatch", origin: "https://n2api.example:8443", info: requestInfo{Scheme: "https", Host: "n2api.example"}},
		{name: "path is not an origin", origin: "https://n2api.example/path", info: requestInfo{Scheme: "https", Host: "n2api.example"}},
		{name: "opaque origin", origin: "null", info: requestInfo{Scheme: "https", Host: "n2api.example"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameRequestOrigin(tt.origin, tt.info); got != tt.want {
				t.Fatalf("sameRequestOrigin(%q, %+v) = %t, want %t", tt.origin, tt.info, got, tt.want)
			}
		})
	}
}

func TestBrowserSecurityAllowsSafeCrossOriginRequestsAndCookielessLogin(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)

	me := httptest.NewRequest(http.MethodGet, "http://n2api.example/api/admin/me", nil)
	me.Header.Set("Origin", "https://attacker.example")
	me.Header.Set("Sec-Fetch-Site", "cross-site")
	me.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	meRecorder := httptest.NewRecorder()
	server.ServeHTTP(meRecorder, me)
	if meRecorder.Code != http.StatusOK {
		t.Fatalf("safe GET status = %d body=%s, want 200", meRecorder.Code, meRecorder.Body.String())
	}

	login := httptest.NewRequest(http.MethodPost, "http://n2api.example/api/admin/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	login.Header.Set("Origin", "https://attacker.example")
	login.Header.Set("Sec-Fetch-Site", "cross-site")
	loginRecorder := httptest.NewRecorder()
	server.ServeHTTP(loginRecorder, login)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("cookieless login status = %d body=%s, want 200", loginRecorder.Code, loginRecorder.Body.String())
	}
}

func TestBrowserSecurityPreservesDownloadsAndGatewayStreams(t *testing.T) {
	admins := newFakeAdminService()
	streamSawFlusher := false
	gateway := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, streamSawFlusher = w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte("data: ok\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	})
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService(), gateway)

	exportReq := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=csv", nil)
	exportReq.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	exportRecorder := httptest.NewRecorder()
	server.ServeHTTP(exportRecorder, exportReq)
	if exportRecorder.Code != http.StatusOK || exportRecorder.Header().Get("Content-Type") != "text/csv; charset=utf-8" {
		t.Fatalf("export status/type = %d/%q", exportRecorder.Code, exportRecorder.Header().Get("Content-Type"))
	}
	if !strings.Contains(exportRecorder.Header().Get("Content-Disposition"), "n2api-request-logs.csv") || exportRecorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("export disposition/cache = %q/%q", exportRecorder.Header().Get("Content-Disposition"), exportRecorder.Header().Get("Cache-Control"))
	}

	streamReq := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	streamReq.Header.Set("Origin", "https://attacker.example")
	streamReq.Header.Set("Sec-Fetch-Site", "cross-site")
	streamReq.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	streamRecorder := httptest.NewRecorder()
	server.ServeHTTP(streamRecorder, streamReq)
	if streamRecorder.Code != http.StatusOK || streamRecorder.Header().Get("Content-Type") != "text/event-stream" || streamRecorder.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("stream status/type/cache = %d/%q/%q", streamRecorder.Code, streamRecorder.Header().Get("Content-Type"), streamRecorder.Header().Get("Cache-Control"))
	}
	if !streamSawFlusher || streamRecorder.Body.String() != "data: ok\n\n" {
		t.Fatalf("stream flusher/body = %t/%q", streamSawFlusher, streamRecorder.Body.String())
	}
}

func assertBrowserSecurityHeaders(t *testing.T, header http.Header, wantCSP string) {
	t.Helper()
	for name, want := range map[string]string{
		"Content-Security-Policy": wantCSP,
		"Permissions-Policy":      browserPermissionsPolicy,
		"Referrer-Policy":         "no-referrer",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
	} {
		if got := header.Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}
