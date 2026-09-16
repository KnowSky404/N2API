package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

// The Codex catalog has not yet hidden every model that the official Codex
// documentation has announced as deprecated for ChatGPT sign-in.
var codexOAuthDeprecatedModels = map[string]struct{}{
	"gpt-5.2":       {},
	"gpt-5.3-codex": {},
}

type oauthModelCatalogCacheKey struct {
	accountID     int64
	generation    uint64
	baseURL       string
	clientVersion string
	planType      string
}

type oauthModelCatalogCacheEntry struct {
	models    []AccountModelInput
	expiresAt time.Time
}

type oauthModelCatalogFlight struct {
	done  chan struct{}
	fetch oauthModelCatalogFetch
	err   error
}

type oauthModelCatalogFetch struct {
	models          []AccountModelInput
	attemptAt       time.Time
	source          string
	cacheHit        bool
	upstreamFetchMS *int64
	generation      uint64
}

// SyncOAuthAccountModels fetches the account-specific Codex model catalog and
// persists only current picker-visible API models.
func (s *Service) SyncOAuthAccountModels(ctx context.Context, accountID int64) ([]AccountModel, AccountModelSyncSummary, error) {
	return s.syncOAuthAccountModels(ctx, accountID, false)
}

// RefreshOAuthAccountModels bypasses a completed catalog cache entry while
// retaining in-flight coalescing for the same account/configuration generation.
func (s *Service) RefreshOAuthAccountModels(ctx context.Context, accountID int64) ([]AccountModel, AccountModelSyncSummary, error) {
	return s.syncOAuthAccountModels(ctx, accountID, true)
}

func (s *Service) syncOAuthAccountModels(ctx context.Context, accountID int64, forceRefresh bool) ([]AccountModel, AccountModelSyncSummary, error) {
	if accountID <= 0 {
		return nil, AccountModelSyncSummary{}, ErrInvalidInput
	}
	account, err := s.repo.FindAccountByID(ctx, s.cfg.Provider, accountID)
	if err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	accountType := strings.TrimSpace(account.AccountType)
	if accountType == "" {
		accountType = AccountTypeCodexOAuth
	}
	if accountType != AccountTypeCodexOAuth {
		return nil, AccountModelSyncSummary{}, ErrInvalidInput
	}
	selected, err := s.selectedAccountForModelSync(ctx, account)
	if err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	catalog, err := s.oauthModelCatalogWithOptions(ctx, account, selected, forceRefresh)
	if err != nil {
		s.recordOAuthModelCatalogFailure(accountID, catalog, err, -1)
		return nil, AccountModelSyncSummary{}, err
	}
	if !s.oauthModelCatalogGenerationCurrent(accountID, catalog.generation) {
		return nil, AccountModelSyncSummary{}, ErrOAuthModelCatalogStale
	}
	syncCtx := withProviderEventIntent(ctx, systemevent.EventIntent{
		Category: systemevent.CategoryAudit,
		Severity: systemevent.SeverityInfo,
		Action:   systemevent.ActionProviderAccountModelsSynced,
		Outcome:  systemevent.OutcomeSuccess,
		Target:   providerAccountTarget(accountID, accountDisplayName(account)),
		Metadata: map[string]any{
			"oauth_catalog_model_count": len(catalog.models),
			"oauth_catalog_source":      catalog.source,
			"oauth_catalog_cache_hit":   catalog.cacheHit,
		},
	})
	applyStarted := time.Now()
	models, summary, err := s.repo.SyncOAuthAccountModels(syncCtx, s.cfg.Provider, accountID, catalog.models, time.Now().UTC())
	localApplyMS := time.Since(applyStarted).Milliseconds()
	if err != nil {
		s.recordOAuthModelCatalogFailure(accountID, catalog, err, localApplyMS)
		return models, summary, err
	}
	if !s.oauthModelCatalogGenerationCurrent(accountID, catalog.generation) {
		return models, summary, ErrOAuthModelCatalogStale
	}
	s.recordOAuthModelCatalogSuccess(accountID, catalog, localApplyMS)
	return models, summary, nil
}

func (s *Service) oauthModelCatalog(ctx context.Context, account Account, selected SelectedAccount) ([]AccountModelInput, error) {
	catalog, err := s.oauthModelCatalogWithOptions(ctx, account, selected, false)
	if err != nil {
		s.recordOAuthModelCatalogFailure(account.ID, catalog, err, -1)
		return nil, err
	}
	s.recordOAuthModelCatalogSuccess(account.ID, catalog, -1)
	return cloneAccountModelInputs(catalog.models), nil
}

func (s *Service) oauthModelCatalogWithOptions(ctx context.Context, account Account, selected SelectedAccount, forceRefresh bool) (oauthModelCatalogFetch, error) {
	key := s.oauthModelCatalogCacheKey(account, selected)

	for {
		now := s.oauthModelCatalogNow()
		s.oauthModelCatalogMu.Lock()
		if s.oauthModelCatalogCache == nil {
			s.oauthModelCatalogCache = make(map[oauthModelCatalogCacheKey]oauthModelCatalogCacheEntry)
		}
		if s.oauthModelCatalogFlights == nil {
			s.oauthModelCatalogFlights = make(map[oauthModelCatalogCacheKey]*oauthModelCatalogFlight)
		}
		if !forceRefresh {
			if cached, ok := s.oauthModelCatalogCache[key]; ok && now.Before(cached.expiresAt) {
				fetch := oauthModelCatalogFetch{
					models:     cloneAccountModelInputs(cached.models),
					attemptAt:  now.UTC(),
					source:     "cache",
					cacheHit:   true,
					generation: key.generation,
				}
				s.oauthModelCatalogMu.Unlock()
				return fetch, nil
			}
		}
		if flight, ok := s.oauthModelCatalogFlights[key]; ok {
			s.oauthModelCatalogMu.Unlock()
			select {
			case <-ctx.Done():
				return oauthModelCatalogFetch{generation: key.generation, attemptAt: now.UTC()}, ctx.Err()
			case <-flight.done:
				if (errors.Is(flight.err, context.Canceled) || errors.Is(flight.err, context.DeadlineExceeded)) && ctx.Err() == nil {
					continue
				}
				fetch := flight.fetch
				fetch.models = cloneAccountModelInputs(fetch.models)
				return fetch, flight.err
			}
		}
		if len(s.oauthModelCatalogFlights) >= maxOAuthModelCatalogFlights {
			s.oauthModelCatalogMu.Unlock()
			return oauthModelCatalogFetch{
				generation: key.generation,
				attemptAt:  now.UTC(),
				source:     "admission",
			}, ErrOAuthModelCatalogBusy
		}

		flight := &oauthModelCatalogFlight{done: make(chan struct{})}
		s.oauthModelCatalogFlights[key] = flight
		s.oauthModelCatalogMu.Unlock()

		startedAt := time.Now()
		models, err := s.fetchOAuthModelCatalog(ctx, selected)
		fetch := oauthModelCatalogFetch{
			models:     cloneAccountModelInputs(models),
			attemptAt:  now.UTC(),
			source:     "upstream",
			cacheHit:   false,
			generation: key.generation,
		}
		fetchDuration := time.Since(startedAt).Milliseconds()
		fetch.upstreamFetchMS = &fetchDuration

		s.oauthModelCatalogMu.Lock()
		if err == nil && s.oauthModelCatalogGenerations[key.accountID] == key.generation {
			s.storeOAuthModelCatalogCacheLocked(key, oauthModelCatalogCacheEntry{
				models:    cloneAccountModelInputs(models),
				expiresAt: s.oauthModelCatalogNow().Add(s.cfg.OAuthModelCatalogCacheTTL),
			})
		}
		flight.fetch = fetch
		flight.err = err
		delete(s.oauthModelCatalogFlights, key)
		close(flight.done)
		s.oauthModelCatalogMu.Unlock()
		return fetch, err
	}
}

func (s *Service) storeOAuthModelCatalogCacheLocked(key oauthModelCatalogCacheKey, entry oauthModelCatalogCacheEntry) {
	if _, exists := s.oauthModelCatalogCache[key]; !exists && len(s.oauthModelCatalogCache) >= maxOAuthModelCatalogCacheEntries {
		var oldestKey oauthModelCatalogCacheKey
		var oldestExpiry time.Time
		for candidateKey, candidate := range s.oauthModelCatalogCache {
			if oldestExpiry.IsZero() || candidate.expiresAt.Before(oldestExpiry) {
				oldestKey = candidateKey
				oldestExpiry = candidate.expiresAt
			}
		}
		delete(s.oauthModelCatalogCache, oldestKey)
	}
	s.oauthModelCatalogCache[key] = entry
}

func (s *Service) oauthModelCatalogCacheKey(account Account, selected SelectedAccount) oauthModelCatalogCacheKey {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.CodexResponsesBaseURL), "/")
	planType := strings.ToLower(strings.TrimSpace(account.Metadata["plan_type"]))
	s.oauthModelCatalogMu.Lock()
	if s.oauthModelCatalogGenerations == nil {
		s.oauthModelCatalogGenerations = make(map[int64]uint64)
	}
	if _, exists := s.oauthModelCatalogGenerations[account.ID]; !exists {
		s.oauthModelCatalogGenerations[account.ID] = 0
	}
	generation := s.oauthModelCatalogGenerations[account.ID]
	s.oauthModelCatalogMu.Unlock()
	return oauthModelCatalogCacheKey{
		accountID:     account.ID,
		generation:    generation,
		baseURL:       baseURL,
		clientVersion: codexCatalogClientVersion(selected),
		planType:      planType,
	}
}

func (s *Service) OAuthModelCatalogStatus(_ context.Context, accountID int64) (OAuthModelCatalogStatus, error) {
	if accountID <= 0 {
		return OAuthModelCatalogStatus{}, ErrInvalidInput
	}
	s.oauthModelCatalogMu.Lock()
	defer s.oauthModelCatalogMu.Unlock()
	return cloneOAuthModelCatalogStatus(s.oauthModelCatalogStatus[accountID]), nil
}

func (s *Service) oauthModelCatalogGenerationCurrent(accountID int64, generation uint64) bool {
	s.oauthModelCatalogMu.Lock()
	defer s.oauthModelCatalogMu.Unlock()
	return s.oauthModelCatalogGenerations[accountID] == generation
}

func cloneOAuthModelCatalogStatus(status OAuthModelCatalogStatus) OAuthModelCatalogStatus {
	status.LastAttemptAt = cloneTimePointer(status.LastAttemptAt)
	status.LastSuccessAt = cloneTimePointer(status.LastSuccessAt)
	status.LastFailureAt = cloneTimePointer(status.LastFailureAt)
	status.CacheHit = cloneBoolPointer(status.CacheHit)
	status.UpstreamFetchMS = cloneInt64Pointer(status.UpstreamFetchMS)
	status.LocalApplyMS = cloneInt64Pointer(status.LocalApplyMS)
	return status
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func (s *Service) recordOAuthModelCatalogSuccess(accountID int64, catalog oauthModelCatalogFetch, localApplyMS int64) {
	if accountID <= 0 {
		return
	}
	now := s.oauthModelCatalogNow().UTC()
	status := OAuthModelCatalogStatus{
		LastAttemptAt:   cloneTimePointer(&catalog.attemptAt),
		LastSuccessAt:   &now,
		LastError:       "",
		Source:          catalog.source,
		CacheHit:        boolPointer(catalog.cacheHit),
		UpstreamFetchMS: cloneInt64Pointer(catalog.upstreamFetchMS),
	}
	if localApplyMS >= 0 {
		status.LocalApplyMS = &localApplyMS
	}
	s.oauthModelCatalogMu.Lock()
	if s.oauthModelCatalogGenerations[accountID] != catalog.generation {
		s.oauthModelCatalogMu.Unlock()
		return
	}
	if s.oauthModelCatalogStatus == nil {
		s.oauthModelCatalogStatus = make(map[int64]OAuthModelCatalogStatus)
	}
	s.oauthModelCatalogStatus[accountID] = mergeOAuthModelCatalogStatus(s.oauthModelCatalogStatus[accountID], status)
	s.oauthModelCatalogMu.Unlock()
}

func (s *Service) recordOAuthModelCatalogFailure(accountID int64, catalog oauthModelCatalogFetch, err error, localApplyMS int64) {
	if accountID <= 0 {
		return
	}
	now := s.oauthModelCatalogNow().UTC()
	attemptAt := catalog.attemptAt
	if attemptAt.IsZero() {
		attemptAt = now
	}
	status := OAuthModelCatalogStatus{
		LastAttemptAt:   cloneTimePointer(&attemptAt),
		LastFailureAt:   &now,
		LastError:       sanitizeCatalogError(err),
		Source:          catalog.source,
		CacheHit:        boolPointer(catalog.cacheHit),
		UpstreamFetchMS: cloneInt64Pointer(catalog.upstreamFetchMS),
	}
	if localApplyMS >= 0 {
		status.LocalApplyMS = &localApplyMS
	}
	s.oauthModelCatalogMu.Lock()
	if s.oauthModelCatalogGenerations[accountID] != catalog.generation {
		s.oauthModelCatalogMu.Unlock()
		return
	}
	if s.oauthModelCatalogStatus == nil {
		s.oauthModelCatalogStatus = make(map[int64]OAuthModelCatalogStatus)
	}
	s.oauthModelCatalogStatus[accountID] = mergeOAuthModelCatalogStatus(s.oauthModelCatalogStatus[accountID], status)
	s.oauthModelCatalogMu.Unlock()
}

func mergeOAuthModelCatalogStatus(previous, current OAuthModelCatalogStatus) OAuthModelCatalogStatus {
	if current.LastSuccessAt == nil {
		current.LastSuccessAt = previous.LastSuccessAt
	}
	if current.LastFailureAt == nil {
		current.LastFailureAt = previous.LastFailureAt
	}
	return current
}

func boolPointer(value bool) *bool {
	return &value
}

func sanitizeCatalogError(err error) string {
	if err == nil {
		return ""
	}
	return sanitizeDiagnosticMessage(err.Error())
}

func cloneAccountModelInputs(models []AccountModelInput) []AccountModelInput {
	if models == nil {
		return nil
	}
	return append([]AccountModelInput(nil), models...)
}

func (s *Service) fetchOAuthModelCatalog(ctx context.Context, selected SelectedAccount) ([]AccountModelInput, error) {
	targetURL, err := codexModelsURL(s.cfg.CodexResponsesBaseURL, codexCatalogClientVersion(selected))
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+selected.AuthorizationToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("ChatGPT-Account-ID", strings.TrimSpace(selected.ChatGPTAccountID))
	req.Header.Set("originator", DefaultCodexFingerprintOriginator)
	req.Header.Set("Version", DefaultCodexFingerprintVersion)
	req.Header.Set("User-Agent", DefaultCodexFingerprintUserAgent)
	for key, value := range selected.FingerprintHeaders {
		req.Header.Set(key, value)
	}
	if strings.TrimSpace(selected.FingerprintUA) != "" {
		req.Header.Set("User-Agent", strings.TrimSpace(selected.FingerprintUA))
	}

	client := s.httpClient.clientForProxy(selected.ProxyURL)
	if strings.TrimSpace(selected.FingerprintTLS) != "" {
		cloned := *client
		transport := newModelProbeTLSFingerprintTransport(client.Transport, selected.ProxyURL).(*modelProbeTLSFingerprintTransport)
		if s.httpClient.modelProbeTLSConfig != nil {
			transport.tlsConfig = s.httpClient.modelProbeTLSConfig.Clone()
		}
		if s.httpClient.modelProbeProxyTLSConfig != nil {
			transport.proxyTLSConfig = s.httpClient.modelProbeProxyTLSConfig.Clone()
		}
		cloned.Transport = transport
		client = &cloned
		req = req.WithContext(contextWithModelProbeTLSFingerprint(req.Context(), selected.FingerprintTLS))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("codex model catalog returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Models []struct {
			Slug           string          `json:"slug"`
			Visibility     string          `json:"visibility"`
			SupportedInAPI bool            `json:"supported_in_api"`
			Upgrade        json.RawMessage `json:"upgrade"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode codex model catalog: %w", err)
	}
	if parsed.Models == nil {
		return nil, errors.New("codex model catalog missing models array")
	}

	models := make([]AccountModelInput, 0, len(parsed.Models))
	for _, model := range parsed.Models {
		slug := strings.TrimSpace(model.Slug)
		_, deprecated := codexOAuthDeprecatedModels[slug]
		hasUpgrade := len(model.Upgrade) > 0 && string(model.Upgrade) != "null"
		if slug == "" || model.Visibility != "list" || !model.SupportedInAPI || hasUpgrade || deprecated {
			continue
		}
		models = append(models, AccountModelInput{Model: slug, Enabled: true})
	}
	if len(models) == 0 {
		return nil, errors.New("no current models found in codex model catalog")
	}
	normalized, err := normalizeAccountModelInputs(models)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

func codexCatalogClientVersion(selected SelectedAccount) string {
	for key, value := range selected.FingerprintHeaders {
		if strings.EqualFold(strings.TrimSpace(key), "Version") && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return DefaultCodexFingerprintVersion
}

func codexModelsURL(baseURL, clientVersion string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		trimmed = "https://chatgpt.com/backend-api/codex"
	}
	parsed, err := url.Parse(trimmed + "/models")
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("client_version", strings.TrimSpace(clientVersion))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func upstreamModelsURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed + "/models"
	}
	return trimmed + "/v1/models"
}
