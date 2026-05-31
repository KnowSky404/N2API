package provider

import (
	"context"
	"errors"
	"fmt"
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
	ConsumeState(ctx context.Context, provider, stateHash string, now time.Time) (OAuthState, error)
}

type OAuthClient interface {
	ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error)
	RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error)
}

type Service struct {
	repo   Repository
	client OAuthClient
	cfg    Config
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

	if _, err := s.repo.ConsumeState(ctx, s.cfg.Provider, secret.HashAPIKey(state), time.Now()); err != nil {
		if errors.Is(err, ErrInvalidState) {
			return Account{}, ErrInvalidState
		}
		return Account{}, err
	}

	tokens, err := s.client.ExchangeCode(ctx, s.cfg, code)
	if err != nil {
		return Account{}, err
	}
	return s.storeTokenResponse(ctx, tokens, nil)
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
