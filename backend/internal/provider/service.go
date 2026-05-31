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

	"github.com/KnowSky404/N2API/backend/internal/secret"
)

const (
	defaultStateTTL      = 10 * time.Minute
	defaultRefreshWindow = 2 * time.Minute
)

var (
	ErrNotConfigured = errors.New("provider not configured")
	ErrNotConnected  = errors.New("provider not connected")
	ErrInvalidState  = errors.New("invalid oauth state")
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
	Provider              string
	Subject               string
	DisplayName           string
	EncryptedAccessToken  string
	EncryptedRefreshToken string
	AccessTokenExpiresAt  *time.Time
	LastRefreshAt         *time.Time
}

type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	Subject      string
	DisplayName  string
}

type Repository interface {
	FindAccount(ctx context.Context, provider string) (Account, error)
	SaveAccount(ctx context.Context, account Account) error
	DeleteAccount(ctx context.Context, provider string) error
	CreateState(ctx context.Context, state OAuthState) error
	FindState(ctx context.Context, provider, stateHash string, now time.Time) (OAuthState, error)
	ConsumeState(ctx context.Context, provider, stateHash string, now time.Time) error
}

type OAuthClient interface {
	ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error)
	RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error)
}

type HTTPClient struct {
	client *http.Client
}

type Service struct {
	repo   Repository
	client OAuthClient
	cfg    Config
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
	return &Service{repo: repo, client: client, cfg: cfg}
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
	validatedAt := time.Now()
	if _, err := s.repo.FindState(ctx, s.cfg.Provider, stateHash, validatedAt); err != nil {
		if errors.Is(err, ErrInvalidState) {
			return Account{}, ErrInvalidState
		}
		return Account{}, err
	}

	tokens, err := s.client.ExchangeCode(ctx, s.cfg, code)
	if err != nil {
		return Account{}, err
	}
	account, err := s.storeTokenResponse(ctx, tokens, nil)
	if err != nil {
		return Account{}, err
	}
	if err := s.repo.ConsumeState(ctx, s.cfg.Provider, stateHash, validatedAt); err != nil {
		if errors.Is(err, ErrInvalidState) {
			return Account{}, ErrInvalidState
		}
		return Account{}, err
	}
	return account, nil
}

func (s *Service) Disconnect(ctx context.Context) error {
	return s.repo.DeleteAccount(ctx, s.cfg.Provider)
}

func (s *Service) AccessToken(ctx context.Context) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}

	account, err := s.repo.FindAccount(ctx, s.cfg.Provider)
	if err != nil {
		if errors.Is(err, ErrNotConnected) {
			return "", ErrNotConnected
		}
		return "", err
	}

	if account.AccessTokenExpiresAt == nil || account.AccessTokenExpiresAt.After(time.Now().Add(s.cfg.RefreshWindow)) {
		return secret.DecryptString(s.cfg.Secret, account.EncryptedAccessToken)
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

func (s *Service) storeTokenResponse(ctx context.Context, tokens TokenResponse, previous *Account) (Account, error) {
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return Account{}, errors.New("oauth token response missing access token")
	}

	refreshToken := tokens.RefreshToken
	subject := tokens.Subject
	displayName := tokens.DisplayName
	if previous != nil {
		subject = valueOrDefault(subject, previous.Subject)
		displayName = valueOrDefault(displayName, previous.DisplayName)
		if refreshToken == "" {
			var err error
			refreshToken, err = secret.DecryptString(s.cfg.Secret, previous.EncryptedRefreshToken)
			if err != nil {
				return Account{}, err
			}
		}
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
		Provider:              s.cfg.Provider,
		Subject:               subject,
		DisplayName:           displayName,
		EncryptedAccessToken:  encryptedAccessToken,
		EncryptedRefreshToken: encryptedRefreshToken,
		AccessTokenExpiresAt:  expiresAt,
		LastRefreshAt:         lastRefreshAt,
	}
	if err := s.repo.SaveAccount(ctx, account); err != nil {
		return Account{}, err
	}
	return account, nil
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
