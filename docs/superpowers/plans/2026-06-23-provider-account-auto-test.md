# Provider Account Auto Test Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a disabled-by-default background loop that periodically runs provider account tests and feeds existing account health and routing diagnostics.

**Architecture:** Keep probing behavior in `provider.Service.TestAccounts`; add a small provider-owned runner that manages timing and overlap avoidance. Parse environment configuration in `internal/config`, wire the runner from `cmd/n2api`, and document the operational controls.

**Tech Stack:** Go, PostgreSQL-backed provider service, standard-library `context`, `time`, `log/slog`, `os/signal`, SvelteKit/Bun verification.

---

## File Structure

- Modify `backend/internal/config/config.go`: add `ProviderAccountAutoTestEnabled` and `ProviderAccountAutoTestInterval` config fields and env parsing.
- Modify `backend/internal/config/config_test.go`: add parsing and validation tests.
- Create `backend/internal/provider/auto_test_runner.go`: implement the disabled-by-default periodic runner.
- Create `backend/internal/provider/auto_test_runner_test.go`: cover disabled, immediate run, cancellation, and overlap behavior.
- Modify `backend/cmd/n2api/main.go`: wire root signal context, HTTP shutdown, and optional runner goroutine.
- Modify `backend/cmd/n2api/main_test.go`: add source-level tests for runner wiring and graceful shutdown primitives if no existing main runtime harness exists.
- Modify `.env.example`: document auto-test env vars.
- Modify `README.md`: document automatic account tests.
- Modify `deploy/README.md`: document deployment env vars and default disabled behavior.
- Modify `backend/internal/gateway/documentation_test.go`: add documentation coverage assertions.

## Task 1: Config Parsing

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`

- [ ] **Step 1: Write failing config tests**

Add tests to `backend/internal/config/config_test.go`:

```go
func TestLoadProviderAccountAutoTestDefaultsDisabled(t *testing.T) {
	env := validEnv()

	cfg, err := Load(env.lookup)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.ProviderAccountAutoTestEnabled {
		t.Fatal("ProviderAccountAutoTestEnabled = true, want false by default")
	}
	if cfg.ProviderAccountAutoTestInterval != 5*time.Minute {
		t.Fatalf("ProviderAccountAutoTestInterval = %v, want 5m", cfg.ProviderAccountAutoTestInterval)
	}
}

func TestLoadProviderAccountAutoTestEnabledWithInterval(t *testing.T) {
	env := validEnv()
	env.values["N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED"] = "true"
	env.values["N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS"] = "120"

	cfg, err := Load(env.lookup)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if !cfg.ProviderAccountAutoTestEnabled {
		t.Fatal("ProviderAccountAutoTestEnabled = false, want true")
	}
	if cfg.ProviderAccountAutoTestInterval != 2*time.Minute {
		t.Fatalf("ProviderAccountAutoTestInterval = %v, want 2m", cfg.ProviderAccountAutoTestInterval)
	}
}

func TestLoadProviderAccountAutoTestRejectsTooSmallEnabledInterval(t *testing.T) {
	env := validEnv()
	env.values["N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED"] = "true"
	env.values["N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS"] = "30"

	_, err := Load(env.lookup)
	if err == nil {
		t.Fatal("Load returned nil error, want interval validation error")
	}
	if !strings.Contains(err.Error(), "N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS must be at least 60 when auto test is enabled") {
		t.Fatalf("error = %v, want auto test interval validation", err)
	}
}

func TestLoadProviderAccountAutoTestRejectsInvalidBoolean(t *testing.T) {
	env := validEnv()
	env.values["N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED"] = "sometimes"

	_, err := Load(env.lookup)
	if err == nil {
		t.Fatal("Load returned nil error, want boolean validation error")
	}
	if !strings.Contains(err.Error(), "N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED must be a boolean") {
		t.Fatalf("error = %v, want boolean validation", err)
	}
}
```

If `validEnv()` does not exist, add this helper near the other config tests:

```go
type testEnv struct {
	values map[string]string
}

func validEnv() testEnv {
	return testEnv{values: map[string]string{
		"DATABASE_URL":              "postgres://n2api:test@localhost:5432/n2api",
		"N2API_ENCRYPTION_SECRET":   "encryption-secret",
		"N2API_ADMIN_PASSWORD":      "admin-password",
	}}
}

func (e testEnv) lookup(key string) string {
	return e.values[key]
}
```

- [ ] **Step 2: Run failing config tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/config -run 'TestLoadProviderAccountAutoTest'
```

Expected: compile failure because config fields are missing.

- [ ] **Step 3: Implement config fields and parsing**

In `backend/internal/config/config.go`, import `time` and add fields to `Config`:

```go
ProviderAccountAutoTestEnabled  bool
ProviderAccountAutoTestInterval time.Duration
```

Add constants:

```go
const (
	defaultProviderAccountAutoTestInterval = 5 * time.Minute
	minProviderAccountAutoTestInterval     = time.Minute
)
```

In `Load`, after parsing `AllowHTTPAPIUpstreams`, add:

```go
autoTestEnabled, err := parseBool(lookup("N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED"), "N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED")
if err != nil {
	return Config{}, err
}
cfg.ProviderAccountAutoTestEnabled = autoTestEnabled

autoTestIntervalSeconds, err := parseNonNegativeInt(
	lookup("N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS"),
	"N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS",
)
if err != nil {
	return Config{}, err
}
if autoTestIntervalSeconds == 0 {
	cfg.ProviderAccountAutoTestInterval = defaultProviderAccountAutoTestInterval
} else {
	cfg.ProviderAccountAutoTestInterval = time.Duration(autoTestIntervalSeconds) * time.Second
}
if cfg.ProviderAccountAutoTestEnabled && cfg.ProviderAccountAutoTestInterval < minProviderAccountAutoTestInterval {
	return Config{}, fmt.Errorf("N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS must be at least 60 when auto test is enabled")
}
```

- [ ] **Step 4: Run config tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/config -run 'TestLoadProviderAccountAutoTest'
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go
git commit -m "feat: add provider account auto test config"
```

## Task 2: Auto-Test Runner

**Files:**
- Create: `backend/internal/provider/auto_test_runner.go`
- Create: `backend/internal/provider/auto_test_runner_test.go`

- [ ] **Step 1: Write failing runner tests**

Create `backend/internal/provider/auto_test_runner_test.go`:

```go
package provider

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

type fakeAutoTestService struct {
	calls   atomic.Int64
	started chan struct{}
	release chan struct{}
	err     error
}

func newFakeAutoTestService() *fakeAutoTestService {
	return &fakeAutoTestService{
		started: make(chan struct{}, 10),
		release: make(chan struct{}),
	}
}

func (s *fakeAutoTestService) TestAccounts(ctx context.Context) ([]Account, error) {
	s.calls.Add(1)
	s.started <- struct{}{}
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if s.err != nil {
		return nil, s.err
	}
	return []Account{{ID: s.calls.Load(), Provider: "openai"}}, nil
}

func TestAutoTestRunnerDisabledDoesNotProbe(t *testing.T) {
	service := newFakeAutoTestService()
	runner := NewAutoTestRunner(service, AutoTestRunnerConfig{
		Enabled:  false,
		Interval: time.Minute,
	}, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	runner.Run(ctx)

	if service.calls.Load() != 0 {
		t.Fatalf("calls = %d, want 0", service.calls.Load())
	}
}

func TestAutoTestRunnerRunsImmediateCycle(t *testing.T) {
	service := newFakeAutoTestService()
	runner := NewAutoTestRunner(service, AutoTestRunnerConfig{
		Enabled:  true,
		Interval: time.Hour,
	}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})

	go func() {
		runner.Run(ctx)
		close(done)
	}()

	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start immediate probe")
	}
	service.release <- struct{}{}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop after cancellation")
	}
	if service.calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", service.calls.Load())
	}
}

func TestAutoTestRunnerSkipsOverlappingTicks(t *testing.T) {
	service := newFakeAutoTestService()
	runner := NewAutoTestRunner(service, AutoTestRunnerConfig{
		Enabled:  true,
		Interval: 10 * time.Millisecond,
	}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})

	go func() {
		runner.Run(ctx)
		close(done)
	}()

	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start immediate probe")
	}
	time.Sleep(50 * time.Millisecond)
	if service.calls.Load() != 1 {
		t.Fatalf("calls while first probe blocked = %d, want 1", service.calls.Load())
	}
	service.release <- struct{}{}
	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("runner did not run a later probe after first released")
	}
	service.release <- struct{}{}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop after cancellation")
	}
}
```

- [ ] **Step 2: Run failing runner tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run TestAutoTestRunner
```

Expected: compile failure because `NewAutoTestRunner` and related types do not exist.

- [ ] **Step 3: Implement the runner**

Create `backend/internal/provider/auto_test_runner.go`:

```go
package provider

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

type autoTestService interface {
	TestAccounts(ctx context.Context) ([]Account, error)
}

type AutoTestRunnerConfig struct {
	Enabled  bool
	Interval time.Duration
}

type AutoTestRunner struct {
	service autoTestService
	cfg     AutoTestRunnerConfig
	logger  *slog.Logger
	running atomic.Bool
}

func NewAutoTestRunner(service autoTestService, cfg AutoTestRunnerConfig, logger *slog.Logger) *AutoTestRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &AutoTestRunner{
		service: service,
		cfg:     cfg,
		logger:  logger,
	}
}

func (r *AutoTestRunner) Run(ctx context.Context) {
	if r == nil || r.service == nil || !r.cfg.Enabled {
		return
	}
	interval := r.cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	r.runCycle(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runCycle(ctx)
		}
	}
}

func (r *AutoTestRunner) runCycle(ctx context.Context) {
	if !r.running.CompareAndSwap(false, true) {
		r.logger.Debug("provider account auto test skipped because previous cycle is still running")
		return
	}
	defer r.running.Store(false)

	started := time.Now()
	accounts, err := r.service.TestAccounts(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		r.logger.Warn("provider account auto test failed", "error", err, "duration", time.Since(started))
		return
	}
	r.logger.Info("provider account auto test completed", "accounts", len(accounts), "duration", time.Since(started))
}
```

- [ ] **Step 4: Run runner tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run TestAutoTestRunner
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
git add backend/internal/provider/auto_test_runner.go backend/internal/provider/auto_test_runner_test.go
git commit -m "feat: add provider account auto test runner"
```

## Task 3: Main Lifecycle Wiring

**Files:**
- Modify: `backend/cmd/n2api/main.go`
- Create or modify: `backend/cmd/n2api/main_test.go`

- [ ] **Step 1: Write failing source-level wiring tests**

Create `backend/cmd/n2api/main_test.go` if it does not exist:

```go
package main

import (
	"os"
	"strings"
	"testing"
)

func TestMainWiresProviderAccountAutoTestRunner(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile main.go returned error: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"signal.NotifyContext",
		"provider.NewAutoTestRunner",
		"ProviderAccountAutoTestEnabled",
		"ProviderAccountAutoTestInterval",
		"go autoTestRunner.Run(ctx)",
		"server.Shutdown",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("main.go missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run failing cmd test**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./cmd/n2api -run TestMainWiresProviderAccountAutoTestRunner
```

Expected: FAIL because main does not yet wire signal context, runner, or shutdown.

- [ ] **Step 3: Implement lifecycle wiring**

In `backend/cmd/n2api/main.go`, update imports:

```go
import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)
```

At the start of `main`, replace `ctx := context.Background()` with:

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

After `providerService := provider.NewService(...)`, add:

```go
autoTestRunner := provider.NewAutoTestRunner(providerService, provider.AutoTestRunnerConfig{
	Enabled:  cfg.ProviderAccountAutoTestEnabled,
	Interval: cfg.ProviderAccountAutoTestInterval,
}, slog.Default())
go autoTestRunner.Run(ctx)
```

Replace the blocking `ListenAndServe` tail with:

```go
serverErrors := make(chan error, 1)
go func() {
	slog.Info("starting n2api", "addr", cfg.Addr())
	serverErrors <- server.ListenAndServe()
}()

select {
case err := <-serverErrors:
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
case <-ctx.Done():
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Run cmd tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./cmd/n2api
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
git add backend/cmd/n2api/main.go backend/cmd/n2api/main_test.go
git commit -m "feat: wire provider account auto tests"
```

## Task 4: Documentation

**Files:**
- Modify: `.env.example`
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Write failing documentation test**

Add to `backend/internal/gateway/documentation_test.go`:

```go
func TestGatewayDocumentationMentionsProviderAccountAutoTests(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md", "../../../.env.example"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED",
			"N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS",
			"disabled by default",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account auto test documentation", path, want)
			}
		}
	}
}
```

- [ ] **Step 2: Run failing documentation test**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run TestGatewayDocumentationMentionsProviderAccountAutoTests
```

Expected: FAIL because docs do not mention the new env vars.

- [ ] **Step 3: Update `.env.example`**

Add:

```dotenv
# Provider account auto tests are disabled by default to avoid surprise upstream traffic.
N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED=false
N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS=300
```

- [ ] **Step 4: Update README and deploy README**

Add this paragraph near the provider account testing docs in `README.md` and `deploy/README.md`:

```markdown
Provider account auto tests are disabled by default. Set `N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED=true` to run `Test all accounts` automatically in the backend, and set `N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS=300` or higher to control the interval. Automatic tests update the same last test status, last test time, last test error, and local account health fields shown in Provider accounts and Routing diagnostics.
```

- [ ] **Step 5: Run documentation test**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run TestGatewayDocumentationMentionsProviderAccountAutoTests
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
git add .env.example README.md deploy/README.md backend/internal/gateway/documentation_test.go
git commit -m "docs: document provider account auto tests"
```

## Task 5: Full Verification

**Files:**
- No new files.

- [ ] **Step 1: Run whitespace check**

Run:

```bash
git diff --check
```

Expected: no output, exit 0.

- [ ] **Step 2: Run backend tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

Expected: all packages pass. If sandbox blocks `httptest` local listeners, rerun with the approved escalation path and record that in the final response.

- [ ] **Step 3: Run frontend checks**

Run:

```bash
cd frontend
bun run check
```

Expected: `svelte-check found 0 errors and 0 warnings`.

- [ ] **Step 4: Run frontend build**

Run:

```bash
cd frontend
bun run build
```

Expected: Vite build and adapter-static output complete with exit 0.

- [ ] **Step 5: Inspect final diff and log**

Run:

```bash
git status --short
git log --oneline -8
```

Expected: worktree clean after commits; log shows atomic commits for config, runner, wiring, and docs.

## Self-Review

- Spec coverage: config, disabled default, runner loop, overlap prevention, main lifecycle, docs, and verification are each covered by tasks.
- Placeholder scan: no unresolved placeholder markers remain.
- Type consistency: `ProviderAccountAutoTestEnabled`, `ProviderAccountAutoTestInterval`, `AutoTestRunnerConfig`, and `NewAutoTestRunner` are used consistently across tasks.
