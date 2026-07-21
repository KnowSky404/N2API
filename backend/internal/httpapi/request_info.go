package httpapi

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

type requestInfo struct {
	ClientIP string
	Scheme   string
	Host     string
}

type requestInfoContextKey struct{}

func resolveRequestInfo(r *http.Request, trustedProxies []netip.Prefix, publicURL string) requestInfo {
	if r == nil {
		return requestInfo{}
	}

	directPeer, directPeerValid := parseRequestAddr(r.RemoteAddr)
	info := requestInfo{
		Scheme: directRequestScheme(r),
		Host:   validRequestHost(r.Host),
	}
	if strings.TrimSpace(publicURL) != "" {
		info.Host = ""
		if scheme, host, ok := canonicalRequestOrigin(publicURL); ok {
			info.Scheme = scheme
			info.Host = host
		}
	}
	if directPeerValid {
		info.ClientIP = directPeer.String()
	}
	if !directPeerValid || !trustedProxyContains(trustedProxies, directPeer) {
		return info
	}

	if forwardedFor, present := forwardedForValues(r.Header); present {
		if chain, ok := parseForwardedFor(forwardedFor); ok {
			info.ClientIP = clientFromForwardedChain(directPeer, chain, trustedProxies).String()
		} else {
			return info
		}
	} else if realIP, ok := singleForwardedValue(r.Header, "X-Real-IP"); ok {
		if parsed, valid := parseRequestAddr(realIP); valid {
			info.ClientIP = parsed.String()
		}
	}

	if proto, ok := singleForwardedValue(r.Header, "X-Forwarded-Proto"); ok {
		proto = strings.ToLower(proto)
		if proto == "http" || proto == "https" {
			info.Scheme = proto
		}
	}
	if host, ok := singleForwardedValue(r.Header, "X-Forwarded-Host"); ok {
		if host = validRequestHost(host); host != "" {
			info.Host = host
		}
	}

	return info
}

func withRequestInfo(next http.Handler, trustedProxies []netip.Prefix, publicURL string) http.Handler {
	trusted := append([]netip.Prefix(nil), trustedProxies...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := resolveRequestInfo(r, trusted, publicURL)
		ctx := context.WithValue(r.Context(), requestInfoContextKey{}, info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestInfoFromContext(ctx context.Context) (requestInfo, bool) {
	info, ok := ctx.Value(requestInfoContextKey{}).(requestInfo)
	return info, ok
}

func requestInfoForRequest(r *http.Request) requestInfo {
	if info, ok := requestInfoFromContext(r.Context()); ok {
		return info
	}
	return resolveRequestInfo(r, nil, "")
}

func canonicalRequestOrigin(raw string) (string, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", false
	}
	host := validRequestHost(parsed.Host)
	if host == "" {
		return "", "", false
	}
	return parsed.Scheme, host, true
}

func directRequestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func forwardedForValues(header http.Header) ([]string, bool) {
	values := header.Values("X-Forwarded-For")
	return values, len(values) > 0
}

func parseForwardedFor(values []string) ([]netip.Addr, bool) {
	chain := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			addr, ok := parseRequestAddr(raw)
			if !ok {
				return nil, false
			}
			chain = append(chain, addr)
		}
	}
	if len(chain) == 0 {
		return nil, false
	}
	return chain, true
}

func clientFromForwardedChain(directPeer netip.Addr, chain []netip.Addr, trustedProxies []netip.Prefix) netip.Addr {
	client := directPeer
	for i := len(chain) - 1; i >= 0 && trustedProxyContains(trustedProxies, client); i-- {
		client = chain[i]
	}
	return client
}

func singleForwardedValue(header http.Header, name string) (string, bool) {
	values := header.Values(name)
	if len(values) != 1 {
		return "", false
	}
	value := strings.TrimSpace(values[0])
	if value == "" || strings.Contains(value, ",") {
		return "", false
	}
	return value, true
}

func parseRequestAddr(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Addr{}, false
	}

	addr, err := netip.ParseAddr(raw)
	if err != nil && strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		addr, err = netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]"))
	}
	if err != nil {
		addrPort, addrPortErr := netip.ParseAddrPort(raw)
		if addrPortErr != nil {
			return netip.Addr{}, false
		}
		addr = addrPort.Addr()
	}
	if addr.Zone() != "" {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func trustedProxyContains(prefixes []netip.Prefix, addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range prefixes {
		prefixAddr := prefix.Addr()
		if prefixAddr.Is4In6() && prefix.Bits() >= 96 {
			prefix = netip.PrefixFrom(prefixAddr.Unmap(), prefix.Bits()-96).Masked()
		}
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func validRequestHost(raw string) string {
	host := strings.TrimSpace(raw)
	if host == "" || strings.ContainsAny(host, ",/\\?#@\r\n\t ") {
		return ""
	}
	if strings.Count(host, ":") > 1 && !strings.HasPrefix(host, "[") {
		return ""
	}
	parsed, err := url.Parse("//" + host)
	if err != nil || parsed.Host != host || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if parsed.Hostname() == "" || strings.HasSuffix(host, ":") {
		return ""
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return ""
		}
	}
	return host
}
