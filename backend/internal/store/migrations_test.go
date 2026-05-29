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
