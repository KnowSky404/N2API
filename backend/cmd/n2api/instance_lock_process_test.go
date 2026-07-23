package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode"

	"github.com/KnowSky404/N2API/backend/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const processTestInstanceLockID = int64(0x4e324150494e53)

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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open process test database: %v", redactProcessText(err.Error(), databaseURL))
	}
	t.Cleanup(pool.Close)
	requireIsolatedProcessTestDatabase(t, ctx, pool)
	if err := store.RunMigrations(ctx, pool); err != nil {
		t.Fatalf("run process test migrations: %v", redactProcessText(err.Error(), databaseURL))
	}

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

func startN2APIProcess(t *testing.T, binaryPath, databaseURL, adminUsername, adminPassword, encryptionSecret string, port int) *n2apiTestProcess {
	t.Helper()
	logs := &lockedProcessLog{}
	command := exec.Command(binaryPath)
	command.Env = n2apiProcessTestEnv(databaseURL, adminUsername, adminPassword, encryptionSecret, port)
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
