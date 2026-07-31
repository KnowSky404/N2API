package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/KnowSky404/N2API/backend/internal/buildinfo"
)

const (
	defaultAPIBaseURL          = "https://api.github.com"
	defaultCheckInterval       = 6 * time.Hour
	defaultRequestTimeout      = 5 * time.Second
	defaultRefreshCooldown     = time.Minute
	maxGitHubResponseBytes     = 1 << 20
	maxReleaseNotesBytes       = 256 << 10
	githubAPIVersion           = "2026-03-10"
	githubJSONMediaType        = "application/vnd.github+json"
	githubUserAgent            = "N2API-update-checker"
	stableUpdateCheckErrorCode = "update_check_failed"
)

var (
	calVerPattern = regexp.MustCompile(`^[0-9]{10}$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type Status string

const (
	StatusDisabled        Status = "disabled"
	StatusUnavailable     Status = "unavailable"
	StatusUnknown         Status = "unknown"
	StatusUpToDate        Status = "up_to_date"
	StatusUpdateAvailable Status = "update_available"
	StatusRunningAhead    Status = "running_ahead"
)

type Release struct {
	Version      string `json:"version"`
	Name         string `json:"name"`
	PublishedAt  string `json:"publishedAt"`
	URL          string `json:"url"`
	TargetCommit string `json:"targetCommit"`
	Notes        string `json:"notes"`
	Image        string `json:"image"`
}

type Snapshot struct {
	Status           Status         `json:"status"`
	Current          buildinfo.Info `json:"current"`
	Latest           *Release       `json:"latest,omitempty"`
	CheckedAt        string         `json:"checkedAt,omitempty"`
	RefreshAllowedAt string         `json:"refreshAllowedAt,omitempty"`
	Stale            bool           `json:"stale"`
	ErrorCode        string         `json:"errorCode,omitempty"`
}

type Config struct {
	Enabled         bool
	Build           buildinfo.Info
	Client          *http.Client
	APIBaseURL      string
	CheckInterval   time.Duration
	RequestTimeout  time.Duration
	RefreshCooldown time.Duration
	Now             func() time.Time
	Logger          *slog.Logger
}

type RefreshCooldownError struct {
	RetryAfter time.Duration
}

func (e *RefreshCooldownError) Error() string {
	return "update check refresh is cooling down"
}

type Service struct {
	enabled         bool
	build           buildinfo.Info
	client          *http.Client
	apiBaseURL      string
	checkInterval   time.Duration
	requestTimeout  time.Duration
	refreshCooldown time.Duration
	now             func() time.Time
	logger          *slog.Logger

	refreshMu sync.Mutex
	mu        sync.RWMutex
	snapshot  Snapshot
	lastTry   time.Time
	etag      string
	cached    *Release
}

func NewService(cfg Config) *Service {
	if strings.TrimSpace(cfg.APIBaseURL) == "" {
		cfg.APIBaseURL = defaultAPIBaseURL
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = defaultCheckInterval
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if cfg.RefreshCooldown <= 0 {
		cfg.RefreshCooldown = defaultRefreshCooldown
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{
			Timeout: cfg.RequestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	status := StatusUnavailable
	if !cfg.Enabled {
		status = StatusDisabled
	}
	return &Service{
		enabled: cfg.Enabled, build: cfg.Build, client: cfg.Client,
		apiBaseURL: strings.TrimRight(cfg.APIBaseURL, "/"), checkInterval: cfg.CheckInterval,
		requestTimeout: cfg.RequestTimeout, refreshCooldown: cfg.RefreshCooldown,
		now: cfg.Now, logger: cfg.Logger,
		snapshot: Snapshot{Status: status, Current: cfg.Build},
	}
}

func (s *Service) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.snapshot, s.refreshAllowedAtLocked())
}

func (s *Service) Refresh(ctx context.Context) (Snapshot, error) {
	if !s.enabled {
		return s.Snapshot(), nil
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	now := s.now().UTC()
	s.mu.Lock()
	if !s.lastTry.IsZero() && now.Before(s.lastTry.Add(s.refreshCooldown)) {
		retryAfter := s.lastTry.Add(s.refreshCooldown).Sub(now)
		snapshot := cloneSnapshot(s.snapshot, s.lastTry.Add(s.refreshCooldown))
		s.mu.Unlock()
		return snapshot, &RefreshCooldownError{RetryAfter: retryAfter}
	}
	s.lastTry = now
	s.mu.Unlock()

	s.refreshOnce(ctx, now)
	return s.Snapshot(), nil
}

func (s *Service) Run(ctx context.Context) {
	if !s.enabled {
		<-ctx.Done()
		return
	}
	s.refreshScheduled(ctx)
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshScheduled(ctx)
		}
	}
}

func (s *Service) refreshScheduled(ctx context.Context) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if ctx.Err() != nil {
		return
	}
	now := s.now().UTC()
	s.mu.Lock()
	s.lastTry = now
	s.mu.Unlock()
	s.refreshOnce(ctx, now)
}

func (s *Service) refreshOnce(ctx context.Context, checkedAt time.Time) {
	requestCtx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()

	release, etag, err := s.fetchLatestRelease(requestCtx)
	if err == nil {
		var status Status
		status, err = s.classify(requestCtx, release.TargetCommit)
		if err == nil {
			s.recordSuccess(status, release, etag, checkedAt)
			return
		}
	}
	if ctx.Err() != nil {
		return
	}
	s.recordFailure()
	s.logger.Warn("release update check failed", "error_code", stableUpdateCheckErrorCode, "error", err)
}

func (s *Service) fetchLatestRelease(ctx context.Context) (Release, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiBaseURL+"/repos/KnowSky404/N2API/releases/latest", nil)
	if err != nil {
		return Release{}, "", err
	}
	s.setGitHubHeaders(req)
	s.mu.RLock()
	etag := s.etag
	cached := cloneRelease(s.cached)
	s.mu.RUnlock()
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return Release{}, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		if cached == nil {
			return Release{}, "", errors.New("GitHub returned 304 without a cached release")
		}
		return *cached, etag, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, "", fmt.Errorf("GitHub latest release returned status %d", resp.StatusCode)
	}
	body, err := readBounded(resp.Body, maxGitHubResponseBytes)
	if err != nil {
		return Release{}, "", err
	}
	var payload struct {
		TagName         string `json:"tag_name"`
		Name            string `json:"name"`
		Body            string `json:"body"`
		HTMLURL         string `json:"html_url"`
		TargetCommitish string `json:"target_commitish"`
		PublishedAt     string `json:"published_at"`
		Draft           bool   `json:"draft"`
		Prerelease      bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Release{}, "", fmt.Errorf("decode GitHub latest release: %w", err)
	}
	release, err := normalizeRelease(payload.TagName, payload.Name, payload.Body, payload.HTMLURL, payload.TargetCommitish, payload.PublishedAt, payload.Draft, payload.Prerelease)
	if err != nil {
		return Release{}, "", err
	}
	return release, strings.TrimSpace(resp.Header.Get("ETag")), nil
}

func (s *Service) classify(ctx context.Context, latestCommit string) (Status, error) {
	currentCommit := strings.TrimSpace(s.build.Commit)
	if !commitPattern.MatchString(currentCommit) {
		return StatusUnknown, nil
	}
	if currentCommit == latestCommit {
		return StatusUpToDate, nil
	}
	compareURL := fmt.Sprintf("%s/repos/KnowSky404/N2API/compare/%s...%s?page=2&per_page=1", s.apiBaseURL, currentCommit, latestCommit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, compareURL, nil)
	if err != nil {
		return StatusUnavailable, err
	}
	s.setGitHubHeaders(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return StatusUnavailable, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return StatusUnavailable, fmt.Errorf("GitHub compare returned status %d", resp.StatusCode)
	}
	body, err := readBounded(resp.Body, maxGitHubResponseBytes)
	if err != nil {
		return StatusUnavailable, err
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return StatusUnavailable, fmt.Errorf("decode GitHub compare: %w", err)
	}
	switch payload.Status {
	case "identical":
		return StatusUpToDate, nil
	case "ahead":
		return StatusUpdateAvailable, nil
	case "behind":
		return StatusRunningAhead, nil
	case "diverged":
		return StatusUnknown, nil
	default:
		return StatusUnavailable, fmt.Errorf("unknown GitHub compare status %q", payload.Status)
	}
}

func (s *Service) setGitHubHeaders(req *http.Request) {
	req.Header.Set("Accept", githubJSONMediaType)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", githubUserAgent)
}

func (s *Service) recordSuccess(status Status, release Release, etag string, checkedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.etag = etag
	s.cached = cloneRelease(&release)
	s.snapshot = Snapshot{
		Status: status, Current: s.build, Latest: cloneRelease(&release),
		CheckedAt: checkedAt.UTC().Format(time.RFC3339), Stale: false,
	}
}

func (s *Service) recordFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot.CheckedAt == "" {
		s.snapshot = Snapshot{Status: StatusUnavailable, Current: s.build, ErrorCode: stableUpdateCheckErrorCode}
		return
	}
	s.snapshot.Stale = true
	s.snapshot.ErrorCode = stableUpdateCheckErrorCode
}

func (s *Service) refreshAllowedAtLocked() time.Time {
	if s.lastTry.IsZero() {
		return time.Time{}
	}
	return s.lastTry.Add(s.refreshCooldown)
}

func normalizeRelease(tag, name, notes, rawURL, targetCommit, publishedAt string, draft, prerelease bool) (Release, error) {
	tag = strings.TrimSpace(tag)
	targetCommit = strings.TrimSpace(targetCommit)
	if draft || prerelease || !calVerPattern.MatchString(tag) || !commitPattern.MatchString(targetCommit) {
		return Release{}, errors.New("GitHub latest release identity is invalid")
	}
	if !utf8.ValidString(notes) || len(notes) > maxReleaseNotesBytes {
		return Release{}, errors.New("GitHub latest release notes are invalid")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "N2API " + tag
	}
	if len(name) > 512 || !utf8.ValidString(name) {
		return Release{}, errors.New("GitHub latest release name is invalid")
	}
	parsedURL, err := url.Parse(strings.TrimSpace(rawURL))
	expectedPath := "/KnowSky404/N2API/releases/tag/" + tag
	if err != nil || parsedURL.Scheme != "https" || !strings.EqualFold(parsedURL.Host, "github.com") || parsedURL.Path != expectedPath || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return Release{}, errors.New("GitHub latest release URL is invalid")
	}
	published, err := time.Parse(time.RFC3339, strings.TrimSpace(publishedAt))
	if err != nil {
		return Release{}, errors.New("GitHub latest release publication time is invalid")
	}
	return Release{
		Version: tag, Name: name, PublishedAt: published.UTC().Format(time.RFC3339),
		URL: parsedURL.String(), TargetCommit: targetCommit, Notes: notes,
		Image: "ghcr.io/knowsky404/n2api:" + tag,
	}, nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("GitHub response exceeds the size limit")
	}
	return body, nil
}

func cloneSnapshot(snapshot Snapshot, refreshAllowedAt time.Time) Snapshot {
	snapshot.Latest = cloneRelease(snapshot.Latest)
	if !refreshAllowedAt.IsZero() {
		snapshot.RefreshAllowedAt = refreshAllowedAt.UTC().Format(time.RFC3339)
	}
	return snapshot
}

func cloneRelease(release *Release) *Release {
	if release == nil {
		return nil
	}
	copy := *release
	return &copy
}
