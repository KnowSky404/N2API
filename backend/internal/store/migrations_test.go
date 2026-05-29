package store

import (
	"strings"
	"testing"
)

func TestInitialMigrationDefinesRequiredTables(t *testing.T) {
	sql, err := MigrationSQL("00001_init.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}

	for _, table := range []string{
		"admins",
		"oauth_accounts",
		"client_api_keys",
		"settings",
		"request_logs",
	} {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("initial migration missing table %s", table)
		}
	}
}

func TestInitialMigrationHasDownSection(t *testing.T) {
	sql, err := MigrationSQL("00001_init.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	if !strings.Contains(sql, "-- +goose Down") {
		t.Fatal("initial migration missing goose down section")
	}
}

func TestAdminSessionsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00002_admin_sessions.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS admin_sessions",
		"admin_id BIGINT NOT NULL REFERENCES admins(id) ON DELETE CASCADE",
		"token_hash TEXT NOT NULL UNIQUE",
		"admin_sessions_token_hash_idx",
		"admin_sessions_expires_at_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
