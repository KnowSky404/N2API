package metrics

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"time"
)

func NewHTTPServer(addr, bearerToken string, handler http.Handler, baseContext context.Context) *http.Server {
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", bearerAuth(strings.TrimSpace(bearerToken), handler))
	if baseContext == nil {
		baseContext = context.Background()
	}
	server := &http.Server{
		Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10,
		BaseContext: func(net.Listener) context.Context { return baseContext },
	}
	return server
}

func bearerAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	expected := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		provided := []byte(req.Header.Get("Authorization"))
		if len(provided) != len(expected) || subtle.ConstantTimeCompare(provided, expected) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="n2api-metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, req)
	})
}
