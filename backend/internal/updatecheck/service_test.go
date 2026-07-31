package updatecheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/buildinfo"
)

const (
	currentCommit = "1111111111111111111111111111111111111111"
	latestCommit  = "2222222222222222222222222222222222222222"
)

func TestRefreshClassifiesCommitRelationship(t *testing.T) {
	for _, test := range []struct {
		name           string
		build          buildinfo.Info
		compareStatus  string
		want           Status
		wantCompareHit bool
	}{
		{name: "identical", build: testBuild(latestCommit), want: StatusUpToDate},
		{name: "update available", build: testBuild(currentCommit), compareStatus: "ahead", want: StatusUpdateAvailable, wantCompareHit: true},
		{name: "running ahead", build: testBuild(currentCommit), compareStatus: "behind", want: StatusRunningAhead, wantCompareHit: true},
		{name: "diverged", build: testBuild(currentCommit), compareStatus: "diverged", want: StatusUnknown, wantCompareHit: true},
		{name: "development build", build: buildinfo.Info{Version: "dev", Commit: "unknown"}, want: StatusUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			compareHits := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertGitHubHeaders(t, r)
				switch {
				case r.URL.Path == "/repos/KnowSky404/N2API/releases/latest":
					writeRelease(t, w, latestCommit)
				case strings.HasPrefix(r.URL.Path, "/repos/KnowSky404/N2API/compare/"):
					compareHits++
					if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "1" {
						t.Fatalf("compare query = %q", r.URL.RawQuery)
					}
					fmt.Fprintf(w, `{"status":%q}`, test.compareStatus)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			service := newTestService(server.URL, test.build, time.Now)
			got, err := service.Refresh(context.Background())
			if err != nil {
				t.Fatalf("Refresh returned error: %v", err)
			}
			if got.Status != test.want || got.Stale || got.ErrorCode != "" {
				t.Fatalf("snapshot = %+v, want status %q without error", got, test.want)
			}
			if got.Latest == nil || got.Latest.Version != "2026073101" || got.Latest.TargetCommit != latestCommit {
				t.Fatalf("latest = %+v", got.Latest)
			}
			if (compareHits > 0) != test.wantCompareHit {
				t.Fatalf("compare hits = %d, want hit %t", compareHits, test.wantCompareHit)
			}
		})
	}
}

func TestRefreshReusesETagAndCachedRelease(t *testing.T) {
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		if requests == 1 {
			w.Header().Set("ETag", `"release-v1"`)
			writeRelease(t, w, latestCommit)
			return
		}
		if got := r.Header.Get("If-None-Match"); got != `"release-v1"` {
			t.Fatalf("If-None-Match = %q", got)
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	service := newTestService(server.URL, testBuild(latestCommit), func() time.Time { return now })

	first, err := service.Refresh(context.Background())
	if err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	now = now.Add(2 * time.Minute)
	second, err := service.Refresh(context.Background())
	if err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	if second.Status != StatusUpToDate || second.Latest == nil || second.CheckedAt == first.CheckedAt {
		t.Fatalf("second snapshot = %+v, first = %+v", second, first)
	}
}

func TestRefreshPreservesLastSuccessAsStale(t *testing.T) {
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			writeRelease(t, w, latestCommit)
			return
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	service := newTestService(server.URL, testBuild(latestCommit), func() time.Time { return now })
	first, err := service.Refresh(context.Background())
	if err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	now = now.Add(2 * time.Minute)
	second, err := service.Refresh(context.Background())
	if err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	if !second.Stale || second.Status != StatusUpToDate || second.ErrorCode != stableUpdateCheckErrorCode || second.CheckedAt != first.CheckedAt || second.Latest == nil {
		t.Fatalf("stale snapshot = %+v", second)
	}
}

func TestRefreshRejectsMalformedRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"tag_name":"latest","name":"bad","body":"notes","html_url":"https://example.com/release","target_commitish":"main","published_at":"not-a-time"}`)
	}))
	defer server.Close()
	service := newTestService(server.URL, testBuild(currentCommit), time.Now)

	got, err := service.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if got.Status != StatusUnavailable || got.ErrorCode != stableUpdateCheckErrorCode || got.Latest != nil {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestManualRefreshEnforcesCooldown(t *testing.T) {
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRelease(t, w, latestCommit)
	}))
	defer server.Close()
	service := newTestService(server.URL, testBuild(latestCommit), func() time.Time { return now })
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	got, err := service.Refresh(context.Background())
	var cooldown *RefreshCooldownError
	if !errors.As(err, &cooldown) || cooldown.RetryAfter != time.Minute {
		t.Fatalf("second Refresh error = %#v", err)
	}
	if got.RefreshAllowedAt != now.Add(time.Minute).Format(time.RFC3339) {
		t.Fatalf("RefreshAllowedAt = %q", got.RefreshAllowedAt)
	}
}

func TestDisabledServiceDoesNotCallGitHub(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("disabled service made an HTTP request")
		return nil, nil
	})}
	service := NewService(Config{Enabled: false, Build: testBuild(currentCommit), Client: client})
	got, err := service.Refresh(context.Background())
	if err != nil || got.Status != StatusDisabled {
		t.Fatalf("Refresh = %+v, %v", got, err)
	}
}

func TestRefreshTimesOutWithoutLosingServiceState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	service := NewService(Config{
		Enabled: true, Build: testBuild(currentCommit), APIBaseURL: server.URL,
		RequestTimeout: 20 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	got, err := service.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if got.Status != StatusUnavailable || got.ErrorCode != stableUpdateCheckErrorCode {
		t.Fatalf("snapshot = %+v", got)
	}
}

func newTestService(apiURL string, build buildinfo.Info, now func() time.Time) *Service {
	return NewService(Config{
		Enabled: true, Build: build, APIBaseURL: apiURL, Now: now,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func testBuild(commit string) buildinfo.Info {
	return buildinfo.Info{Version: "sha-" + commit[:12], Commit: commit, BuiltAt: "2026-07-31T07:00:00Z"}
}

func writeRelease(t *testing.T, w http.ResponseWriter, targetCommit string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"tag_name":"2026073101","name":"N2API 2026073101","body":"### Features\n\n- Add updates","html_url":"https://github.com/KnowSky404/N2API/releases/tag/2026073101","target_commitish":%q,"published_at":"2026-07-31T02:14:12Z","draft":false,"prerelease":false}`, targetCommit)
}

func assertGitHubHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Header.Get("Accept") != githubJSONMediaType || r.Header.Get("X-GitHub-Api-Version") != githubAPIVersion || r.Header.Get("User-Agent") != githubUserAgent {
		t.Fatalf("GitHub headers = %#v", r.Header)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
