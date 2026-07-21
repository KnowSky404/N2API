package httpapi

import (
	"net/http"
	"net/url"
	"strings"
)

const (
	browserAppContentSecurityPolicy   = "default-src 'self'; base-uri 'self'; connect-src 'self'; font-src 'self'; form-action 'self'; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'"
	browserDataContentSecurityPolicy  = "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
	browserOAuthContentSecurityPolicy = browserDataContentSecurityPolicy + "; style-src 'unsafe-inline'"
	browserPermissionsPolicy          = "camera=(), geolocation=(), microphone=(), payment=(), usb=()"
)

func withBrowserSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setBrowserSecurityHeaders(w, r)
		if rejectCrossOriginAdminMutation(r) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func setBrowserSecurityHeaders(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Content-Security-Policy", browserContentSecurityPolicy(r.URL.Path))
	header.Set("Permissions-Policy", browserPermissionsPolicy)
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")

	info := requestInfoForRequest(r)
	if info.Scheme == "https" {
		header.Set("Strict-Transport-Security", "max-age=31536000")
	}
	if sensitiveBrowserResponsePath(r.URL.Path) {
		header.Set("Cache-Control", "no-store")
	}
}

func rejectCrossOriginAdminMutation(r *http.Request) bool {
	if r == nil || browserSafeMethod(r.Method) || !adminBrowserPath(r.URL.Path) {
		return false
	}
	_, hasSession := readSessionCookie(r)
	if !hasSession && r.URL.Path != "/api/admin/logout" {
		return false
	}

	fetchSite, fetchSitePresent, fetchSiteValid := singleBrowserHeader(r.Header, "Sec-Fetch-Site")
	if fetchSitePresent && (!fetchSiteValid || strings.ToLower(fetchSite) != "same-origin") {
		return true
	}
	origin, originPresent, originValid := singleBrowserHeader(r.Header, "Origin")
	if !originPresent {
		return false
	}
	if !originValid {
		return true
	}
	return !sameRequestOrigin(origin, requestInfoForRequest(r))
}

func singleBrowserHeader(header http.Header, name string) (string, bool, bool) {
	values := header.Values(name)
	if len(values) == 0 {
		return "", false, true
	}
	if len(values) != 1 {
		return "", true, false
	}
	value := strings.TrimSpace(values[0])
	return value, true, value != "" && !strings.Contains(value, ",")
}

func adminBrowserPath(path string) bool {
	return path == "/api/admin" || strings.HasPrefix(path, "/api/admin/")
}

func browserSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func sameRequestOrigin(rawOrigin string, info requestInfo) bool {
	scheme, host, ok := canonicalRequestOrigin(rawOrigin)
	if !ok || scheme != info.Scheme || info.Host == "" {
		return false
	}
	originHostname, originPort, ok := originHostPort(scheme, host)
	if !ok {
		return false
	}
	requestHostname, requestPort, ok := originHostPort(info.Scheme, info.Host)
	return ok && strings.EqualFold(originHostname, requestHostname) && originPort == requestPort
}

func originHostPort(scheme, host string) (string, string, bool) {
	parsed, err := url.Parse("//" + host)
	if err != nil || parsed.Hostname() == "" {
		return "", "", false
	}
	port := parsed.Port()
	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", "", false
		}
	}
	return parsed.Hostname(), port, true
}

func sensitiveBrowserResponsePath(path string) bool {
	return adminBrowserPath(path) || strings.HasPrefix(path, "/oauth/")
}

func browserContentSecurityPolicy(path string) string {
	if strings.HasPrefix(path, "/oauth/") {
		return browserOAuthContentSecurityPolicy
	}
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/v1/") {
		return browserDataContentSecurityPolicy
	}
	return browserAppContentSecurityPolicy
}
