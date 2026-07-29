package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode"

	"github.com/KnowSky404/N2API/backend/internal/secret"
	"github.com/KnowSky404/N2API/backend/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	processTestInstanceLockID  = int64(0x4e324150494e53)
	processTestMigrationLockID = int64(0x4e324150494d47)
)

type lockedProcessLog struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (l *lockedProcessLog) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.Write(data)
}

func (l *lockedProcessLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.String()
}

type n2apiTestProcess struct {
	command *exec.Cmd
	logs    *lockedProcessLog
	done    chan struct{}
	waitErr error
}

func TestInstanceLockProcessLifecycle(t *testing.T) {
	databaseURL := os.Getenv("N2API_STORE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("N2API_STORE_TEST_DATABASE_URL is not set")
	}
	if os.Getenv("N2API_STORE_TEST_ALLOW_DESTRUCTIVE") != "1" {
		t.Skip("N2API_STORE_TEST_ALLOW_DESTRUCTIVE is not enabled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, databaseURL := newIsolatedProcessTestPool(t, ctx, databaseURL)

	adminUsername, cleanupAdmin := processTestAdminUsername(t, ctx, pool)
	if cleanupAdmin {
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cleanupCancel()
			if _, err := pool.Exec(cleanupCtx, `DELETE FROM admins WHERE username = $1`, adminUsername); err != nil {
				t.Errorf("clean up process test admin: %v", redactProcessText(err.Error(), databaseURL))
			}
		})
	}

	binaryPath := buildN2APIProcessTestBinary(t)
	adminPassword := "process-test-admin-" + strings.Repeat("p", 24)
	encryptionSecret := "process-test-encryption-" + strings.Repeat("e", 32)

	t.Run("keeps a single-connection business pool available", func(t *testing.T) {
		limitedURL := processTestDatabaseURLWithParam(t, databaseURL, "pool_max_conns", "1")
		port := reserveProcessTestPort(t)
		process := startN2APIProcess(t, binaryPath, limitedURL, adminUsername, adminPassword, encryptionSecret, port)
		waitForProcessListener(t, process, port, limitedURL, adminPassword, encryptionSecret)
		assertProcessReady(t, port)
		sessionToken := createProcessTestAdminSession(t, pool)
		assertAuthenticatedManagementQuery(t, port, sessionToken)
		assertPostgresApplicationConnectionCount(t, pool, store.PostgresApplicationNameAppPool, 1)
		stopN2APIProcess(t, process, limitedURL, adminPassword, encryptionSecret)
	})

	t.Run("keeps a single-connection business pool available with alert delivery", func(t *testing.T) {
		limitedURL := processTestDatabaseURLWithParam(t, databaseURL, "pool_max_conns", "1")
		port := reserveProcessTestPort(t)
		process := startN2APIProcess(t, binaryPath, limitedURL, adminUsername, adminPassword, encryptionSecret, port,
			"N2API_ALERT_DELIVERY_ENABLED=true",
		)
		waitForProcessListener(t, process, port, limitedURL, adminPassword, encryptionSecret)
		assertProcessReady(t, port)
		waitForPostgresApplicationBackend(t, pool, store.PostgresApplicationNameSystemEventListener, 0)
		assertPostgresApplicationConnectionCount(t, pool, store.PostgresApplicationNameAppPool, 1)
		stopN2APIProcess(t, process, limitedURL, adminPassword, encryptionSecret)
	})

	t.Run("reconnects a dedicated alert listener without exhausting the business pool", func(t *testing.T) {
		limitedURL := processTestDatabaseURLWithParam(t, databaseURL, "pool_max_conns", "2")
		port := reserveProcessTestPort(t)
		process := startN2APIProcess(t, binaryPath, limitedURL, adminUsername, adminPassword, encryptionSecret, port,
			"N2API_ALERT_DELIVERY_ENABLED=true",
		)
		waitForProcessListener(t, process, port, limitedURL, adminPassword, encryptionSecret)
		assertProcessReady(t, port)
		listenerPID := waitForPostgresApplicationBackend(t, pool, store.PostgresApplicationNameSystemEventListener, 0)
		sessionToken := createProcessTestAdminSession(t, pool)
		assertAuthenticatedManagementQuery(t, port, sessionToken)
		assertPostgresApplicationConnectionCount(t, pool, store.PostgresApplicationNameAppPool, 2)
		assertPostgresApplicationConnectionCount(t, pool, store.PostgresApplicationNameSystemEventListener, 1)

		for attempt := 1; attempt <= 2; attempt++ {
			var terminated bool
			terminateCtx, terminateCancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := pool.QueryRow(terminateCtx, `SELECT pg_terminate_backend($1)`, listenerPID).Scan(&terminated)
			terminateCancel()
			if err != nil || !terminated {
				t.Fatalf("terminate alert listener backend attempt %d = terminated:%v err:%v", attempt, terminated, err)
			}
			nextPID := waitForPostgresApplicationBackend(t, pool, store.PostgresApplicationNameSystemEventListener, listenerPID)
			if nextPID == listenerPID {
				t.Fatalf("alert listener reconnect attempt %d reused terminated backend PID %d", attempt, listenerPID)
			}
			listenerPID = nextPID
			assertProcessReady(t, port)
			assertAuthenticatedManagementQuery(t, port, sessionToken)
			assertPostgresApplicationConnectionCount(t, pool, store.PostgresApplicationNameAppPool, 2)
			assertPostgresApplicationConnectionCount(t, pool, store.PostgresApplicationNameSystemEventListener, 1)
		}
		stopN2APIProcess(t, process, limitedURL, adminPassword, encryptionSecret)
	})

	t.Run("serializes concurrent safe cold starts before schema changes", func(t *testing.T) {
		coldPool, coldURL := newIsolatedProcessTestPoolWithMigrations(t, ctx, databaseURL, false)
		username := "safe-cold-start-admin"
		firstPort := reserveProcessTestPort(t)
		secondPort := reserveProcessTestPort(t)
		first := startN2APIProcess(t, binaryPath, coldURL, username, adminPassword, encryptionSecret, firstPort)
		second := startN2APIProcess(t, binaryPath, coldURL, username, adminPassword, encryptionSecret, secondPort)

		winner, loser, winnerPort := waitForSingleProcessWinner(t, first, firstPort, second, secondPort, coldURL, adminPassword, encryptionSecret)
		if err := waitForProcessExit(t, loser, 10*time.Second); err == nil || loser.command.ProcessState.ExitCode() == 0 {
			t.Fatalf("losing safe cold-start process exited successfully; logs: %s", processLogs(loser, coldURL, adminPassword, encryptionSecret))
		}
		if !strings.Contains(loser.logs.String(), "instance_already_running") {
			t.Fatalf("losing safe cold-start process did not report instance_already_running; logs: %s", processLogs(loser, coldURL, adminPassword, encryptionSecret))
		}
		assertSingleBootstrapAdmin(t, coldPool, username)
		stopN2APIProcess(t, winner, coldURL, adminPassword, encryptionSecret)
		waitForProcessListenerClosed(t, winnerPort)
	})

	t.Run("serializes migrations for concurrent unsafe cold starts", func(t *testing.T) {
		coldPool, coldURL := newIsolatedProcessTestPoolWithMigrations(t, ctx, databaseURL, false)
		username := "unsafe-cold-start-admin"
		firstPort := reserveProcessTestPort(t)
		secondPort := reserveProcessTestPort(t)
		first := startN2APIProcess(t, binaryPath, coldURL, username, adminPassword, encryptionSecret, firstPort,
			"N2API_ALLOW_UNSAFE_MULTI_INSTANCE=true",
		)
		second := startN2APIProcess(t, binaryPath, coldURL, username, adminPassword, encryptionSecret, secondPort,
			"N2API_ALLOW_UNSAFE_MULTI_INSTANCE=true",
		)
		waitForProcessListener(t, first, firstPort, coldURL, adminPassword, encryptionSecret)
		waitForProcessListener(t, second, secondPort, coldURL, adminPassword, encryptionSecret)
		assertProcessReady(t, firstPort)
		assertProcessReady(t, secondPort)
		assertSingleBootstrapAdmin(t, coldPool, username)
		stopN2APIProcess(t, first, coldURL, adminPassword, encryptionSecret)
		stopN2APIProcess(t, second, coldURL, adminPassword, encryptionSecret)
	})

	t.Run("blocks unsafe migration and bootstrap behind the migration lock", func(t *testing.T) {
		coldPool, coldURL := newIsolatedProcessTestPoolWithMigrations(t, ctx, databaseURL, false)
		lockConn, err := pgx.Connect(ctx, coldURL)
		if err != nil {
			t.Fatalf("connect migration lock holder: %v", err)
		}
		defer func() { _ = lockConn.Close(context.Background()) }()
		if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock($1)`, processTestMigrationLockID); err != nil {
			t.Fatalf("hold migration advisory lock: %v", err)
		}

		username := "blocked-unsafe-cold-start-admin"
		port := reserveProcessTestPort(t)
		process := startN2APIProcess(t, binaryPath, coldURL, username, adminPassword, encryptionSecret, port,
			"N2API_ALLOW_UNSAFE_MULTI_INSTANCE=true",
		)
		waitForMigrationLockWaiter(t, coldPool)
		if processListenerOpen(port) {
			t.Fatal("unsafe process opened its listener while the migration lock was held")
		}
		var adminsTable *string
		if err := coldPool.QueryRow(ctx, `SELECT to_regclass('admins')::text`).Scan(&adminsTable); err != nil {
			t.Fatalf("inspect schema while migration lock is held: %v", err)
		}
		if adminsTable != nil {
			t.Fatalf("unsafe process migrated schema while migration lock was held: admins=%q", *adminsTable)
		}
		var unlocked bool
		if err := lockConn.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, processTestMigrationLockID).Scan(&unlocked); err != nil || !unlocked {
			t.Fatalf("release migration advisory lock = unlocked:%v err:%v", unlocked, err)
		}
		waitForProcessListener(t, process, port, coldURL, adminPassword, encryptionSecret)
		assertProcessReady(t, port)
		assertSingleBootstrapAdmin(t, coldPool, username)
		stopN2APIProcess(t, process, coldURL, adminPassword, encryptionSecret)
	})

	t.Run("rejects a second process and releases on normal shutdown", func(t *testing.T) {
		firstPort := reserveProcessTestPort(t)
		first := startN2APIProcess(t, binaryPath, databaseURL, adminUsername, adminPassword, encryptionSecret, firstPort)
		waitForProcessListener(t, first, firstPort, databaseURL, adminPassword, encryptionSecret)

		secondPort := reserveProcessTestPort(t)
		second := startN2APIProcess(t, binaryPath, databaseURL, adminUsername, adminPassword, encryptionSecret, secondPort)
		secondErr := waitForProcessExit(t, second, 10*time.Second)
		if secondErr == nil || second.command.ProcessState == nil || second.command.ProcessState.ExitCode() == 0 {
			t.Fatalf("second process exit = %v, want non-zero; logs: %s", secondErr, processLogs(second, databaseURL, adminPassword, encryptionSecret))
		}
		if !strings.Contains(second.logs.String(), "instance_already_running") {
			t.Fatalf("second process did not report instance_already_running; logs: %s", processLogs(second, databaseURL, adminPassword, encryptionSecret))
		}
		assertProcessListenerOpen(t, firstPort)

		stopN2APIProcess(t, first, databaseURL, adminPassword, encryptionSecret)
		waitForProcessListenerClosed(t, firstPort)

		replacementPort := reserveProcessTestPort(t)
		replacement := startN2APIProcess(t, binaryPath, databaseURL, adminUsername, adminPassword, encryptionSecret, replacementPort)
		waitForProcessListener(t, replacement, replacementPort, databaseURL, adminPassword, encryptionSecret)
		stopN2APIProcess(t, replacement, databaseURL, adminPassword, encryptionSecret)
		waitForProcessListenerClosed(t, replacementPort)
	})

	t.Run("exits non-zero when the lock backend is terminated", func(t *testing.T) {
		port := reserveProcessTestPort(t)
		process := startN2APIProcess(t, binaryPath, databaseURL, adminUsername, adminPassword, encryptionSecret, port)
		waitForProcessListener(t, process, port, databaseURL, adminPassword, encryptionSecret)

		backendPID := waitForInstanceLockBackend(t, pool)
		var terminated bool
		terminateCtx, terminateCancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := pool.QueryRow(terminateCtx, `SELECT pg_terminate_backend($1)`, backendPID).Scan(&terminated)
		terminateCancel()
		if err != nil || !terminated {
			t.Fatalf("terminate instance lock backend = terminated:%v err:%v", terminated, redactProcessText(fmt.Sprint(err), databaseURL))
		}

		waitErr := waitForProcessExit(t, process, 15*time.Second)
		if waitErr == nil || process.command.ProcessState == nil || process.command.ProcessState.ExitCode() == 0 {
			t.Fatalf("process exit after lock loss = %v, want non-zero; logs: %s", waitErr, processLogs(process, databaseURL, adminPassword, encryptionSecret))
		}
		if !strings.Contains(process.logs.String(), "instance_lock_lost") {
			t.Fatalf("process did not report instance_lock_lost; logs: %s", processLogs(process, databaseURL, adminPassword, encryptionSecret))
		}
		waitForProcessListenerClosed(t, port)
		waitForInstanceLockAvailable(t, pool)
	})

	t.Run("allows two processes with the unsafe override", func(t *testing.T) {
		firstPort := reserveProcessTestPort(t)
		first := startN2APIProcess(t, binaryPath, databaseURL, adminUsername, adminPassword, encryptionSecret, firstPort,
			"N2API_ALLOW_UNSAFE_MULTI_INSTANCE=true",
		)
		waitForProcessListener(t, first, firstPort, databaseURL, adminPassword, encryptionSecret)

		secondPort := reserveProcessTestPort(t)
		second := startN2APIProcess(t, binaryPath, databaseURL, adminUsername, adminPassword, encryptionSecret, secondPort,
			"N2API_ALLOW_UNSAFE_MULTI_INSTANCE=true",
		)
		waitForProcessListener(t, second, secondPort, databaseURL, adminPassword, encryptionSecret)

		for name, process := range map[string]*n2apiTestProcess{"first": first, "second": second} {
			if !strings.Contains(process.logs.String(), "unsafe_multi_instance_enabled") {
				t.Fatalf("%s unsafe process did not report unsafe_multi_instance_enabled; logs: %s", name, processLogs(process, databaseURL, adminPassword, encryptionSecret))
			}
		}
		assertProcessListenerOpen(t, firstPort)
		assertProcessListenerOpen(t, secondPort)

		sessionToken := createProcessTestAdminSession(t, pool)
		assertUnsafeMultiInstanceHealthWarning(t, firstPort, sessionToken)
		assertUnsafeMultiInstanceHealthWarning(t, secondPort, sessionToken)

		stopN2APIProcess(t, first, databaseURL, adminPassword, encryptionSecret, sessionToken)
		waitForProcessListenerClosed(t, firstPort)
		assertProcessListenerOpen(t, secondPort)
		stopN2APIProcess(t, second, databaseURL, adminPassword, encryptionSecret, sessionToken)
		waitForProcessListenerClosed(t, secondPort)
	})

	t.Run("does not start a metrics listener when disabled", func(t *testing.T) {
		port := reserveProcessTestPort(t)
		metricsPort := reserveProcessTestPort(t)
		process := startN2APIProcess(t, binaryPath, databaseURL, adminUsername, adminPassword, encryptionSecret, port,
			"N2API_METRICS_ENABLED=false",
			fmt.Sprintf("N2API_METRICS_PORT=%d", metricsPort),
		)
		waitForProcessListener(t, process, port, databaseURL, adminPassword, encryptionSecret)
		if connection, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", metricsPort), 250*time.Millisecond); err == nil {
			_ = connection.Close()
			t.Fatal("disabled metrics configuration started a listener")
		}
		if strings.Contains(process.logs.String(), "starting n2api metrics") {
			t.Fatalf("disabled metrics configuration entered listener startup; logs: %s", processLogs(process, databaseURL, adminPassword, encryptionSecret))
		}
		stopN2APIProcess(t, process, databaseURL, adminPassword, encryptionSecret)
		waitForProcessListenerClosed(t, port)
	})

	t.Run("exits non-zero when the enabled metrics listener cannot bind", func(t *testing.T) {
		occupied, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("occupy metrics port: %v", err)
		}
		t.Cleanup(func() { _ = occupied.Close() })
		metricsPort := occupied.Addr().(*net.TCPAddr).Port
		port := reserveProcessTestPort(t)
		process := startN2APIProcess(t, binaryPath, databaseURL, adminUsername, adminPassword, encryptionSecret, port,
			"N2API_METRICS_ENABLED=true",
			fmt.Sprintf("N2API_METRICS_PORT=%d", metricsPort),
		)
		waitErr := waitForProcessExit(t, process, 15*time.Second)
		if waitErr == nil || process.command.ProcessState == nil || process.command.ProcessState.ExitCode() == 0 {
			t.Fatalf("process exit after metrics bind failure = %v, want non-zero; logs: %s", waitErr, processLogs(process, databaseURL, adminPassword, encryptionSecret))
		}
		if !strings.Contains(process.logs.String(), "metrics_server_stopped") {
			t.Fatalf("metrics bind failure did not report metrics_server_stopped; logs: %s", processLogs(process, databaseURL, adminPassword, encryptionSecret))
		}
		waitForProcessListenerClosed(t, port)
		waitForInstanceLockAvailable(t, pool)
	})
}

func TestProcessTestDatabaseURLSetsSearchPath(t *testing.T) {
	t.Parallel()

	for name, databaseURL := range map[string]string{
		"URL":     "postgres://n2api:secret@127.0.0.1:5432/n2api_store_test?sslmode=disable",
		"keyword": "host=127.0.0.1 port=5432 dbname=n2api_store_test user=n2api password=secret sslmode=disable",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			const schema = "instance_lock_process_test_123"
			isolatedURL, err := processTestDatabaseURL(databaseURL, schema)
			if err != nil {
				t.Fatalf("processTestDatabaseURL returned error: %v", err)
			}
			config, err := pgxpool.ParseConfig(isolatedURL)
			if err != nil {
				t.Fatalf("parse isolated process test database URL: %v", err)
			}
			if got := config.ConnConfig.RuntimeParams["search_path"]; got != schema {
				t.Fatalf("search_path = %q, want %q", got, schema)
			}
		})
	}
}

func newIsolatedProcessTestPool(t *testing.T, ctx context.Context, databaseURL string) (*pgxpool.Pool, string) {
	return newIsolatedProcessTestPoolWithMigrations(t, ctx, databaseURL, true)
}

func newIsolatedProcessTestPoolWithMigrations(t *testing.T, ctx context.Context, databaseURL string, migrate bool) (*pgxpool.Pool, string) {
	t.Helper()

	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open process test database: %v", redactProcessText(err.Error(), databaseURL))
	}
	t.Cleanup(adminPool.Close)
	requireIsolatedProcessTestDatabase(t, ctx, adminPool)

	schema := fmt.Sprintf("instance_lock_process_test_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create process test schema: %v", redactProcessText(err.Error(), databaseURL))
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop process test schema: %v", redactProcessText(err.Error(), databaseURL))
		}
	})

	isolatedURL, err := processTestDatabaseURL(databaseURL, schema)
	if err != nil {
		t.Fatalf("configure process test schema: %v", err)
	}
	pool, err := pgxpool.New(ctx, isolatedURL)
	if err != nil {
		t.Fatalf("open isolated process test schema: %v", redactProcessText(err.Error(), databaseURL, isolatedURL))
	}
	t.Cleanup(pool.Close)
	if migrate {
		if err := store.RunMigrations(ctx, pool); err != nil {
			t.Fatalf("run isolated process test migrations: %v", redactProcessText(err.Error(), databaseURL, isolatedURL))
		}
	}
	return pool, isolatedURL
}

func processTestDatabaseURLWithParam(t *testing.T, databaseURL, key, value string) string {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatalf("set process test database URL parameter: unsupported URL")
	}
	query := parsed.Query()
	query.Set(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func processTestDatabaseURL(databaseURL, schema string) (string, error) {
	if strings.HasPrefix(databaseURL, "postgres://") || strings.HasPrefix(databaseURL, "postgresql://") {
		parsed, err := url.Parse(databaseURL)
		if err != nil {
			return "", fmt.Errorf("parse process test database URL: %w", err)
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String(), nil
	}
	return strings.TrimSpace(databaseURL) + " search_path=" + schema, nil
}

func requireIsolatedProcessTestDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var databaseName string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatalf("identify process test database: %v", err)
	}
	segments := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(databaseName)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, segment := range segments {
		if segment == "test" || segment == "e2e" || segment == "restore" {
			return
		}
	}
	t.Fatalf("refusing process test against non-test database %q", databaseName)
}

func processTestAdminUsername(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, bool) {
	t.Helper()
	var username string
	err := pool.QueryRow(ctx, `SELECT username FROM admins ORDER BY id ASC LIMIT 1`).Scan(&username)
	if err == nil {
		return username, false
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("find process test admin: %v", err)
	}
	return "n2api-instance-lock-process-test", true
}

func buildN2APIProcessTestBinary(t *testing.T) string {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), "n2api-process-test")
	command := exec.Command("go", "build", "-o", binaryPath, ".")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build n2api process test binary: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return binaryPath
}

func reserveProcessTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve process test port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release process test port: %v", err)
	}
	return port
}

func startN2APIProcess(t *testing.T, binaryPath, databaseURL, adminUsername, adminPassword, encryptionSecret string, port int, overrides ...string) *n2apiTestProcess {
	t.Helper()
	logs := &lockedProcessLog{}
	command := exec.Command(binaryPath)
	command.Dir = newProcessTestWorkingDirectory(t)
	command.Env = overrideProcessTestEnv(n2apiProcessTestEnv(databaseURL, adminUsername, adminPassword, encryptionSecret, port), overrides...)
	command.Stdout = logs
	command.Stderr = logs
	if err := command.Start(); err != nil {
		t.Fatalf("start n2api process: %v", err)
	}
	process := &n2apiTestProcess{command: command, logs: logs, done: make(chan struct{})}
	go func() {
		process.waitErr = command.Wait()
		close(process.done)
	}()
	t.Cleanup(func() {
		select {
		case <-process.done:
			return
		default:
		}
		_ = process.command.Process.Signal(syscall.SIGTERM)
		select {
		case <-process.done:
		case <-time.After(12 * time.Second):
			_ = process.command.Process.Kill()
			<-process.done
		}
	})
	return process
}

func newProcessTestWorkingDirectory(t *testing.T) string {
	t.Helper()
	workingDirectory := t.TempDir()
	buildDirectory := filepath.Join(workingDirectory, "frontend", "build")
	if err := os.MkdirAll(buildDirectory, 0o755); err != nil {
		t.Fatalf("create process test frontend build directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildDirectory, "200.html"), []byte("<!doctype html><title>N2API process test</title>"), 0o644); err != nil {
		t.Fatalf("create process test frontend fixture: %v", err)
	}
	return workingDirectory
}

func overrideProcessTestEnv(environment []string, overrides ...string) []string {
	for _, override := range overrides {
		key, _, ok := strings.Cut(override, "=")
		if !ok || key == "" {
			continue
		}
		filtered := environment[:0]
		for _, item := range environment {
			itemKey, _, _ := strings.Cut(item, "=")
			if itemKey != key {
				filtered = append(filtered, item)
			}
		}
		environment = append(filtered, override)
	}
	return environment
}

func n2apiProcessTestEnv(databaseURL, adminUsername, adminPassword, encryptionSecret string, port int) []string {
	environment := make([]string, 0, len(os.Environ())+12)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key == "DATABASE_URL" || strings.HasPrefix(key, "N2API_") || strings.HasPrefix(key, "OPENAI_") {
			continue
		}
		environment = append(environment, item)
	}
	return append(environment,
		"DATABASE_URL="+databaseURL,
		"N2API_HOST=127.0.0.1",
		fmt.Sprintf("N2API_PORT=%d", port),
		fmt.Sprintf("N2API_PUBLIC_URL=http://127.0.0.1:%d", port),
		"N2API_ADMIN_USERNAME="+adminUsername,
		"N2API_ADMIN_PASSWORD="+adminPassword,
		"N2API_ENCRYPTION_SECRET="+encryptionSecret,
		"N2API_ACCEPT_RISKS=database-plaintext",
		"N2API_ALLOW_UNSAFE_MULTI_INSTANCE=false",
		"N2API_METRICS_ENABLED=false",
		"N2API_ALERT_DELIVERY_ENABLED=false",
		"N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED=false",
	)
}

func waitForProcessListener(t *testing.T, process *n2apiTestProcess, port int, secrets ...string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp4", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		select {
		case <-process.done:
			t.Fatalf("n2api process exited before listening: %v; logs: %s", process.waitErr, processLogs(process, secrets...))
		default:
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("n2api process did not listen on reserved port; logs: %s", processLogs(process, secrets...))
}

func waitForSingleProcessWinner(t *testing.T, first *n2apiTestProcess, firstPort int, second *n2apiTestProcess, secondPort int, secrets ...string) (*n2apiTestProcess, *n2apiTestProcess, int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		firstListening := processListenerOpen(firstPort)
		secondListening := processListenerOpen(secondPort)
		if firstListening && secondListening {
			t.Fatal("both safe cold-start processes opened listeners")
		}
		if firstListening {
			return first, second, firstPort
		}
		if secondListening {
			return second, first, secondPort
		}
		select {
		case <-first.done:
			select {
			case <-second.done:
				t.Fatalf("both safe cold-start processes exited; first: %s; second: %s", processLogs(first, secrets...), processLogs(second, secrets...))
			default:
			}
		default:
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("neither safe cold-start process opened a listener; first: %s; second: %s", processLogs(first, secrets...), processLogs(second, secrets...))
	return nil, nil, 0
}

func processListenerOpen(port int) bool {
	connection, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func assertProcessReady(t *testing.T, port int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lastResult := "no readiness response"
	for ctx.Err() == nil {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/readyz", port), nil)
		if err != nil {
			t.Fatalf("create readiness request: %v", err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			lastResult = err.Error()
		} else {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			_ = response.Body.Close()
			if readErr != nil {
				lastResult = readErr.Error()
			} else {
				var readiness struct {
					Status       string `json:"status"`
					Database     string `json:"database"`
					StaticAssets string `json:"staticAssets"`
				}
				if decodeErr := json.Unmarshal(body, &readiness); decodeErr != nil {
					lastResult = decodeErr.Error()
				} else if response.StatusCode == http.StatusOK && readiness.Status == "ok" && readiness.Database == "ok" && readiness.StaticAssets == "ok" {
					return
				} else {
					lastResult = fmt.Sprintf("status %d and state %+v: %s", response.StatusCode, readiness, strings.TrimSpace(string(body)))
				}
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(25 * time.Millisecond):
		}
	}
	t.Fatalf("readiness on port %d did not become ready: %s", port, lastResult)
}

func assertPostgresApplicationConnectionCount(t *testing.T, pool *pgxpool.Pool, applicationName string, maximum int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pg_stat_activity
		WHERE datname = current_database() AND application_name = $1
	`, applicationName).Scan(&count); err != nil {
		t.Fatalf("count PostgreSQL %s connections: %v", applicationName, err)
	}
	if count > maximum {
		t.Fatalf("PostgreSQL %s connections = %d, want at most %d", applicationName, count, maximum)
	}
}

func waitForPostgresApplicationBackend(t *testing.T, pool *pgxpool.Pool, applicationName string, previousPID int32) int32 {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		var pid int32
		err := pool.QueryRow(ctx, `
			SELECT pid
			FROM pg_stat_activity
			WHERE datname = current_database()
				AND application_name = $1
				AND pid <> $2
			ORDER BY backend_start DESC
			LIMIT 1
		`, applicationName, previousPID).Scan(&pid)
		cancel()
		if err == nil {
			return pid
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("find PostgreSQL %s backend: %v", applicationName, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("PostgreSQL %s backend was not found", applicationName)
	return 0
}

func waitForMigrationLockWaiter(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	lockID := uint64(processTestMigrationLockID)
	classID := int64(lockID >> 32)
	objectID := int64(lockID & 0xffffffff)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_locks AS locks
				JOIN pg_stat_activity AS activity ON activity.pid = locks.pid
				WHERE locks.locktype = 'advisory'
					AND locks.database = (SELECT oid FROM pg_database WHERE datname = current_database())
					AND locks.classid::bigint = $1
					AND locks.objid::bigint = $2
					AND locks.objsubid = 1
					AND NOT locks.granted
					AND activity.application_name = $3
			)
		`, classID, objectID, store.PostgresApplicationNameMigrationLock).Scan(&exists)
		cancel()
		if err != nil {
			t.Fatalf("inspect waiting migration lock backend: %v", err)
		}
		if exists {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("unsafe process did not block on the migration advisory lock")
}

func assertSingleBootstrapAdmin(t *testing.T, pool *pgxpool.Pool, username string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM admins WHERE username = $1`, username).Scan(&count); err != nil {
		t.Fatalf("count bootstrap admins: %v", err)
	}
	if count != 1 {
		t.Fatalf("bootstrap admin count = %d, want 1", count)
	}
}

func assertProcessListenerOpen(t *testing.T, port int) {
	t.Helper()
	connection, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err != nil {
		t.Fatalf("first n2api process stopped listening while second process was rejected: %v", err)
	}
	_ = connection.Close()
}

func waitForProcessListenerClosed(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp4", address, 100*time.Millisecond)
		if err != nil {
			return
		}
		_ = connection.Close()
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("n2api process continued listening after exit")
}

func stopN2APIProcess(t *testing.T, process *n2apiTestProcess, secrets ...string) {
	t.Helper()
	if err := process.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal n2api process: %v", err)
	}
	if err := waitForProcessExit(t, process, 12*time.Second); err != nil {
		t.Fatalf("n2api process did not exit cleanly: %v; logs: %s", err, processLogs(process, secrets...))
	}
}

func waitForProcessExit(t *testing.T, process *n2apiTestProcess, timeout time.Duration) error {
	t.Helper()
	select {
	case <-process.done:
		return process.waitErr
	case <-time.After(timeout):
		_ = process.command.Process.Kill()
		<-process.done
		t.Fatalf("n2api process did not exit within %s", timeout)
		return nil
	}
}

func waitForInstanceLockBackend(t *testing.T, pool *pgxpool.Pool) int32 {
	t.Helper()
	lockID := uint64(processTestInstanceLockID)
	classID := int64(lockID >> 32)
	objectID := int64(lockID & 0xffffffff)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		var pid int32
		err := pool.QueryRow(ctx, `
			SELECT pid
			FROM pg_locks
			WHERE locktype = 'advisory'
				AND database = (SELECT oid FROM pg_database WHERE datname = current_database())
				AND classid::bigint = $1
				AND objid::bigint = $2
				AND objsubid = 1
				AND granted
			LIMIT 1
		`, classID, objectID).Scan(&pid)
		cancel()
		if err == nil {
			return pid
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("find instance lock backend: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("instance lock backend was not found")
	return 0
}

func waitForInstanceLockAvailable(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		connection, err := pool.Acquire(ctx)
		if err != nil {
			cancel()
			t.Fatalf("acquire lock verification connection: %v", err)
		}
		var acquired bool
		err = connection.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, processTestInstanceLockID).Scan(&acquired)
		if err == nil && acquired {
			var unlocked bool
			unlockErr := connection.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, processTestInstanceLockID).Scan(&unlocked)
			connection.Release()
			cancel()
			if unlockErr != nil || !unlocked {
				t.Fatalf("release verification lock = unlocked:%v err:%v", unlocked, unlockErr)
			}
			return
		}
		connection.Release()
		cancel()
		if err != nil {
			t.Fatalf("verify instance lock availability: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("instance lock remained held after process exit")
}

func createProcessTestAdminSession(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var adminID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM admins ORDER BY id ASC LIMIT 1`).Scan(&adminID); err != nil {
		t.Fatalf("find process test admin for authenticated health: %v", err)
	}
	sessionToken, err := secret.GenerateToken("instance_lock_process_test")
	if err != nil {
		t.Fatal("generate process test admin session")
	}
	sessionHash := secret.HashAPIKey(sessionToken)
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `
		INSERT INTO admin_sessions (
			admin_id, token_hash, expires_at, created_at, last_used_at,
			created_ip_summary, user_agent_summary
		)
		VALUES ($1, $2, $3, $4, $4, '', 'instance-lock-process-test')
	`, adminID, sessionHash, now.Add(time.Hour), now); err != nil {
		t.Fatalf("create process test admin session: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM admin_sessions WHERE token_hash = $1`, sessionHash); err != nil {
			t.Errorf("clean up process test admin session: %v", err)
		}
	})
	return sessionToken
}

func assertAuthenticatedManagementQuery(t *testing.T, port int, sessionToken string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/admin/keys?limit=1", port), nil)
	if err != nil {
		t.Fatal("create authenticated management request")
	}
	request.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: sessionToken})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request authenticated management API on port %d: %v", port, err)
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20)); err != nil {
		t.Fatalf("read authenticated management response on port %d: %v", port, err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated management API on port %d returned status %d", port, response.StatusCode)
	}
}

func assertUnsafeMultiInstanceHealthWarning(t *testing.T, port int, sessionToken string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/admin/health", port), nil)
	if err != nil {
		t.Fatal("create authenticated process health request")
	}
	request.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: sessionToken})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request authenticated process health on port %d: %v", port, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatalf("read authenticated process health on port %d: %v", port, err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated process health on port %d returned status %d", port, response.StatusCode)
	}
	var health struct {
		Status   string   `json:"status"`
		Database string   `json:"database"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(body, &health); err != nil {
		t.Fatalf("decode authenticated process health on port %d", port)
	}
	if health.Status != "ok" || health.Database != "ok" {
		t.Fatalf("authenticated process health on port %d = %q/%q, want ok/ok", port, health.Status, health.Database)
	}
	for _, warning := range health.Warnings {
		if warning == "unsafe_multi_instance_enabled" {
			return
		}
	}
	t.Fatalf("authenticated process health on port %d missing unsafe_multi_instance_enabled", port)
}

func processLogs(process *n2apiTestProcess, secrets ...string) string {
	return strings.TrimSpace(redactProcessText(process.logs.String(), secrets...))
}

func redactProcessText(value string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	return value
}
