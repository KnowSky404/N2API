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
	defaultSessionTTL = 7 * 24 * time.Hour
	sessionTokenName  = "admin_session"
	apiKeyTokenName   = "n2api"
	defaultModel      = "gpt-4.1"
	maxModels         = 100
	maxModelNameLen   = 128
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
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
}

type RequestLog struct {
	ID         int64     `json:"id"`
	RequestID  string    `json:"requestId"`
	ClientKey  string    `json:"clientKey"`
	Provider   string    `json:"provider"`
	Route      string    `json:"route"`
	Method     string    `json:"method"`
	StatusCode int       `json:"statusCode"`
	LatencyMS  int       `json:"latencyMs"`
	Error      string    `json:"error"`
	CreatedAt  time.Time `json:"createdAt"`
}

type ModelSettings struct {
	DefaultModel  string   `json:"defaultModel"`
	AllowedModels []string `json:"allowedModels"`
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

	seen := map[string]struct{}{}
	allowed := make([]string, 0, len(settings.AllowedModels))
	defaultAllowed := false
	for _, raw := range settings.AllowedModels {
		model := strings.TrimSpace(raw)
		if model == "" {
			continue
		}
		if len(model) > maxModelNameLen {
			return ModelSettings{}, ErrInvalidInput
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		allowed = append(allowed, model)
		if len(allowed) > maxModels {
			return ModelSettings{}, ErrInvalidInput
		}
		if model == defaultName {
			defaultAllowed = true
		}
	}
	if len(allowed) == 0 || len(defaultName) > maxModelNameLen || !defaultAllowed {
		return ModelSettings{}, ErrInvalidInput
	}

	return ModelSettings{DefaultModel: defaultName, AllowedModels: allowed}, nil
}
