package provider

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/secret"
)

const (
	defaultStateTTL      = 10 * time.Minute
	defaultRefreshWindow = 2 * time.Minute
	defaultCircuitOpen   = 5 * time.Minute
	defaultManualPause   = 5 * time.Minute
	maxManualPause       = 24 * time.Hour

	refreshFailureCircuitThreshold = 3

	defaultOpenAIOAuthClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	defaultOpenAIOAuthRedirectURL   = "http://localhost:1455/auth/callback"
	defaultOpenAIOAuthAuthURL       = "https://auth.openai.com/oauth/authorize"
	defaultOpenAIOAuthTokenURL      = "https://auth.openai.com/oauth/token"
	defaultOpenAIOAuthScopes        = "openid profile email offline_access"
	defaultOpenAIOAuthRefreshScopes = "openid profile email"
)

const (
	AccountTypeCodexOAuth  = "codex_oauth"
	AccountTypeAPIUpstream = "api_upstream"
)

const (
	CredentialTypeOAuthToken = "oauth_token"
	CredentialTypeAPIKey     = "api_key"
)

const (
	AccountStatusActive      = "active"
	AccountStatusDisabled    = "disabled"
	AccountStatusRateLimited = "rate_limited"
	AccountStatusCircuitOpen = "circuit_open"
	AccountStatusExpired     = "expired"
)

const (
	AccountTestStatusPassed = "passed"
	AccountTestStatusFailed = "failed"
)

const (
	AccountModelSourceManual = "manual"

	maxAccountModels = 100
	maxModelNameLen  = 128
)

var (
	ErrNotConfigured       = errors.New("provider not configured")
	ErrNotConnected        = errors.New("provider not connected")
	ErrInvalidState        = errors.New("invalid oauth state")
	ErrInvalidInput        = errors.New("invalid provider input")
	ErrAccountsDisabled    = errors.New("provider accounts disabled")
	ErrAccountsUnavailable = errors.New("provider accounts unavailable")
	ErrModelUnavailable    = errors.New("model unavailable")
)

type Config struct {
	Provider              string
	ClientID              string
	ClientSecret          string
	RedirectURL           string
	AuthURL               string
	TokenURL              string
	APIBaseURL            string
	CodexResponsesBaseURL string
	ProbeChatGPTAccountID string
	Secret                string
	StateTTL              time.Duration
	RefreshWindow         time.Duration
	CodeVerifier          string
	AllowHTTPAPIUpstreams bool
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

type Fingerprint struct {
	Value     string
	UserAgent string
	IP        string
}

type ConnectOptions struct {
	RedirectAfter   string
	Name            string
	Priority        int
	Enabled         *bool
	TargetAccountID int64
	Fingerprint     Fingerprint
}

type OAuthState struct {
	Provider              string
	StateHash             string
	RedirectAfter         string
	ExpiresAt             time.Time
	ConsumedAt            *time.Time
	CodeVerifier          string `json:"-"`
	EncryptedCodeVerifier string `json:"-"`
	CodeVerifierHash      string
	ClientID              string
	TargetAccountID       int64
	PendingAccountName    string
	PendingPriority       int
	PendingEnabled        *bool
	FingerprintHash       string
	UserAgentHash         string
	IPHash                string
}

type Account struct {
	ID                    int64             `json:"id"`
	Provider              string            `json:"provider"`
	AccountType           string            `json:"accountType"`
	Subject               string            `json:"subject"`
	Name                  string            `json:"name"`
	DisplayName           string            `json:"displayName"`
	BaseURL               string            `json:"baseUrl,omitempty"`
	Credential            AccountCredential `json:"-"`
	EncryptedAccessToken  string            `json:"-"`
	EncryptedRefreshToken string            `json:"-"`
	EncryptedIDToken      string            `json:"-"`
	AccessTokenExpiresAt  *time.Time        `json:"accessTokenExpiresAt"`
	LastRefreshAt         *time.Time        `json:"lastRefreshAt"`
	Enabled               bool              `json:"enabled"`
	Priority              int               `json:"priority"`
	LoadFactor            int               `json:"loadFactor"`
	LastUsedAt            *time.Time        `json:"lastUsedAt"`
	LastError             string            `json:"lastError"`
	LastErrorAt           *time.Time        `json:"lastErrorAt"`
	LastTestAt            *time.Time        `json:"lastTestAt"`
	LastTestStatus        string            `json:"lastTestStatus"`
	LastTestError         string            `json:"lastTestError"`
	Metadata              map[string]string `json:"metadata"`
	Status                string            `json:"status"`
	StatusReason          string            `json:"statusReason"`
	FingerprintHash       string            `json:"fingerprintHash"`
	UserAgentHash         string            `json:"userAgentHash"`
	IPHash                string            `json:"ipHash"`
	FailureCount          int               `json:"failureCount"`
	CircuitOpenUntil      *time.Time        `json:"circuitOpenUntil"`
	RateLimitedUntil      *time.Time        `json:"rateLimitedUntil"`
	LastRefreshError      string            `json:"lastRefreshError"`
	LastRefreshErrorAt    *time.Time        `json:"lastRefreshErrorAt"`
	CreatedAt             time.Time         `json:"createdAt"`
	UpdatedAt             time.Time         `json:"updatedAt"`
}

type AccountTestResult struct {
	ID        int64     `json:"id"`
	AccountID int64     `json:"accountId"`
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	CheckedAt time.Time `json:"checkedAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type AccountCredential struct {
	CredentialType        string            `json:"credentialType"`
	EncryptedAccessToken  string            `json:"-"`
	EncryptedRefreshToken string            `json:"-"`
	EncryptedIDToken      string            `json:"-"`
	AccessTokenExpiresAt  *time.Time        `json:"accessTokenExpiresAt"`
	LastRefreshAt         *time.Time        `json:"lastRefreshAt"`
	LastRefreshError      string            `json:"lastRefreshError"`
	LastRefreshErrorAt    *time.Time        `json:"lastRefreshErrorAt"`
	EncryptedAPIKey       string            `json:"-"`
	BaseURL               string            `json:"baseUrl"`
	Metadata              map[string]string `json:"metadata"`
}

type AccountModel struct {
	ID         int64             `json:"id"`
	AccountID  int64             `json:"accountId"`
	Provider   string            `json:"provider"`
	Model      string            `json:"model"`
	Enabled    bool              `json:"enabled"`
	Source     string            `json:"source"`
	LastSeenAt *time.Time        `json:"lastSeenAt"`
	LastError  string            `json:"lastError"`
	Metadata   map[string]string `json:"metadata"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

type AccountModelInput struct {
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
}

type APIUpstreamInput struct {
	Name       string   `json:"name"`
	BaseURL    string   `json:"baseUrl"`
	APIKey     string   `json:"apiKey"`
	Enabled    *bool    `json:"enabled"`
	Priority   int      `json:"priority"`
	LoadFactor int      `json:"loadFactor"`
	Models     []string `json:"models"`
}

type ExposedModel struct {
	ID      string `json:"id"`
	OwnedBy string `json:"ownedBy"`
}

type AccountUpdate struct {
	Enabled                    *bool
	Priority                   *int
	LoadFactor                 *int
	ClearStatus                bool
	Name                       *string
	APIUpstreamBaseURL         *string
	APIUpstreamAPIKey          *string
	EncryptedAPIUpstreamAPIKey *string
}

type SelectedAccount struct {
	AccountID          int64
	Provider           string
	AccountType        string
	DisplayName        string
	AuthorizationToken string
	BaseURL            string
	ChatGPTAccountID   string
}

type SelectionPreview struct {
	Model             string               `json:"model"`
	SessionID         string               `json:"sessionId"`
	SelectedAccountID int64                `json:"selectedAccountId"`
	Candidates        []SelectionCandidate `json:"candidates"`
}

type SelectionCandidate struct {
	ID                  int64      `json:"id"`
	DisplayName         string     `json:"displayName"`
	AccountType         string     `json:"accountType"`
	Priority            int        `json:"priority"`
	LoadFactor          int        `json:"loadFactor"`
	Status              string     `json:"status"`
	LastUsedAt          *time.Time `json:"lastUsedAt"`
	LastTestAt          *time.Time `json:"lastTestAt"`
	LastTestStatus      string     `json:"lastTestStatus"`
	LastTestError       string     `json:"lastTestError"`
	ScheduleRank        int        `json:"scheduleRank"`
	Selected            bool       `json:"selected"`
	Schedulable         bool       `json:"schedulable"`
	UnschedulableReason string     `json:"unschedulableReason"`
}

type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresIn    int
	Subject      string
	DisplayName  string
	AccountID    string
	Email        string
	PlanType     string
	ClientID     string
}

type idTokenClaims struct {
	Subject    string            `json:"sub"`
	Email      string            `json:"email"`
	OpenAIAuth *openAIAuthClaims `json:"https://api.openai.com/auth"`
}

type openAIAuthClaims struct {
	ChatGPTAccountID string              `json:"chatgpt_account_id"`
	ChatGPTUserID    string              `json:"chatgpt_user_id"`
	ChatGPTPlanType  string              `json:"chatgpt_plan_type"`
	UserID           string              `json:"user_id"`
	POID             string              `json:"poid"`
	Organizations    []organizationClaim `json:"organizations"`
}

type organizationClaim struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Title     string `json:"title"`
	IsDefault bool   `json:"is_default"`
}

type Repository interface {
	ListAccounts(ctx context.Context, provider string) ([]Account, error)
	HasEnabledAccounts(ctx context.Context, provider string) (bool, error)
	FindAccount(ctx context.Context, provider string) (Account, error)
	FindAccountByID(ctx context.Context, provider string, id int64) (Account, error)
	FindAccountByIdentity(ctx context.Context, provider string, identities AccountIdentities) (Account, error)
	SaveAccount(ctx context.Context, account Account) (Account, error)
	UpdateAccount(ctx context.Context, provider string, id int64, update AccountUpdate) (Account, error)
	DeleteAccount(ctx context.Context, provider string, id int64) error
	DeleteAccounts(ctx context.Context, provider string) error
	MarkAccountUsed(ctx context.Context, provider string, id int64, usedAt time.Time) error
	MarkAccountError(ctx context.Context, provider string, id int64, message string, at time.Time) error
	RecordRefreshFailure(ctx context.Context, provider string, id int64, message string, at time.Time, openUntil *time.Time) error
	RecordAccountStatus(ctx context.Context, provider string, id int64, status, reason string, at time.Time, rateLimitedUntil, circuitOpenUntil *time.Time) error
	RecordAccountTestResult(ctx context.Context, provider string, id int64, status, message string, at time.Time) error
	ListAccountTestResults(ctx context.Context, provider string, accountID int64, limit int) ([]AccountTestResult, error)
	ListAccountModels(ctx context.Context, provider string, accountID int64) ([]AccountModel, error)
	ReplaceAccountModels(ctx context.Context, provider string, accountID int64, models []AccountModelInput) ([]AccountModel, error)
	ListExposedModels(ctx context.Context, provider string, allowedModels []string) ([]ExposedModel, error)
	ListEligibleAccountsForModel(ctx context.Context, provider string, model string, excludedAccountIDs []int64, now time.Time) ([]Account, error)
	CreateState(ctx context.Context, state OAuthState) error
	ClaimState(ctx context.Context, provider, stateHash string, now time.Time) (OAuthState, error)
}

type AccountIdentities struct {
	ChatGPTAccountID  string
	ChatGPTUserID     string
	Email             string
	AccessTokenSHA256 string
}

type OAuthClient interface {
	ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error)
	RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error)
}

type accountStatusProber interface {
	ProbeAccountStatus(ctx context.Context, cfg Config, accessToken string) (probeResult, error)
}

type probeResult struct {
	statusCode int
	retryAfter string
	message    string
}

type HTTPClient struct {
	client *http.Client
}

type Service struct {
	repo         Repository
	client       OAuthClient
	prober       accountStatusProber
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
	if strings.TrimSpace(cfg.ClientSecret) != "" {
		values.Set("client_secret", cfg.ClientSecret)
	}
	values.Set("redirect_uri", cfg.RedirectURL)
	if strings.TrimSpace(cfg.CodeVerifier) != "" {
		values.Set("code_verifier", cfg.CodeVerifier)
	}
	return c.postToken(ctx, cfg.TokenURL, values)
}

func (c *HTTPClient) RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	values.Set("client_id", cfg.ClientID)
	values.Set("scope", defaultOpenAIOAuthRefreshScopes)
	if strings.TrimSpace(cfg.ClientSecret) != "" {
		values.Set("client_secret", cfg.ClientSecret)
	}
	return c.postToken(ctx, cfg.TokenURL, values)
}

func NewService(repo Repository, client OAuthClient, cfg Config) *Service {
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	if cfg.ClientID == "" {
		cfg.ClientID = defaultOpenAIOAuthClientID
	}
	if cfg.RedirectURL == "" {
		cfg.RedirectURL = defaultOpenAIOAuthRedirectURL
	}
	if cfg.AuthURL == "" {
		cfg.AuthURL = defaultOpenAIOAuthAuthURL
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = defaultOpenAIOAuthTokenURL
	}
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = "https://api.openai.com"
	}
	if cfg.CodexResponsesBaseURL == "" {
		cfg.CodexResponsesBaseURL = "https://chatgpt.com/backend-api/codex"
	}
	if cfg.StateTTL <= 0 {
		cfg.StateTTL = defaultStateTTL
	}
	if cfg.RefreshWindow <= 0 {
		cfg.RefreshWindow = defaultRefreshWindow
	}
	prober, _ := client.(accountStatusProber)
	return &Service{
		repo:         repo,
		client:       client,
		prober:       prober,
		cfg:          cfg,
		refreshLocks: make(map[int64]*sync.Mutex),
	}
}

func (c *HTTPClient) ProbeAccountStatus(ctx context.Context, cfg Config, accessToken string) (probeResult, error) {
	chatGPTAccountID := strings.TrimSpace(cfg.ProbeChatGPTAccountID)
	if chatGPTAccountID != "" {
		codexBaseURL := strings.TrimRight(strings.TrimSpace(cfg.CodexResponsesBaseURL), "/")
		if codexBaseURL == "" {
			codexBaseURL = "https://chatgpt.com/backend-api/codex"
		}
		return c.probeURL(ctx, codexBaseURL+"/responses", accessToken, func(req *http.Request) {
			req.Header.Set("chatgpt-account-id", chatGPTAccountID)
			req.Header.Set("Accept", "text/event-stream")
			req.Header.Set("OpenAI-Beta", "responses=experimental")
			req.Header.Set("originator", "codex_cli_rs")
			req.Header.Set("User-Agent", "codex_cli_rs/0.125.0 (Ubuntu 22.4.0; x86_64) xterm-256color")
			req.Header.Set("Content-Type", "application/json")
		})
	}

	apiBaseURL := strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = "https://api.openai.com"
	}
	return c.probeURL(ctx, apiBaseURL+"/v1/models", accessToken, nil)
}

func (c *HTTPClient) probeURL(ctx context.Context, targetURL, accessToken string, decorate func(*http.Request)) (probeResult, error) {
	var body io.Reader
	method := http.MethodGet
	if strings.HasSuffix(targetURL, "/responses") {
		method = http.MethodPost
		body = strings.NewReader(`{"model":"gpt-5.4-mini","instructions":"You are Codex, a coding agent.","input":[{"type":"message","role":"user","content":"n2api account status probe"}],"stream":true,"store":false}`)
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return probeResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	if decorate != nil {
		decorate(req)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return probeResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return probeResult{statusCode: resp.StatusCode}, nil
	}
	return probeResult{
		statusCode: resp.StatusCode,
		retryAfter: resp.Header.Get("Retry-After"),
		message:    readErrorMessage(resp.Body, resp.StatusCode),
	}, nil
}

func readErrorMessage(body io.Reader, statusCode int) string {
	if body == nil {
		return http.StatusText(statusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(body, 64<<10))
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
		return http.StatusText(statusCode)
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && strings.TrimSpace(payload.Error.Message) != "" {
		return strings.TrimSpace(payload.Error.Message)
	}
	return strings.TrimSpace(string(raw))
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
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
		Subject      string `json:"subject"`
		DisplayName  string `json:"display_name"`
		AccountID    string `json:"account_id"`
		Email        string `json:"email"`
		PlanType     string `json:"plan_type"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return TokenResponse{}, fmt.Errorf("decode oauth token response: %w", err)
	}
	return TokenResponse{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		IDToken:      payload.IDToken,
		ExpiresIn:    payload.ExpiresIn,
		Subject:      payload.Subject,
		DisplayName:  payload.DisplayName,
		AccountID:    payload.AccountID,
		Email:        payload.Email,
		PlanType:     payload.PlanType,
	}, nil
}

func (s *Service) Configured() bool {
	return strings.TrimSpace(s.cfg.ClientID) != "" &&
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

func (s *Service) StartConnect(ctx context.Context, options ConnectOptions) (ConnectResult, error) {
	if !s.Configured() {
		return ConnectResult{}, ErrNotConfigured
	}
	if strings.TrimSpace(options.RedirectAfter) == "" {
		options.RedirectAfter = "/"
	}

	state, err := secret.GenerateToken("oauth_state")
	if err != nil {
		return ConnectResult{}, fmt.Errorf("generate oauth state: %w", err)
	}
	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return ConnectResult{}, fmt.Errorf("generate code verifier: %w", err)
	}
	codeChallenge := codeChallengeS256(codeVerifier)
	encryptedCodeVerifier, err := secret.EncryptString(s.cfg.Secret, codeVerifier)
	if err != nil {
		return ConnectResult{}, fmt.Errorf("encrypt code verifier: %w", err)
	}

	if err := s.repo.CreateState(ctx, OAuthState{
		Provider:              s.cfg.Provider,
		StateHash:             secret.HashAPIKey(state),
		RedirectAfter:         options.RedirectAfter,
		ExpiresAt:             time.Now().Add(s.cfg.StateTTL),
		CodeVerifier:          codeVerifier,
		EncryptedCodeVerifier: encryptedCodeVerifier,
		CodeVerifierHash:      secret.HashAPIKey(codeVerifier),
		ClientID:              s.cfg.ClientID,
		TargetAccountID:       options.TargetAccountID,
		PendingAccountName:    strings.TrimSpace(options.Name),
		PendingPriority:       options.Priority,
		PendingEnabled:        options.Enabled,
		FingerprintHash:       hashOptional(options.Fingerprint.Value),
		UserAgentHash:         hashOptional(options.Fingerprint.UserAgent),
		IPHash:                hashOptional(options.Fingerprint.IP),
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
	query.Set("scope", defaultOpenAIOAuthScopes)
	query.Set("state", state)
	query.Set("code_challenge", codeChallenge)
	query.Set("code_challenge_method", "S256")
	query.Set("id_token_add_organizations", "true")
	query.Set("codex_cli_simplified_flow", "true")
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
	codeVerifier := strings.TrimSpace(claimed.CodeVerifier)
	if codeVerifier == "" {
		codeVerifier, err = secret.DecryptString(s.cfg.Secret, claimed.EncryptedCodeVerifier)
		if err != nil {
			return Account{}, ErrInvalidState
		}
	}
	if strings.TrimSpace(claimed.CodeVerifierHash) != "" && secret.HashAPIKey(codeVerifier) != claimed.CodeVerifierHash {
		return Account{}, ErrInvalidState
	}

	exchangeCfg := s.cfg
	exchangeCfg.CodeVerifier = codeVerifier
	if strings.TrimSpace(claimed.ClientID) != "" {
		exchangeCfg.ClientID = claimed.ClientID
	}
	tokens, err := s.client.ExchangeCode(ctx, exchangeCfg, code)
	if err != nil {
		return Account{}, err
	}
	account, err := s.storeCallbackTokenResponse(ctx, tokens, claimed)
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
	if update.Enabled == nil && update.Priority == nil && update.LoadFactor == nil && !update.ClearStatus && update.Name == nil && update.APIUpstreamBaseURL == nil && update.APIUpstreamAPIKey == nil {
		return Account{}, ErrInvalidInput
	}
	if update.Priority != nil && *update.Priority < 0 {
		return Account{}, ErrInvalidInput
	}
	if update.LoadFactor != nil && (*update.LoadFactor < 1 || *update.LoadFactor > 100) {
		return Account{}, ErrInvalidInput
	}
	if update.Name != nil {
		name := strings.TrimSpace(*update.Name)
		if name == "" || len(name) > 128 {
			return Account{}, ErrInvalidInput
		}
		update.Name = &name
	}
	if update.APIUpstreamBaseURL != nil || update.APIUpstreamAPIKey != nil {
		account, err := s.repo.FindAccountByID(ctx, s.cfg.Provider, id)
		if err != nil {
			return Account{}, err
		}
		if account.AccountType != AccountTypeAPIUpstream {
			return Account{}, ErrInvalidInput
		}
	}
	if update.APIUpstreamBaseURL != nil {
		baseURL := normalizeOpenAICompatibleBaseURL(*update.APIUpstreamBaseURL)
		parsedBaseURL, err := url.Parse(baseURL)
		if err != nil ||
			!parsedBaseURL.IsAbs() ||
			parsedBaseURL.Host == "" ||
			!s.apiUpstreamSchemeAllowed(parsedBaseURL.Scheme) {
			return Account{}, ErrInvalidInput
		}
		update.APIUpstreamBaseURL = &baseURL
	}
	if update.APIUpstreamAPIKey != nil {
		apiKey := strings.TrimSpace(*update.APIUpstreamAPIKey)
		if apiKey == "" {
			return Account{}, ErrInvalidInput
		}
		encryptedAPIKey, err := secret.EncryptString(s.cfg.Secret, apiKey)
		if err != nil {
			return Account{}, err
		}
		update.EncryptedAPIUpstreamAPIKey = &encryptedAPIKey
		update.APIUpstreamAPIKey = nil
	}
	if update.APIUpstreamBaseURL != nil || update.EncryptedAPIUpstreamAPIKey != nil {
		update.ClearStatus = true
	}
	return s.repo.UpdateAccount(ctx, s.cfg.Provider, id, update)
}

func (s *Service) ResetAccountStatus(ctx context.Context, id int64) (Account, error) {
	return s.UpdateAccount(ctx, id, AccountUpdate{ClearStatus: true})
}

func (s *Service) CreateAPIUpstreamAccount(ctx context.Context, input APIUpstreamInput) (Account, error) {
	name := strings.TrimSpace(input.Name)
	baseURL := normalizeOpenAICompatibleBaseURL(input.BaseURL)
	apiKey := strings.TrimSpace(input.APIKey)
	if name == "" || baseURL == "" || apiKey == "" {
		return Account{}, ErrInvalidInput
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil ||
		!parsedBaseURL.IsAbs() ||
		parsedBaseURL.Host == "" ||
		!s.apiUpstreamSchemeAllowed(parsedBaseURL.Scheme) {
		return Account{}, ErrInvalidInput
	}

	encryptedAPIKey, err := secret.EncryptString(s.cfg.Secret, apiKey)
	if err != nil {
		return Account{}, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	account, err := s.repo.SaveAccount(ctx, Account{
		Provider:    s.cfg.Provider,
		AccountType: AccountTypeAPIUpstream,
		Name:        name,
		DisplayName: name,
		Enabled:     enabled,
		Priority:    input.Priority,
		LoadFactor:  normalizedLoadFactor(input.LoadFactor),
		Status:      AccountStatusActive,
		Credential: AccountCredential{
			CredentialType:  CredentialTypeAPIKey,
			EncryptedAPIKey: encryptedAPIKey,
			BaseURL:         baseURL,
		},
	})
	if err != nil {
		return Account{}, err
	}

	if len(input.Models) > 0 {
		models := make([]AccountModelInput, 0, len(input.Models))
		for _, model := range input.Models {
			models = append(models, AccountModelInput{
				Model:   model,
				Enabled: true,
			})
		}
		if _, err := s.ReplaceAccountModels(ctx, account.ID, models); err != nil {
			if deleteErr := s.repo.DeleteAccount(ctx, s.cfg.Provider, account.ID); deleteErr != nil {
				return Account{}, fmt.Errorf("replace account models: %w; cleanup account: %v", err, deleteErr)
			}
			return Account{}, err
		}
	}
	return account, nil
}

func normalizeOpenAICompatibleBaseURL(value string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(value), "/")
	if strings.HasSuffix(baseURL, "/v1") {
		return strings.TrimSuffix(baseURL, "/v1")
	}
	return baseURL
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

func normalizeAccountModelInputs(inputs []AccountModelInput) ([]AccountModelInput, error) {
	models := make([]AccountModelInput, 0, len(inputs))
	seen := map[string]bool{}
	for _, input := range inputs {
		model := strings.TrimSpace(input.Model)
		if model == "" {
			continue
		}
		if len(model) > maxModelNameLen {
			return nil, ErrInvalidInput
		}
		if seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, AccountModelInput{
			Model:   model,
			Enabled: input.Enabled,
		})
		if len(models) > maxAccountModels {
			return nil, ErrInvalidInput
		}
	}
	return models, nil
}

func (s *Service) ListAccountModels(ctx context.Context, accountID int64) ([]AccountModel, error) {
	if accountID <= 0 {
		return nil, ErrInvalidInput
	}
	return s.repo.ListAccountModels(ctx, s.cfg.Provider, accountID)
}

func (s *Service) ReplaceAccountModels(ctx context.Context, accountID int64, models []AccountModelInput) ([]AccountModel, error) {
	if accountID <= 0 {
		return nil, ErrInvalidInput
	}
	normalized, err := normalizeAccountModelInputs(models)
	if err != nil {
		return nil, err
	}
	return s.repo.ReplaceAccountModels(ctx, s.cfg.Provider, accountID, normalized)
}

func (s *Service) ListExposedModels(ctx context.Context, allowedModels []string) ([]ExposedModel, error) {
	return s.repo.ListExposedModels(ctx, s.cfg.Provider, allowedModels)
}

func (s *Service) RecordAccountFailure(ctx context.Context, accountID int64, statusCode int, retryAfter, message string) error {
	if accountID <= 0 {
		return ErrInvalidInput
	}
	now := time.Now()
	reason := strings.TrimSpace(message)
	if reason == "" {
		reason = http.StatusText(statusCode)
	}

	switch {
	case statusCode == http.StatusTooManyRequests:
		until := retryAfterTime(retryAfter, now, time.Minute)
		return s.repo.RecordAccountStatus(ctx, s.cfg.Provider, accountID, AccountStatusRateLimited, reason, now, &until, nil)
	case statusCode == http.StatusForbidden && isEndpointPermissionError(reason):
		return s.repo.MarkAccountError(ctx, s.cfg.Provider, accountID, reason, now)
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return s.repo.RecordAccountStatus(ctx, s.cfg.Provider, accountID, AccountStatusExpired, reason, now, nil, nil)
	case statusCode >= http.StatusInternalServerError:
		until := now.Add(defaultCircuitOpen)
		return s.repo.RecordAccountStatus(ctx, s.cfg.Provider, accountID, AccountStatusCircuitOpen, reason, now, nil, &until)
	default:
		return s.repo.MarkAccountError(ctx, s.cfg.Provider, accountID, reason, now)
	}
}

func isEndpointPermissionError(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "missing scopes") ||
		strings.Contains(lower, "api.responses.write") ||
		(strings.Contains(lower, "insufficient permissions") && strings.Contains(lower, "scope"))
}

func (s *Service) RefreshAccount(ctx context.Context, id int64) (Account, error) {
	if id <= 0 {
		return Account{}, ErrInvalidInput
	}
	unlock := s.lockAccountRefresh(id)
	defer unlock()

	account, err := s.repo.FindAccountByID(ctx, s.cfg.Provider, id)
	if err != nil {
		return Account{}, err
	}
	if strings.TrimSpace(account.AccountType) == AccountTypeAPIUpstream {
		return Account{}, ErrInvalidInput
	}
	refreshToken, err := secret.DecryptString(s.cfg.Secret, account.EncryptedRefreshToken)
	if err != nil {
		return Account{}, err
	}
	tokens, err := s.client.RefreshToken(ctx, s.cfg, refreshToken)
	if err != nil {
		now := time.Now()
		var openUntil *time.Time
		if account.FailureCount+1 >= refreshFailureCircuitThreshold {
			until := now.Add(defaultCircuitOpen)
			openUntil = &until
		}
		if markErr := s.repo.RecordRefreshFailure(ctx, s.cfg.Provider, account.ID, err.Error(), now, openUntil); markErr != nil {
			return Account{}, markErr
		}
		return Account{}, err
	}
	refreshed, err := s.storeTokenResponse(ctx, tokens, &account)
	if err != nil {
		return Account{}, err
	}
	return s.probeLatestAccountStatus(ctx, refreshed, tokens.AccessToken)
}

func (s *Service) TestAccount(ctx context.Context, id int64) (Account, error) {
	if id <= 0 {
		return Account{}, ErrInvalidInput
	}

	account, err := s.repo.FindAccountByID(ctx, s.cfg.Provider, id)
	if err != nil {
		return Account{}, err
	}

	selected, err := s.selectedAccount(ctx, account)
	if err != nil {
		now := time.Now()
		if markErr := s.repo.RecordAccountTestResult(ctx, s.cfg.Provider, account.ID, AccountTestStatusFailed, err.Error(), now); markErr != nil {
			return Account{}, markErr
		}
		if markErr := s.recordSelectionFailure(ctx, account.ID, err); markErr != nil {
			return Account{}, markErr
		}
		return s.repo.FindAccountByID(ctx, s.cfg.Provider, account.ID)
	}
	if s.prober == nil || strings.TrimSpace(selected.AuthorizationToken) == "" {
		return account, nil
	}

	cfg := s.cfg
	if selected.AccountType == AccountTypeAPIUpstream {
		cfg.APIBaseURL = selected.BaseURL
		cfg.ProbeChatGPTAccountID = ""
	} else {
		cfg.ProbeChatGPTAccountID = strings.TrimSpace(selected.ChatGPTAccountID)
	}

	result, err := s.prober.ProbeAccountStatus(ctx, cfg, selected.AuthorizationToken)
	if err != nil {
		now := time.Now()
		until := now.Add(defaultCircuitOpen)
		if markErr := s.repo.RecordAccountTestResult(ctx, s.cfg.Provider, account.ID, AccountTestStatusFailed, err.Error(), now); markErr != nil {
			return Account{}, markErr
		}
		if markErr := s.repo.RecordAccountStatus(ctx, s.cfg.Provider, account.ID, AccountStatusCircuitOpen, err.Error(), now, nil, &until); markErr != nil {
			return Account{}, markErr
		}
		return s.repo.FindAccountByID(ctx, s.cfg.Provider, account.ID)
	}
	if isAccountFailureStatus(result.statusCode) {
		now := time.Now()
		message := strings.TrimSpace(result.message)
		if message == "" {
			message = http.StatusText(result.statusCode)
		}
		if markErr := s.repo.RecordAccountTestResult(ctx, s.cfg.Provider, account.ID, AccountTestStatusFailed, message, now); markErr != nil {
			return Account{}, markErr
		}
		if err := s.RecordAccountFailure(ctx, account.ID, result.statusCode, result.retryAfter, result.message); err != nil {
			return Account{}, err
		}
		return s.repo.FindAccountByID(ctx, s.cfg.Provider, account.ID)
	}
	if err := s.repo.RecordAccountTestResult(ctx, s.cfg.Provider, account.ID, AccountTestStatusPassed, "", time.Now()); err != nil {
		return Account{}, err
	}
	return s.ResetAccountStatus(ctx, account.ID)
}

func (s *Service) TestAccounts(ctx context.Context) ([]Account, error) {
	accounts, err := s.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	tested := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		updated, err := s.TestAccount(ctx, account.ID)
		if err != nil {
			return nil, err
		}
		tested = append(tested, updated)
	}
	return tested, nil
}

func (s *Service) PauseAccountScheduling(ctx context.Context, id int64, duration time.Duration) (Account, error) {
	if id <= 0 {
		return Account{}, ErrInvalidInput
	}
	if duration <= 0 {
		duration = defaultManualPause
	}
	if duration > maxManualPause {
		return Account{}, ErrInvalidInput
	}
	if _, err := s.repo.FindAccountByID(ctx, s.cfg.Provider, id); err != nil {
		return Account{}, err
	}
	now := time.Now()
	until := now.Add(duration)
	if err := s.repo.RecordAccountStatus(ctx, s.cfg.Provider, id, AccountStatusCircuitOpen, "manually paused", now, nil, &until); err != nil {
		return Account{}, err
	}
	return s.repo.FindAccountByID(ctx, s.cfg.Provider, id)
}

func (s *Service) probeLatestAccountStatus(ctx context.Context, account Account, accessToken string) (Account, error) {
	if s.prober == nil || strings.TrimSpace(accessToken) == "" {
		return account, nil
	}
	cfg := s.cfg
	cfg.ProbeChatGPTAccountID = strings.TrimSpace(account.Metadata["chatgpt_account_id"])
	result, err := s.prober.ProbeAccountStatus(ctx, cfg, accessToken)
	if err != nil {
		return account, nil
	}
	if !isAccountFailureStatus(result.statusCode) {
		return account, nil
	}
	if err := s.RecordAccountFailure(ctx, account.ID, result.statusCode, result.retryAfter, result.message); err != nil {
		return Account{}, err
	}
	return s.repo.FindAccountByID(ctx, s.cfg.Provider, account.ID)
}

func isAccountFailureStatus(statusCode int) bool {
	return statusCode == http.StatusUnauthorized ||
		statusCode == http.StatusForbidden ||
		statusCode == http.StatusTooManyRequests ||
		statusCode >= http.StatusInternalServerError
}

func (s *Service) AccessToken(ctx context.Context) (string, error) {
	selected, err := s.SelectAccountForModel(ctx, "")
	if err != nil {
		return "", err
	}
	return selected.AuthorizationToken, nil
}

func (s *Service) AccessTokenForAccount(ctx context.Context, account Account) (string, error) {
	account = normalizeAccountCredentialFields(account)
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
		latest = normalizeAccountCredentialFields(latest)
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
		if account.ID > 0 {
			now := time.Now()
			var openUntil *time.Time
			if account.FailureCount+1 >= refreshFailureCircuitThreshold {
				until := now.Add(defaultCircuitOpen)
				openUntil = &until
			}
			if markErr := s.repo.RecordRefreshFailure(ctx, s.cfg.Provider, account.ID, err.Error(), now, openUntil); markErr != nil {
				return "", markErr
			}
		}
		return "", err
	}
	refreshed, err := s.storeTokenResponse(ctx, tokens, &account)
	if err != nil {
		return "", err
	}
	return secret.DecryptString(s.cfg.Secret, refreshed.EncryptedAccessToken)
}

func (s *Service) SelectAccountForModel(ctx context.Context, model string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	if !s.Configured() {
		return SelectedAccount{}, ErrNotConfigured
	}

	accounts, hasEnabled, notFoundErr, err := s.selectionCandidates(ctx, model, excludedAccountIDs)
	if err != nil {
		return SelectedAccount{}, err
	}
	return s.selectFromCandidates(ctx, accounts, hasEnabled, notFoundErr)
}

func (s *Service) SelectAccountForModelAndSession(ctx context.Context, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return s.SelectAccountForModel(ctx, model, excludedAccountIDs...)
	}
	if !s.Configured() {
		return SelectedAccount{}, ErrNotConfigured
	}

	accounts, hasEnabled, notFoundErr, err := s.selectionCandidates(ctx, model, excludedAccountIDs)
	if err != nil {
		return SelectedAccount{}, err
	}
	accounts = stickySessionCandidates(accounts, sessionID)
	return s.selectFromCandidates(ctx, accounts, hasEnabled, notFoundErr)
}

func (s *Service) PreviewAccountSelection(ctx context.Context, model, sessionID string, excludedAccountIDs ...int64) (SelectionPreview, error) {
	if !s.Configured() {
		return SelectionPreview{}, ErrNotConfigured
	}
	model = strings.TrimSpace(model)
	sessionID = strings.TrimSpace(sessionID)
	now := time.Now()
	accounts, _, notFoundErr, err := s.selectionCandidates(ctx, model, excludedAccountIDs)
	if err != nil {
		return SelectionPreview{}, err
	}
	if sessionID != "" {
		accounts = stickySessionCandidates(accounts, sessionID)
	}
	if len(accounts) == 0 {
		blocked := s.unschedulableSelectionCandidates(ctx, model, nil, excludedAccountIDs, now)
		if len(blocked) > 0 {
			return SelectionPreview{
				Model:      model,
				SessionID:  sessionID,
				Candidates: blocked,
			}, nil
		}
		return SelectionPreview{}, notFoundErr
	}

	preview := SelectionPreview{
		Model:             model,
		SessionID:         sessionID,
		SelectedAccountID: accounts[0].ID,
		Candidates:        make([]SelectionCandidate, 0, len(accounts)),
	}
	for index, account := range accounts {
		preview.Candidates = append(preview.Candidates, selectionCandidate(account, index+1, index == 0, true, ""))
	}
	preview.Candidates = append(preview.Candidates, s.unschedulableSelectionCandidates(ctx, model, accounts, excludedAccountIDs, now)...)
	return preview, nil
}

func (s *Service) unschedulableSelectionCandidates(ctx context.Context, model string, selected []Account, excludedAccountIDs []int64, now time.Time) []SelectionCandidate {
	accounts, err := s.repo.ListAccounts(ctx, s.cfg.Provider)
	if err != nil {
		return nil
	}
	selectedIDs := make(map[int64]struct{}, len(selected))
	for _, account := range selected {
		selectedIDs[account.ID] = struct{}{}
	}
	excluded := make(map[int64]struct{}, len(excludedAccountIDs))
	for _, id := range excludedAccountIDs {
		if id > 0 {
			excluded[id] = struct{}{}
		}
	}

	candidates := make([]SelectionCandidate, 0, len(accounts))
	for _, account := range accounts {
		if _, ok := selectedIDs[account.ID]; ok {
			continue
		}
		reason := s.selectionUnschedulableReason(ctx, account, model, excluded, now)
		if reason == "" {
			continue
		}
		candidates = append(candidates, selectionCandidate(account, 0, false, false, reason))
	}
	return candidates
}

func (s *Service) selectionUnschedulableReason(ctx context.Context, account Account, model string, excluded map[int64]struct{}, now time.Time) string {
	if _, ok := excluded[account.ID]; ok {
		return "account excluded"
	}
	if !account.Enabled {
		return "account disabled"
	}
	if reason := accountUnschedulableReason(account, now); reason != "" {
		return reason
	}
	if strings.TrimSpace(model) == "" {
		return ""
	}
	models, err := s.repo.ListAccountModels(ctx, s.cfg.Provider, account.ID)
	if err != nil {
		return "model not configured"
	}
	hasModel := false
	for _, item := range models {
		if item.Model != model {
			continue
		}
		hasModel = true
		if item.Enabled {
			return ""
		}
	}
	if hasModel {
		return "model disabled"
	}
	return "model not configured"
}

func selectionCandidate(account Account, scheduleRank int, selected bool, schedulable bool, reason string) SelectionCandidate {
	account = normalizeAccountCredentialFields(account)
	return SelectionCandidate{
		ID:                  account.ID,
		DisplayName:         accountDisplayName(account),
		AccountType:         account.AccountType,
		Priority:            account.Priority,
		LoadFactor:          normalizedLoadFactor(account.LoadFactor),
		Status:              valueOrDefault(account.Status, AccountStatusActive),
		LastUsedAt:          account.LastUsedAt,
		LastTestAt:          account.LastTestAt,
		LastTestStatus:      account.LastTestStatus,
		LastTestError:       account.LastTestError,
		ScheduleRank:        scheduleRank,
		Selected:            selected,
		Schedulable:         schedulable,
		UnschedulableReason: reason,
	}
}

func stickySessionCandidates(accounts []Account, sessionID string) []Account {
	if len(accounts) <= 1 {
		return accounts
	}

	priority := accounts[0].Priority
	loadFactor := normalizedLoadFactor(accounts[0].LoadFactor)
	hasError := accounts[0].LastErrorAt != nil
	groupEnd := 0
	for groupEnd < len(accounts) &&
		accounts[groupEnd].Priority == priority &&
		normalizedLoadFactor(accounts[groupEnd].LoadFactor) == loadFactor &&
		(accounts[groupEnd].LastErrorAt != nil) == hasError {
		groupEnd++
	}
	if groupEnd <= 1 {
		return accounts
	}

	priorityGroup := append([]Account(nil), accounts[:groupEnd]...)
	sort.SliceStable(priorityGroup, func(i, j int) bool {
		return priorityGroup[i].ID < priorityGroup[j].ID
	})
	start := stickyAccountIndex(sessionID, len(priorityGroup))
	rotated := append([]Account(nil), priorityGroup[start:]...)
	rotated = append(rotated, priorityGroup[:start]...)
	rotated = append(rotated, accounts[groupEnd:]...)
	return rotated
}

func stickyAccountIndex(sessionID string, count int) int {
	if count <= 1 {
		return 0
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(sessionID))
	return int(hash.Sum64() % uint64(count))
}

func (s *Service) selectFromCandidates(ctx context.Context, accounts []Account, hasEnabled bool, notFoundErr error) (SelectedAccount, error) {
	for _, account := range accounts {
		selected, err := s.selectedAccount(ctx, account)
		if err != nil {
			if markErr := s.recordSelectionFailure(ctx, account.ID, err); markErr != nil {
				return SelectedAccount{}, fmt.Errorf("mark provider account error: %w", markErr)
			}
			continue
		}
		return selected, nil
	}
	if !hasEnabled {
		return SelectedAccount{}, ErrAccountsDisabled
	}
	return SelectedAccount{}, notFoundErr
}

func (s *Service) recordSelectionFailure(ctx context.Context, accountID int64, err error) error {
	now := time.Now()
	reason := strings.TrimSpace(err.Error())
	if reason == "" {
		reason = "provider account selection failed"
	}
	if errors.Is(err, ErrInvalidInput) {
		until := now.Add(defaultCircuitOpen)
		return s.repo.RecordAccountStatus(ctx, s.cfg.Provider, accountID, AccountStatusCircuitOpen, reason, now, nil, &until)
	}
	return s.repo.MarkAccountError(ctx, s.cfg.Provider, accountID, reason, now)
}

func (s *Service) RecordAccountUsed(ctx context.Context, accountID int64) error {
	if accountID <= 0 {
		return ErrInvalidInput
	}
	return s.repo.MarkAccountUsed(ctx, s.cfg.Provider, accountID, time.Now())
}

func (s *Service) selectedAccount(ctx context.Context, account Account) (SelectedAccount, error) {
	account = normalizeAccountCredentialFields(account)
	accountType := strings.TrimSpace(account.AccountType)
	if accountType == "" {
		accountType = AccountTypeCodexOAuth
	}
	selected := SelectedAccount{
		AccountID:        account.ID,
		Provider:         valueOrDefault(strings.TrimSpace(account.Provider), s.cfg.Provider),
		AccountType:      accountType,
		DisplayName:      accountDisplayName(account),
		ChatGPTAccountID: strings.TrimSpace(account.Metadata["chatgpt_account_id"]),
	}
	switch accountType {
	case AccountTypeCodexOAuth:
		token, err := s.AccessTokenForAccount(ctx, account)
		if err != nil {
			return SelectedAccount{}, err
		}
		selected.AuthorizationToken = token
		selected.BaseURL = strings.TrimRight(strings.TrimSpace(s.cfg.APIBaseURL), "/")
		return selected, nil
	case AccountTypeAPIUpstream:
		token, err := secret.DecryptString(s.cfg.Secret, account.Credential.EncryptedAPIKey)
		if err != nil {
			return SelectedAccount{}, err
		}
		selected.AuthorizationToken = token
		baseURL := strings.TrimRight(strings.TrimSpace(account.Credential.BaseURL), "/")
		parsedBaseURL, err := url.Parse(baseURL)
		if err != nil ||
			!parsedBaseURL.IsAbs() ||
			parsedBaseURL.Host == "" ||
			!s.apiUpstreamSchemeAllowed(parsedBaseURL.Scheme) {
			return SelectedAccount{}, ErrInvalidInput
		}
		selected.BaseURL = baseURL
		return selected, nil
	default:
		return SelectedAccount{}, fmt.Errorf("unsupported account type %q", accountType)
	}
}

func (s *Service) apiUpstreamSchemeAllowed(scheme string) bool {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "https":
		return true
	case "http":
		return s.cfg.AllowHTTPAPIUpstreams
	default:
		return false
	}
}

func normalizeAccountCredentialFields(account Account) Account {
	if strings.TrimSpace(account.AccountType) == "" {
		account.AccountType = AccountTypeCodexOAuth
	}
	if account.Metadata == nil {
		account.Metadata = account.Credential.Metadata
	}
	if account.Metadata == nil {
		account.Metadata = map[string]string{}
	}
	if account.Credential.Metadata == nil {
		account.Credential.Metadata = account.Metadata
	}
	if account.EncryptedAccessToken == "" {
		account.EncryptedAccessToken = account.Credential.EncryptedAccessToken
	}
	if account.EncryptedRefreshToken == "" {
		account.EncryptedRefreshToken = account.Credential.EncryptedRefreshToken
	}
	if account.EncryptedIDToken == "" {
		account.EncryptedIDToken = account.Credential.EncryptedIDToken
	}
	if account.AccessTokenExpiresAt == nil {
		account.AccessTokenExpiresAt = account.Credential.AccessTokenExpiresAt
	}
	if account.LastRefreshAt == nil {
		account.LastRefreshAt = account.Credential.LastRefreshAt
	}
	if account.LastRefreshError == "" {
		account.LastRefreshError = account.Credential.LastRefreshError
	}
	if account.LastRefreshErrorAt == nil {
		account.LastRefreshErrorAt = account.Credential.LastRefreshErrorAt
	}
	if account.Credential.EncryptedAccessToken == "" {
		account.Credential.EncryptedAccessToken = account.EncryptedAccessToken
	}
	if account.Credential.EncryptedRefreshToken == "" {
		account.Credential.EncryptedRefreshToken = account.EncryptedRefreshToken
	}
	if account.Credential.EncryptedIDToken == "" {
		account.Credential.EncryptedIDToken = account.EncryptedIDToken
	}
	if account.Credential.AccessTokenExpiresAt == nil {
		account.Credential.AccessTokenExpiresAt = account.AccessTokenExpiresAt
	}
	if account.Credential.LastRefreshAt == nil {
		account.Credential.LastRefreshAt = account.LastRefreshAt
	}
	if account.Credential.LastRefreshError == "" {
		account.Credential.LastRefreshError = account.LastRefreshError
	}
	if account.Credential.LastRefreshErrorAt == nil {
		account.Credential.LastRefreshErrorAt = account.LastRefreshErrorAt
	}
	if account.BaseURL == "" {
		account.BaseURL = account.Credential.BaseURL
	}
	if account.Credential.BaseURL == "" {
		account.Credential.BaseURL = account.BaseURL
	}
	return account
}

func accountDisplayName(account Account) string {
	for _, value := range []string{account.Name, account.DisplayName, account.Subject, account.Provider} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (s *Service) selectionCandidates(ctx context.Context, model string, excludedAccountIDs []int64) ([]Account, bool, error, error) {
	model = strings.TrimSpace(model)
	if model != "" {
		now := time.Now()
		excluded := normalizedExcludedAccountIDs(excludedAccountIDs)
		accounts, err := s.repo.ListEligibleAccountsForModel(ctx, s.cfg.Provider, model, excluded, now)
		if err != nil {
			return nil, false, ErrModelUnavailable, err
		}
		notFoundErr := ErrAccountsUnavailable
		if len(accounts) == 0 {
			hasEnabled, err := s.repo.HasEnabledAccounts(ctx, s.cfg.Provider)
			if err != nil {
				return nil, false, ErrAccountsUnavailable, err
			}
			if !hasEnabled {
				return accounts, false, ErrAccountsDisabled, nil
			}
			if len(excluded) > 0 {
				availableWithoutExclusions, err := s.repo.ListEligibleAccountsForModel(ctx, s.cfg.Provider, model, nil, now)
				if err != nil {
					return nil, false, ErrModelUnavailable, err
				}
				if len(availableWithoutExclusions) > 0 {
					return accounts, true, ErrAccountsUnavailable, nil
				}
			}
			notFoundErr = ErrModelUnavailable
		}
		return accounts, true, notFoundErr, nil
	}

	accounts, err := s.repo.ListAccounts(ctx, s.cfg.Provider)
	if err != nil {
		return nil, false, ErrAccountsUnavailable, err
	}
	if len(accounts) == 0 {
		return nil, false, ErrAccountsUnavailable, ErrNotConnected
	}

	excluded := make(map[int64]struct{}, len(excludedAccountIDs))
	for _, id := range excludedAccountIDs {
		if id > 0 {
			excluded[id] = struct{}{}
		}
	}

	candidates := make([]Account, 0, len(accounts))
	hasEnabled := false
	now := time.Now()
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		hasEnabled = true
		if _, ok := excluded[account.ID]; ok {
			continue
		}
		if !accountSchedulable(account, now) {
			continue
		}
		candidates = append(candidates, account)
	}
	return candidates, hasEnabled, ErrAccountsUnavailable, nil
}

func normalizedExcludedAccountIDs(ids []int64) []int64 {
	excluded := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			excluded = append(excluded, id)
		}
	}
	return excluded
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
	tokens = enrichTokenResponseFromIDToken(tokens)

	refreshToken := tokens.RefreshToken
	subject := tokens.Subject
	displayName := tokens.DisplayName
	if strings.TrimSpace(subject) == "" {
		subject = tokens.AccountID
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = valueOrDefault(tokens.Email, tokens.AccountID)
	}
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
	var encryptedIDToken string
	if strings.TrimSpace(tokens.IDToken) != "" {
		encryptedIDToken, err = secret.EncryptString(s.cfg.Secret, tokens.IDToken)
		if err != nil {
			return Account{}, fmt.Errorf("encrypt id token: %w", err)
		}
	} else if previous != nil {
		encryptedIDToken = previous.EncryptedIDToken
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
		ID:          previousID(previous),
		Provider:    s.cfg.Provider,
		AccountType: AccountTypeCodexOAuth,
		Subject:     subject,
		DisplayName: displayName,
		Credential: AccountCredential{
			CredentialType:        CredentialTypeOAuthToken,
			EncryptedAccessToken:  encryptedAccessToken,
			EncryptedRefreshToken: encryptedRefreshToken,
			EncryptedIDToken:      encryptedIDToken,
			AccessTokenExpiresAt:  expiresAt,
			LastRefreshAt:         lastRefreshAt,
			Metadata:              tokenMetadata(tokens, previous),
		},
		EncryptedAccessToken:  encryptedAccessToken,
		EncryptedRefreshToken: encryptedRefreshToken,
		EncryptedIDToken:      encryptedIDToken,
		AccessTokenExpiresAt:  expiresAt,
		LastRefreshAt:         lastRefreshAt,
		Enabled:               previousEnabled(previous),
		Priority:              previousPriority(previous),
		Name:                  previousName(previous),
		Status:                AccountStatusActive,
		FingerprintHash:       previousFingerprintHash(previous),
		UserAgentHash:         previousUserAgentHash(previous),
		IPHash:                previousIPHash(previous),
		FailureCount:          0,
		CircuitOpenUntil:      nil,
		LastRefreshError:      "",
		LastRefreshErrorAt:    nil,
		Metadata:              tokenMetadata(tokens, previous),
	}
	saved, err := s.repo.SaveAccount(ctx, account)
	if err != nil {
		return Account{}, err
	}
	return saved, nil
}

func (s *Service) storeCallbackTokenResponse(ctx context.Context, tokens TokenResponse, state OAuthState) (Account, error) {
	tokens = enrichTokenResponseFromIDToken(tokens)
	identities := identitiesFromTokenResponse(tokens)

	var previous *Account
	if state.TargetAccountID > 0 {
		account, err := s.repo.FindAccountByID(ctx, s.cfg.Provider, state.TargetAccountID)
		if err != nil {
			return Account{}, err
		}
		previous = &account
	} else {
		account, err := s.repo.FindAccountByIdentity(ctx, s.cfg.Provider, identities)
		if err == nil {
			previous = &account
		} else if !errors.Is(err, ErrNotConnected) {
			return Account{}, err
		}
	}

	account, err := s.accountFromTokenResponse(tokens, previous)
	if err != nil {
		return Account{}, err
	}
	applyOAuthStateToAccount(&account, state, previous)
	return s.repo.SaveAccount(ctx, account)
}

func AccountSchedulable(account Account, now time.Time) bool {
	if !account.Enabled {
		return false
	}
	switch account.Status {
	case "", AccountStatusActive:
	case AccountStatusRateLimited:
		if account.RateLimitedUntil == nil || account.RateLimitedUntil.After(now) {
			return false
		}
	case AccountStatusCircuitOpen:
		if account.CircuitOpenUntil == nil || account.CircuitOpenUntil.After(now) {
			return false
		}
	case AccountStatusDisabled, AccountStatusExpired:
		return false
	default:
		return false
	}
	if account.RateLimitedUntil != nil && account.RateLimitedUntil.After(now) {
		return false
	}
	if account.CircuitOpenUntil != nil && account.CircuitOpenUntil.After(now) {
		return false
	}
	return true
}

func accountUnschedulableReason(account Account, now time.Time) string {
	if !account.Enabled {
		return "account disabled"
	}
	switch account.Status {
	case "", AccountStatusActive:
	case AccountStatusRateLimited:
		if account.RateLimitedUntil == nil || account.RateLimitedUntil.After(now) {
			return "account rate limited"
		}
	case AccountStatusCircuitOpen:
		if account.CircuitOpenUntil == nil || account.CircuitOpenUntil.After(now) {
			return "account circuit open"
		}
	case AccountStatusDisabled:
		return "account disabled"
	case AccountStatusExpired:
		return "account expired"
	default:
		return "status " + account.Status
	}
	if account.RateLimitedUntil != nil && account.RateLimitedUntil.After(now) {
		return "account rate limited"
	}
	if account.CircuitOpenUntil != nil && account.CircuitOpenUntil.After(now) {
		return "account circuit open"
	}
	return ""
}

func accountSchedulable(account Account, now time.Time) bool {
	return AccountSchedulable(account, now)
}

func retryAfterTime(value string, now time.Time, fallback time.Duration) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return now.Add(fallback)
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if parsed, err := http.ParseTime(value); err == nil && parsed.After(now) {
		return parsed
	}
	return now.Add(fallback)
}

func identitiesFromTokenResponse(tokens TokenResponse) AccountIdentities {
	tokens = enrichTokenResponseFromIDToken(tokens)
	return AccountIdentities{
		ChatGPTAccountID: strings.TrimSpace(tokens.AccountID),
		Email:            strings.ToLower(strings.TrimSpace(tokens.Email)),
	}
}

func applyOAuthStateToAccount(account *Account, state OAuthState, previous *Account) {
	if previous != nil {
		account.Name = previous.Name
		account.Priority = previous.Priority
		account.Enabled = previous.Enabled
		account.FingerprintHash = previous.FingerprintHash
		account.UserAgentHash = previous.UserAgentHash
		account.IPHash = previous.IPHash
	}
	if strings.TrimSpace(state.PendingAccountName) != "" && (previous == nil || state.TargetAccountID > 0) {
		account.Name = strings.TrimSpace(state.PendingAccountName)
	}
	if state.PendingPriority > 0 && (previous == nil || state.TargetAccountID > 0) {
		account.Priority = state.PendingPriority
	}
	if state.PendingEnabled != nil && (previous == nil || state.TargetAccountID > 0) {
		account.Enabled = *state.PendingEnabled
	}
	if previous == nil && account.Priority == 0 {
		account.Priority = 100
	}
	if previous == nil && account.Name == "" {
		account.Name = firstNonEmpty(account.DisplayName, account.Subject)
	}
	if state.FingerprintHash != "" {
		account.FingerprintHash = state.FingerprintHash
	}
	if state.UserAgentHash != "" {
		account.UserAgentHash = state.UserAgentHash
	}
	if state.IPHash != "" {
		account.IPHash = state.IPHash
	}
	account.Status = AccountStatusActive
	account.StatusReason = ""
	account.FailureCount = 0
	account.CircuitOpenUntil = nil
	account.LastRefreshError = ""
	account.LastRefreshErrorAt = nil
}

func (s *Service) accountFromTokenResponse(tokens TokenResponse, previous *Account) (Account, error) {
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return Account{}, errors.New("oauth token response missing access token")
	}
	tokens = enrichTokenResponseFromIDToken(tokens)

	refreshToken := tokens.RefreshToken
	subject := tokens.Subject
	displayName := tokens.DisplayName
	if strings.TrimSpace(subject) == "" {
		subject = firstNonEmpty(tokens.AccountID, tokens.Email)
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = firstNonEmpty(tokens.Email, tokens.AccountID, subject)
	}
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
	var encryptedIDToken string
	if strings.TrimSpace(tokens.IDToken) != "" {
		encryptedIDToken, err = secret.EncryptString(s.cfg.Secret, tokens.IDToken)
		if err != nil {
			return Account{}, fmt.Errorf("encrypt id token: %w", err)
		}
	} else if previous != nil {
		encryptedIDToken = previous.EncryptedIDToken
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

	return Account{
		ID:          previousID(previous),
		Provider:    s.cfg.Provider,
		AccountType: AccountTypeCodexOAuth,
		Subject:     subject,
		Name:        previousName(previous),
		DisplayName: displayName,
		Credential: AccountCredential{
			CredentialType:        CredentialTypeOAuthToken,
			EncryptedAccessToken:  encryptedAccessToken,
			EncryptedRefreshToken: encryptedRefreshToken,
			EncryptedIDToken:      encryptedIDToken,
			AccessTokenExpiresAt:  expiresAt,
			LastRefreshAt:         lastRefreshAt,
			Metadata:              tokenMetadata(tokens, previous),
		},
		EncryptedAccessToken:  encryptedAccessToken,
		EncryptedRefreshToken: encryptedRefreshToken,
		EncryptedIDToken:      encryptedIDToken,
		AccessTokenExpiresAt:  expiresAt,
		LastRefreshAt:         lastRefreshAt,
		Enabled:               previousEnabled(previous),
		Priority:              previousPriority(previous),
		Status:                AccountStatusActive,
		Metadata:              tokenMetadata(tokens, previous),
	}, nil
}

func generateCodeVerifier() (string, error) {
	raw := make([]byte, 64)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func codeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func tokenMetadata(tokens TokenResponse, previous *Account) map[string]string {
	metadata := map[string]string{}
	if previous != nil {
		for key, value := range previous.Metadata {
			if strings.TrimSpace(value) != "" {
				metadata[key] = value
			}
		}
	}
	setMetadata(metadata, "account_id", tokens.AccountID)
	setMetadata(metadata, "chatgpt_account_id", tokens.AccountID)
	setMetadata(metadata, "email", tokens.Email)
	setMetadata(metadata, "plan_type", tokens.PlanType)
	setMetadata(metadata, "client_id", tokens.ClientID)
	setMetadata(metadata, "access_token_sha256", sha256Hex(tokens.AccessToken))
	return metadata
}

func setMetadata(metadata map[string]string, key, value string) {
	if strings.TrimSpace(value) != "" {
		metadata[key] = strings.TrimSpace(value)
	}
}

func enrichTokenResponseFromIDToken(tokens TokenResponse) TokenResponse {
	claims, err := decodeIDTokenClaims(tokens.IDToken)
	if err != nil {
		return tokens
	}
	if strings.TrimSpace(tokens.Subject) == "" {
		tokens.Subject = claims.Subject
	}
	if strings.TrimSpace(tokens.Email) == "" {
		tokens.Email = claims.Email
	}
	if claims.OpenAIAuth != nil {
		if strings.TrimSpace(tokens.AccountID) == "" {
			tokens.AccountID = claims.OpenAIAuth.ChatGPTAccountID
		}
		if strings.TrimSpace(tokens.PlanType) == "" {
			tokens.PlanType = claims.OpenAIAuth.ChatGPTPlanType
		}
	}
	return tokens
}

func decodeIDTokenClaims(idToken string) (idTokenClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return idTokenClaims{}, errors.New("invalid id token format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return idTokenClaims{}, err
		}
	}
	var claims idTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return idTokenClaims{}, err
	}
	return claims, nil
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func normalizedLoadFactor(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func hashOptional(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return secret.HashAPIKey(strings.TrimSpace(value))
}

func sha256Hex(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
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

func previousName(previous *Account) string {
	if previous == nil {
		return ""
	}
	return previous.Name
}

func previousFingerprintHash(previous *Account) string {
	if previous == nil {
		return ""
	}
	return previous.FingerprintHash
}

func previousUserAgentHash(previous *Account) string {
	if previous == nil {
		return ""
	}
	return previous.UserAgentHash
}

func previousIPHash(previous *Account) string {
	if previous == nil {
		return ""
	}
	return previous.IPHash
}
