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
	"sync"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/secret"
)

const (
	defaultStateTTL      = 10 * time.Minute
	defaultRefreshWindow = 2 * time.Minute
)

var (
	ErrNotConfigured       = errors.New("provider not configured")
	ErrNotConnected        = errors.New("provider not connected")
	ErrInvalidState        = errors.New("invalid oauth state")
	ErrInvalidInput        = errors.New("invalid provider input")
	ErrAccountsDisabled    = errors.New("provider accounts disabled")
	ErrAccountsUnavailable = errors.New("provider accounts unavailable")
)

type Config struct {
	Provider      string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	AuthURL       string
	TokenURL      string
	Secret        string
	StateTTL      time.Duration
	RefreshWindow time.Duration
}

type Status struct {
	Provider             string     `json:"provider"`
	Configured           bool       `json:"configured"`
	Connected            bool       `json:"connected"`
	DisplayName          string     `json:"displayName"`
	AccessTokenExpiresAt *time.Time `json:"accessTokenExpiresAt"`
	LastRefreshAt        *time.Time `json:"lastRefreshAt"`
}

type ConnectResult struct {
	AuthorizationURL string
}

type OAuthState struct {
	Provider      string
	StateHash     string
	RedirectAfter string
	ExpiresAt     time.Time
	ConsumedAt    *time.Time
}

type Account struct {
	ID                    int64      `json:"id"`
	Provider              string     `json:"provider"`
	Subject               string     `json:"subject"`
	DisplayName           string     `json:"displayName"`
	EncryptedAccessToken  string     `json:"-"`
	EncryptedRefreshToken string     `json:"-"`
	AccessTokenExpiresAt  *time.Time `json:"accessTokenExpiresAt"`
	LastRefreshAt         *time.Time `json:"lastRefreshAt"`
	Enabled               bool       `json:"enabled"`
	Priority              int        `json:"priority"`
	LastUsedAt            *time.Time `json:"lastUsedAt"`
	LastError             string     `json:"lastError"`
	LastErrorAt           *time.Time `json:"lastErrorAt"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

type AccountUpdate struct {
	Enabled  *bool
	Priority *int
}

type SelectedToken struct {
	AccountID int64
	Token     string
}

type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	Subject      string
	DisplayName  string
}

type Repository interface {
	ListAccounts(ctx context.Context, provider string) ([]Account, error)
	FindAccount(ctx context.Context, provider string) (Account, error)
	FindAccountByID(ctx context.Context, provider string, id int64) (Account, error)
	SaveAccount(ctx context.Context, account Account) (Account, error)
	UpdateAccount(ctx context.Context, provider string, id int64, update AccountUpdate) (Account, error)
	DeleteAccount(ctx context.Context, provider string, id int64) error
	DeleteAccounts(ctx context.Context, provider string) error
	MarkAccountUsed(ctx context.Context, provider string, id int64, usedAt time.Time) error
	MarkAccountError(ctx context.Context, provider string, id int64, message string, at time.Time) error
	CreateState(ctx context.Context, state OAuthState) error
	ClaimState(ctx context.Context, provider, stateHash string, now time.Time) (OAuthState, error)
}

type OAuthClient interface {
	ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error)
	RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error)
}

type HTTPClient struct {
	client *http.Client
}

type Service struct {
	repo         Repository
	client       OAuthClient
	cfg          Config
	refreshMu    sync.Mutex
	refreshLocks map[int64]*sync.Mutex
}

func NewHTTPClient(client *http.Client) *HTTPClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPClient{client: client}
}

func (c *HTTPClient) ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("client_id", cfg.ClientID)
	values.Set("client_secret", cfg.ClientSecret)
	values.Set("redirect_uri", cfg.RedirectURL)
	return c.postToken(ctx, cfg.TokenURL, values)
}

func (c *HTTPClient) RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	values.Set("client_id", cfg.ClientID)
	values.Set("client_secret", cfg.ClientSecret)
	return c.postToken(ctx, cfg.TokenURL, values)
}

func NewService(repo Repository, client OAuthClient, cfg Config) *Service {
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	if cfg.StateTTL <= 0 {
		cfg.StateTTL = defaultStateTTL
	}
	if cfg.RefreshWindow <= 0 {
		cfg.RefreshWindow = defaultRefreshWindow
	}
	return &Service{
		repo:         repo,
		client:       client,
		cfg:          cfg,
		refreshLocks: make(map[int64]*sync.Mutex),
	}
}

func (c *HTTPClient) postToken(ctx context.Context, tokenURL string, values url.Values) (TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("create oauth token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("send oauth token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return TokenResponse{}, fmt.Errorf("oauth token endpoint returned status %d", resp.StatusCode)
	}

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Subject      string `json:"subject"`
		DisplayName  string `json:"display_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return TokenResponse{}, fmt.Errorf("decode oauth token response: %w", err)
	}
	return TokenResponse{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		ExpiresIn:    payload.ExpiresIn,
		Subject:      payload.Subject,
		DisplayName:  payload.DisplayName,
	}, nil
}

func (s *Service) Configured() bool {
	return strings.TrimSpace(s.cfg.ClientID) != "" &&
		strings.TrimSpace(s.cfg.ClientSecret) != "" &&
		strings.TrimSpace(s.cfg.RedirectURL) != "" &&
		strings.TrimSpace(s.cfg.AuthURL) != "" &&
		strings.TrimSpace(s.cfg.TokenURL) != "" &&
		strings.TrimSpace(s.cfg.Secret) != ""
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	status := Status{
		Provider:   s.cfg.Provider,
		Configured: s.Configured(),
	}

	account, err := s.repo.FindAccount(ctx, s.cfg.Provider)
	if err != nil {
		if errors.Is(err, ErrNotConnected) {
			return status, nil
		}
		return Status{}, err
	}

	status.Connected = true
	status.DisplayName = account.DisplayName
	status.AccessTokenExpiresAt = account.AccessTokenExpiresAt
	status.LastRefreshAt = account.LastRefreshAt
	return status, nil
}

func (s *Service) StartConnect(ctx context.Context, redirectAfter string) (ConnectResult, error) {
	if !s.Configured() {
		return ConnectResult{}, ErrNotConfigured
	}
	if strings.TrimSpace(redirectAfter) == "" {
		redirectAfter = "/"
	}

	state, err := secret.GenerateToken("oauth_state")
	if err != nil {
		return ConnectResult{}, fmt.Errorf("generate oauth state: %w", err)
	}

	if err := s.repo.CreateState(ctx, OAuthState{
		Provider:      s.cfg.Provider,
		StateHash:     secret.HashAPIKey(state),
		RedirectAfter: redirectAfter,
		ExpiresAt:     time.Now().Add(s.cfg.StateTTL),
	}); err != nil {
		return ConnectResult{}, err
	}

	authURL, err := url.Parse(s.cfg.AuthURL)
	if err != nil {
		return ConnectResult{}, fmt.Errorf("parse oauth authorization url: %w", err)
	}
	query := authURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", s.cfg.ClientID)
	query.Set("redirect_uri", s.cfg.RedirectURL)
	query.Set("state", state)
	authURL.RawQuery = query.Encode()

	return ConnectResult{AuthorizationURL: authURL.String()}, nil
}

func (s *Service) CompleteCallback(ctx context.Context, code, state string) (Account, error) {
	code = strings.TrimSpace(code)
	state = strings.TrimSpace(state)
	if code == "" || state == "" {
		return Account{}, ErrInvalidState
	}
	if !s.Configured() {
		return Account{}, ErrNotConfigured
	}

	stateHash := secret.HashAPIKey(state)
	claimed, err := s.repo.ClaimState(ctx, s.cfg.Provider, stateHash, time.Now())
	if err != nil {
		if errors.Is(err, ErrInvalidState) {
			return Account{}, ErrInvalidState
		}
		return Account{}, err
	}
	_ = claimed

	tokens, err := s.client.ExchangeCode(ctx, s.cfg, code)
	if err != nil {
		return Account{}, err
	}
	account, err := s.storeTokenResponse(ctx, tokens, nil)
	if err != nil {
		return Account{}, err
	}
	return account, nil
}

func (s *Service) ListAccounts(ctx context.Context) ([]Account, error) {
	return s.repo.ListAccounts(ctx, s.cfg.Provider)
}

func (s *Service) UpdateAccount(ctx context.Context, id int64, update AccountUpdate) (Account, error) {
	if id <= 0 {
		return Account{}, ErrInvalidInput
	}
	if update.Enabled == nil && update.Priority == nil {
		return Account{}, ErrInvalidInput
	}
	if update.Priority != nil && *update.Priority < 0 {
		return Account{}, ErrInvalidInput
	}
	return s.repo.UpdateAccount(ctx, s.cfg.Provider, id, update)
}

func (s *Service) DisconnectAccount(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalidInput
	}
	return s.repo.DeleteAccount(ctx, s.cfg.Provider, id)
}

func (s *Service) Disconnect(ctx context.Context) error {
	return s.repo.DeleteAccounts(ctx, s.cfg.Provider)
}

func (s *Service) AccessToken(ctx context.Context) (string, error) {
	selected, err := s.SelectAccessToken(ctx)
	if err != nil {
		return "", err
	}
	return selected.Token, nil
}

func (s *Service) AccessTokenForAccount(ctx context.Context, account Account) (string, error) {
	if account.AccessTokenExpiresAt == nil || account.AccessTokenExpiresAt.After(time.Now().Add(s.cfg.RefreshWindow)) {
		return secret.DecryptString(s.cfg.Secret, account.EncryptedAccessToken)
	}

	if account.ID > 0 {
		unlock := s.lockAccountRefresh(account.ID)
		defer unlock()

		latest, err := s.repo.FindAccountByID(ctx, s.cfg.Provider, account.ID)
		if err != nil {
			return "", err
		}
		if latest.AccessTokenExpiresAt == nil || latest.AccessTokenExpiresAt.After(time.Now().Add(s.cfg.RefreshWindow)) {
			return secret.DecryptString(s.cfg.Secret, latest.EncryptedAccessToken)
		}
		account = latest
	}

	refreshToken, err := secret.DecryptString(s.cfg.Secret, account.EncryptedRefreshToken)
	if err != nil {
		return "", err
	}
	tokens, err := s.client.RefreshToken(ctx, s.cfg, refreshToken)
	if err != nil {
		return "", err
	}
	refreshed, err := s.storeTokenResponse(ctx, tokens, &account)
	if err != nil {
		return "", err
	}
	return secret.DecryptString(s.cfg.Secret, refreshed.EncryptedAccessToken)
}

func (s *Service) SelectAccessToken(ctx context.Context, excludedAccountIDs ...int64) (SelectedToken, error) {
	if !s.Configured() {
		return SelectedToken{}, ErrNotConfigured
	}

	accounts, err := s.repo.ListAccounts(ctx, s.cfg.Provider)
	if err != nil {
		return SelectedToken{}, err
	}
	if len(accounts) == 0 {
		return SelectedToken{}, ErrNotConnected
	}

	excluded := make(map[int64]struct{}, len(excludedAccountIDs))
	for _, id := range excludedAccountIDs {
		if id > 0 {
			excluded[id] = struct{}{}
		}
	}

	hasEnabled := false
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		hasEnabled = true
		if _, ok := excluded[account.ID]; ok {
			continue
		}
		token, err := s.AccessTokenForAccount(ctx, account)
		if err != nil {
			if markErr := s.repo.MarkAccountError(ctx, s.cfg.Provider, account.ID, err.Error(), time.Now()); markErr != nil {
				return SelectedToken{}, fmt.Errorf("mark provider account error: %w", markErr)
			}
			continue
		}
		if err := s.repo.MarkAccountUsed(ctx, s.cfg.Provider, account.ID, time.Now()); err != nil {
			return SelectedToken{}, fmt.Errorf("mark provider account used: %w", err)
		}
		return SelectedToken{AccountID: account.ID, Token: token}, nil
	}
	if !hasEnabled {
		return SelectedToken{}, ErrAccountsDisabled
	}
	return SelectedToken{}, ErrAccountsUnavailable
}

func (s *Service) lockAccountRefresh(accountID int64) func() {
	s.refreshMu.Lock()
	lock := s.refreshLocks[accountID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.refreshLocks[accountID] = lock
	}
	s.refreshMu.Unlock()

	lock.Lock()
	return lock.Unlock
}

func (s *Service) storeTokenResponse(ctx context.Context, tokens TokenResponse, previous *Account) (Account, error) {
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return Account{}, errors.New("oauth token response missing access token")
	}

	refreshToken := tokens.RefreshToken
	subject := tokens.Subject
	displayName := tokens.DisplayName
	if previous != nil {
		subject = previous.Subject
		displayName = valueOrDefault(displayName, previous.DisplayName)
		if refreshToken == "" {
			var err error
			refreshToken, err = secret.DecryptString(s.cfg.Secret, previous.EncryptedRefreshToken)
			if err != nil {
				return Account{}, err
			}
		}
	}
	if strings.TrimSpace(subject) == "" {
		generatedSubject, err := secret.GenerateToken("local_acct")
		if err != nil {
			return Account{}, fmt.Errorf("generate local account subject: %w", err)
		}
		subject = generatedSubject
	}
	if strings.TrimSpace(refreshToken) == "" {
		return Account{}, errors.New("oauth token response missing refresh token")
	}

	encryptedAccessToken, err := secret.EncryptString(s.cfg.Secret, tokens.AccessToken)
	if err != nil {
		return Account{}, fmt.Errorf("encrypt access token: %w", err)
	}
	encryptedRefreshToken, err := secret.EncryptString(s.cfg.Secret, refreshToken)
	if err != nil {
		return Account{}, fmt.Errorf("encrypt refresh token: %w", err)
	}

	var expiresAt *time.Time
	if tokens.ExpiresIn > 0 {
		expiry := time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)
		expiresAt = &expiry
	}
	var lastRefreshAt *time.Time
	if previous != nil {
		now := time.Now()
		lastRefreshAt = &now
	}

	account := Account{
		ID:                    previousID(previous),
		Provider:              s.cfg.Provider,
		Subject:               subject,
		DisplayName:           displayName,
		EncryptedAccessToken:  encryptedAccessToken,
		EncryptedRefreshToken: encryptedRefreshToken,
		AccessTokenExpiresAt:  expiresAt,
		LastRefreshAt:         lastRefreshAt,
		Enabled:               previousEnabled(previous),
		Priority:              previousPriority(previous),
	}
	saved, err := s.repo.SaveAccount(ctx, account)
	if err != nil {
		return Account{}, err
	}
	return saved, nil
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func previousID(previous *Account) int64 {
	if previous == nil {
		return 0
	}
	return previous.ID
}

func previousEnabled(previous *Account) bool {
	if previous == nil {
		return true
	}
	return previous.Enabled
}

func previousPriority(previous *Account) int {
	if previous == nil {
		return 100
	}
	return previous.Priority
}
