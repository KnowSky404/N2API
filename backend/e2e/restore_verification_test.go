package e2e_test

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/store"
)

func TestCreateRestoreBackupFixture(t *testing.T) {
	if os.Getenv("N2API_E2E_CREATE_RESTORE_FIXTURE") != "1" {
		t.Skip("set N2API_E2E_CREATE_RESTORE_FIXTURE=1 only for an isolated fixture database")
	}
	env := loadE2EEnvironment(t)
	client := newE2EHTTPClient(t)
	mustJSON(t, client, http.MethodPost, env.baseURL, "/api/admin/login", "restore_fixture_login", nil, map[string]string{
		"username": env.adminUsername,
		"password": env.adminPassword,
	}, nil, http.StatusOK)

	var response struct {
		Key struct {
			ID int64 `json:"id"`
		} `json:"key"`
		Secret string `json:"secret"`
	}
	mustJSON(t, client, http.MethodPost, env.baseURL, "/api/admin/keys", "restore_fixture_key", nil, map[string]string{
		"name": "restore-verification-fixture",
	}, &response, http.StatusCreated)
	if response.Key.ID <= 0 || response.Secret == "" {
		t.Fatal("stage=restore_fixture field=credentials")
	}
}

func TestDowngradeRestoreBackupFixture(t *testing.T) {
	if os.Getenv("N2API_E2E_DOWNGRADE_RESTORE_FIXTURE") != "1" {
		t.Skip("set N2API_E2E_DOWNGRADE_RESTORE_FIXTURE=1 only for an isolated fixture database")
	}
	databaseURL := strings.TrimSpace(os.Getenv("N2API_E2E_DATABASE_URL"))
	targetVersion, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("N2API_E2E_RESTORE_TARGET_SCHEMA")), 10, 64)
	if databaseURL == "" || err != nil || targetVersion < 0 {
		t.Fatal("stage=restore_fixture_downgrade field=config")
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	pool, err := store.OpenMigrationPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("stage=restore_fixture_downgrade action=open_database error=%v", err)
	}
	defer pool.Close()
	if err := store.RunMigrationsDownTo(ctx, pool, targetVersion); err != nil {
		t.Fatalf("stage=restore_fixture_downgrade action=migrate error=%v", err)
	}
}

func TestRestoredAPIKeySecretDecrypts(t *testing.T) {
	rawID := strings.TrimSpace(os.Getenv("N2API_E2E_RESTORED_API_KEY_ID"))
	if rawID == "" {
		t.Skip("restored backup has no reusable API key secret")
	}
	keyID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || keyID <= 0 {
		t.Fatal("stage=restored_secret field=key_id")
	}

	env := loadE2EEnvironment(t)
	client := newE2EHTTPClient(t)
	mustJSON(t, client, http.MethodPost, env.baseURL, "/api/admin/login", "restored_secret_login", nil, map[string]string{
		"username": env.adminUsername,
		"password": env.adminPassword,
	}, nil, http.StatusOK)

	var response struct {
		Secret string `json:"secret"`
	}
	mustJSON(t, client, http.MethodPost, env.baseURL, "/api/admin/keys/"+strconv.FormatInt(keyID, 10)+"/reveal-secret", "restored_secret_read", nil, map[string]string{
		"currentPassword": env.adminPassword,
	}, &response, http.StatusOK)
	if response.Secret == "" {
		t.Fatal("stage=restored_secret field=secret")
	}
}
