package provider

import (
	"context"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
)

func TestModelProbeTLSFingerprintTransportCancelsBlockedResponseBody(t *testing.T) {
	headersWritten := make(chan struct{})
	releaseHandler := make(chan struct{})
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(headersWritten)
		select {
		case <-r.Context().Done():
		case <-releaseHandler:
		}
	}))
	defer func() {
		close(releaseHandler)
		upstream.Close()
	}()

	pool := x509.NewCertPool()
	pool.AddCert(upstream.Certificate())
	transport := newModelProbeTLSFingerprintTransport(http.DefaultTransport, "").(*modelProbeTLSFingerprintTransport)
	transport.tlsConfig = &utls.Config{RootCAs: pool}
	client := &http.Client{Transport: transport}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext returned error: %v", err)
	}
	req = req.WithContext(contextWithModelProbeTLSFingerprint(req.Context(), "chrome"))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do returned error before response headers: %v", err)
	}
	defer resp.Body.Close()
	<-headersWritten

	body, ok := resp.Body.(*modelProbeCloseWithConn)
	if !ok {
		t.Fatalf("response body type = %T, want *modelProbeCloseWithConn", resp.Body)
	}
	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.ReadAll(resp.Body)
		readDone <- readErr
	}()

	select {
	case readErr := <-readDone:
		if readErr == nil {
			t.Fatal("blocked response body read returned nil error after context deadline")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked response body read did not exit after context deadline")
	}
	select {
	case <-body.watcherDone:
	case <-time.After(time.Second):
		t.Fatal("context watcher did not exit after closing the connection")
	}
}

func TestModelProbeTLSFingerprintTransportUsesBaseWithoutFingerprint(t *testing.T) {
	baseCalled := false
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		baseCalled = true
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       http.NoBody,
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	transport := newModelProbeTLSFingerprintTransport(base, "http://proxy.example.test:8080")
	req, err := http.NewRequest(http.MethodGet, "https://upstream.example.test", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	defer resp.Body.Close()
	if !baseCalled || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("base called=%v status=%d, want base response", baseCalled, resp.StatusCode)
	}
}
