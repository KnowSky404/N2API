package config

import (
	"errors"
	"fmt"
	"strconv"
)

type Config struct {
	Host                   string
	Port                   int
	PublicURL              string
	DatabaseURL            string
	AdminUsername          string
	AdminPassword          string
	EncryptionSecret       string
	OpenAIOAuthClientID    string
	OpenAIOAuthSecret      string
	OpenAIOAuthRedirectURL string
	OpenAIOAuthAuthURL     string
	OpenAIOAuthTokenURL    string
}

func Load(lookup func(string) string) (Config, error) {
	cfg := Config{
		Host:          valueOrDefault(lookup("N2API_HOST"), "0.0.0.0"),
		PublicURL:     valueOrDefault(lookup("N2API_PUBLIC_URL"), "http://localhost:3000"),
		AdminUsername: valueOrDefault(lookup("N2API_ADMIN_USERNAME"), "admin"),
		AdminPassword: lookup("N2API_ADMIN_PASSWORD"),

		DatabaseURL:            lookup("DATABASE_URL"),
		EncryptionSecret:       lookup("N2API_ENCRYPTION_SECRET"),
		OpenAIOAuthClientID:    lookup("OPENAI_OAUTH_CLIENT_ID"),
		OpenAIOAuthSecret:      lookup("OPENAI_OAUTH_CLIENT_SECRET"),
		OpenAIOAuthRedirectURL: lookup("OPENAI_OAUTH_REDIRECT_URL"),
		OpenAIOAuthAuthURL:     lookup("OPENAI_OAUTH_AUTH_URL"),
		OpenAIOAuthTokenURL:    lookup("OPENAI_OAUTH_TOKEN_URL"),
	}

	port, err := parsePort(valueOrDefault(lookup("N2API_PORT"), "3000"))
	if err != nil {
		return Config{}, err
	}
	cfg.Port = port

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.EncryptionSecret == "" {
		return Config{}, errors.New("N2API_ENCRYPTION_SECRET is required")
	}
	if cfg.AdminPassword == "" {
		return Config{}, errors.New("N2API_ADMIN_PASSWORD is required")
	}

	return cfg, nil
}

func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("N2API_PORT must be a number: %w", err)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("N2API_PORT must be between 1 and 65535")
	}
	return port, nil
}
