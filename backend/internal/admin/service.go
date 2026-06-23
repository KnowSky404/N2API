package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/secret"
)

const (
	defaultSessionTTL         = 7 * 24 * time.Hour
	sessionTokenName          = "admin_session"
	apiKeyTokenName           = "n2api"
	defaultModel              = "gpt-4.1"
	maxModels                 = 100
	maxModelNameLen           = 128
	APIKeyModelPolicyAll      = "all"
	APIKeyModelPolicySelected = "selected"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
	ErrInvalidInput = errors.New("invalid input")
)

type Config struct {
	SessionTTL time.Duration
}

type Admin struct {
	ID           int64
	Username     string
	PasswordHash string `json:"-"`
}

type Session struct {
	Token     string
	AdminID   int64
	ExpiresAt time.Time
}

type APIKey struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Prefix        string     `json:"prefix"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastUsedAt    *time.Time `json:"lastUsedAt"`
	RevokedAt     *time.Time `json:"revokedAt"`
	ModelPolicy   string     `json:"modelPolicy"`
	AllowedModels []string   `json:"allowedModels"`
}

type RequestLog struct {
	ID                  int64     `json:"id"`
	RequestID           string    `json:"requestId"`
	ClientKey           string    `json:"clientKey"`
	Provider            string    `json:"provider"`
	ProviderAccountID   int64     `json:"providerAccountId"`
	ProviderAccountType string    `json:"providerAccountType"`
	ProviderAccountName string    `json:"providerAccountName"`
	Model               string    `json:"model"`
	Route               string    `json:"route"`
	Method              string    `json:"method"`
	StatusCode          int       `json:"statusCode"`
	LatencyMS           int       `json:"latencyMs"`
	Error               string    `json:"error"`
	CreatedAt           time.Time `json:"createdAt"`
}

type ModelSettings struct {
	DefaultModel  string   `json:"defaultModel"`
	AllowedModels []string `json:"allowedModels"`
}

type ModelRoutingStatus struct {
	DefaultModel  string              `json:"defaultModel"`
	AllowedModels []string            `json:"allowedModels"`
	Models        []ModelRoutingModel `json:"models"`
	Warnings      []string            `json:"warnings"`
}

type ModelRoutingModel struct {
	Model           string                `json:"model"`
	Allowed         bool                  `json:"allowed"`
	ConfiguredCount int                   `json:"configuredCount"`
	EnabledCount    int                   `json:"enabledCount"`
	Accounts        []ModelRoutingAccount `json:"accounts,omitempty"`
}

type ModelRoutingAccount struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"displayName"`
	AccountType string `json:"accountType"`
	Enabled     bool   `json:"enabled"`
	Priority    int    `json:"priority"`
	Status      string `json:"status"`
}

type CreatedAPIKey struct {
	Key    APIKey
	Secret string
}

type Repository interface {
	FindBootstrapAdmin(ctx context.Context) (Admin, error)
	FindAdminByUsername(ctx context.Context, username string) (Admin, error)
	CreateAdmin(ctx context.Context, username, passwordHash string) (Admin, error)
	UpdateAdminUsername(ctx context.Context, id int64, username string) (Admin, error)
	CreateSession(ctx context.Context, adminID int64, tokenHash string, expiresAt time.Time) error
	FindAdminBySessionHash(ctx context.Context, tokenHash string, now time.Time) (Admin, error)
	RevokeSession(ctx context.Context, tokenHash string) error
	CreateAPIKey(ctx context.Context, name, hash, prefix string) (APIKey, error)
	ListAPIKeys(ctx context.Context) ([]APIKey, error)
	RevokeAPIKey(ctx context.Context, id int64) (APIKey, error)
	FindAPIKeyByHash(ctx context.Context, hash string, now time.Time) (APIKey, error)
	UpdateAPIKeyModelPolicy(ctx context.Context, id int64, policy string, models []string) (APIKey, error)
	ListAPIKeyModels(ctx context.Context, id int64) ([]string, error)
	TouchAPIKey(ctx context.Context, id int64, usedAt time.Time) error
	ListRequestLogs(ctx context.Context, limit int) ([]RequestLog, error)
	GetModelSettings(ctx context.Context) (ModelSettings, error)
	SaveModelSettings(ctx context.Context, settings ModelSettings) (ModelSettings, error)
}

type Service struct {
	repo       Repository
	sessionTTL time.Duration
}

func NewService(repo Repository, cfg Config) *Service {
	sessionTTL := cfg.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = defaultSessionTTL
	}

	return &Service{
		repo:       repo,
		sessionTTL: sessionTTL,
	}
}

func (s *Service) BootstrapAdmin(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		return ErrInvalidInput
	}

	existing, err := s.repo.FindBootstrapAdmin(ctx)
	if err == nil {
		if existing.Username == username {
			return nil
		}
		_, err = s.repo.UpdateAdminUsername(ctx, existing.ID, username)
		return err
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}

	hash, err := secret.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	_, err = s.repo.CreateAdmin(ctx, username, hash)
	return err
}

func (s *Service) Login(ctx context.Context, username, password string) (Session, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		return Session{}, ErrUnauthorized
	}

	admin, err := s.repo.FindAdminByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Session{}, ErrUnauthorized
		}
		return Session{}, err
	}
	if !secret.VerifyPassword(admin.PasswordHash, password) {
		return Session{}, ErrUnauthorized
	}

	token, err := secret.GenerateToken(sessionTokenName)
	if err != nil {
		return Session{}, fmt.Errorf("generate admin session token: %w", err)
	}
	expiresAt := time.Now().Add(s.sessionTTL)
	if err := s.repo.CreateSession(ctx, admin.ID, secret.HashAPIKey(token), expiresAt); err != nil {
		return Session{}, err
	}

	return Session{Token: token, AdminID: admin.ID, ExpiresAt: expiresAt}, nil
}

func (s *Service) ValidateSession(ctx context.Context, token string) (Admin, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Admin{}, ErrUnauthorized
	}

	admin, err := s.repo.FindAdminBySessionHash(ctx, secret.HashAPIKey(token), time.Now())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Admin{}, ErrUnauthorized
		}
		return Admin{}, err
	}
	return admin, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if err := s.repo.RevokeSession(ctx, secret.HashAPIKey(token)); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
}

func (s *Service) CreateAPIKey(ctx context.Context, name string) (CreatedAPIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return CreatedAPIKey{}, ErrInvalidInput
	}

	token, err := secret.GenerateToken(apiKeyTokenName)
	if err != nil {
		return CreatedAPIKey{}, fmt.Errorf("generate api key: %w", err)
	}
	key, err := s.repo.CreateAPIKey(ctx, name, secret.HashAPIKey(token), secret.TokenPrefix(token))
	if err != nil {
		return CreatedAPIKey{}, err
	}

	return CreatedAPIKey{Key: key, Secret: token}, nil
}

func (s *Service) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	return s.repo.ListAPIKeys(ctx)
}

func (s *Service) RevokeAPIKey(ctx context.Context, id int64) (APIKey, error) {
	return s.repo.RevokeAPIKey(ctx, id)
}

func (s *Service) UpdateAPIKeyModelPolicy(ctx context.Context, id int64, policy string, models []string) (APIKey, error) {
	policy = strings.TrimSpace(policy)
	switch policy {
	case APIKeyModelPolicyAll:
		return s.repo.UpdateAPIKeyModelPolicy(ctx, id, policy, nil)
	case APIKeyModelPolicySelected:
		normalized, err := normalizeModelList(models)
		if err != nil {
			return APIKey{}, err
		}
		if len(normalized) == 0 {
			return APIKey{}, ErrInvalidInput
		}
		return s.repo.UpdateAPIKeyModelPolicy(ctx, id, policy, normalized)
	default:
		return APIKey{}, ErrInvalidInput
	}
}

func (s *Service) APIKeyAllowsModel(key APIKey, model string) bool {
	switch key.ModelPolicy {
	case "", APIKeyModelPolicyAll:
		return true
	case APIKeyModelPolicySelected:
		model = strings.TrimSpace(model)
		for _, allowed := range key.AllowedModels {
			if model == allowed {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func (s *Service) AuthenticateAPIKey(ctx context.Context, apiKey string) (APIKey, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return APIKey{}, ErrUnauthorized
	}

	key, err := s.repo.FindAPIKeyByHash(ctx, secret.HashAPIKey(apiKey), time.Now())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return APIKey{}, ErrUnauthorized
		}
		return APIKey{}, err
	}
	if key.RevokedAt != nil {
		return APIKey{}, ErrUnauthorized
	}
	if err := s.repo.TouchAPIKey(ctx, key.ID, time.Now()); err != nil {
		if errors.Is(err, ErrNotFound) {
			return APIKey{}, ErrUnauthorized
		}
		return APIKey{}, err
	}

	return key, nil
}

func (s *Service) ListRequestLogs(ctx context.Context, limit int) ([]RequestLog, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	return s.repo.ListRequestLogs(ctx, limit)
}

func (s *Service) GetModelSettings(ctx context.Context) (ModelSettings, error) {
	settings, err := s.repo.GetModelSettings(ctx)
	if err == nil {
		return normalizeModelSettings(settings)
	}
	if errors.Is(err, ErrNotFound) {
		return defaultModelSettings(), nil
	}
	return ModelSettings{}, err
}

func (s *Service) UpdateModelSettings(ctx context.Context, settings ModelSettings) (ModelSettings, error) {
	normalized, err := normalizeModelSettings(settings)
	if err != nil {
		return ModelSettings{}, err
	}
	return s.repo.SaveModelSettings(ctx, normalized)
}

func (s *Service) DefaultModel(ctx context.Context) (string, error) {
	settings, err := s.GetModelSettings(ctx)
	if err != nil {
		return "", err
	}
	return settings.DefaultModel, nil
}

func (s *Service) IsModelAllowed(ctx context.Context, model string) (bool, error) {
	settings, err := s.GetModelSettings(ctx)
	if err != nil {
		return false, err
	}
	model = strings.TrimSpace(model)
	for _, allowed := range settings.AllowedModels {
		if model == allowed {
			return true, nil
		}
	}
	return false, nil
}

func defaultModelSettings() ModelSettings {
	return ModelSettings{
		DefaultModel: defaultModel,
		AllowedModels: []string{
			defaultModel,
			"gpt-4.1-mini",
			"gpt-4o",
			"gpt-4o-mini",
		},
	}
}

func normalizeModelSettings(settings ModelSettings) (ModelSettings, error) {
	defaultName := strings.TrimSpace(settings.DefaultModel)
	if defaultName == "" {
		return ModelSettings{}, ErrInvalidInput
	}

	allowed, err := normalizeModelList(settings.AllowedModels)
	if err != nil {
		return ModelSettings{}, err
	}
	defaultAllowed := false
	for _, model := range allowed {
		if model == defaultName {
			defaultAllowed = true
		}
	}
	if len(allowed) == 0 || len(defaultName) > maxModelNameLen || !defaultAllowed {
		return ModelSettings{}, ErrInvalidInput
	}

	return ModelSettings{DefaultModel: defaultName, AllowedModels: allowed}, nil
}

func normalizeModelList(models []string) ([]string, error) {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(models))
	for _, raw := range models {
		model := strings.TrimSpace(raw)
		if model == "" {
			continue
		}
		if len(model) > maxModelNameLen {
			return nil, ErrInvalidInput
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		normalized = append(normalized, model)
		if len(normalized) > maxModels {
			return nil, ErrInvalidInput
		}
	}
	return normalized, nil
}
