package store

import (
	"testing"
)

const postgresConfigTestURL = "postgres://n2api:secret@primary.example:5432,secondary.example:5433/n2api?sslmode=require&application_name=original&search_path=test_schema&pool_max_conns=1"

func TestPostgresConnectionFactoryPreservesParsedConnectionConfig(t *testing.T) {
	factory, err := NewPostgresConnectionFactory(postgresConfigTestURL)
	if err != nil {
		t.Fatalf("NewPostgresConnectionFactory returned error: %v", err)
	}
	if factory.config.Host != "primary.example" || factory.config.Port != 5432 {
		t.Fatalf("primary endpoint = %s:%d", factory.config.Host, factory.config.Port)
	}
	if factory.config.TLSConfig == nil {
		t.Fatal("TLS configuration was not preserved")
	}
	if len(factory.config.Fallbacks) != 1 || factory.config.Fallbacks[0].Host != "secondary.example" || factory.config.Fallbacks[0].Port != 5433 || factory.config.Fallbacks[0].TLSConfig == nil {
		t.Fatalf("fallbacks = %+v, want secondary TLS endpoint", factory.config.Fallbacks)
	}
	if got := factory.config.RuntimeParams["search_path"]; got != "test_schema" {
		t.Fatalf("search_path = %q, want test_schema", got)
	}

	instanceConfig, err := factory.connectionConfig(PostgresApplicationNameInstanceLock)
	if err != nil {
		t.Fatalf("instance connectionConfig returned error: %v", err)
	}
	if got := instanceConfig.RuntimeParams["application_name"]; got != PostgresApplicationNameInstanceLock {
		t.Fatalf("instance application_name = %q", got)
	}
	if instanceConfig.TLSConfig == factory.config.TLSConfig || instanceConfig.TLSConfig.ServerName != factory.config.TLSConfig.ServerName {
		t.Fatal("connection config copy did not preserve TLS configuration in an independent copy")
	}
	if instanceConfig.Fallbacks[0] == factory.config.Fallbacks[0] {
		t.Fatal("connection config copy reused mutable fallback configuration")
	}
	instanceConfig.RuntimeParams["search_path"] = "mutated"

	listenerConfig, err := factory.connectionConfig(PostgresApplicationNameSystemEventListener)
	if err != nil {
		t.Fatalf("listener connectionConfig returned error: %v", err)
	}
	if got := listenerConfig.RuntimeParams["application_name"]; got != PostgresApplicationNameSystemEventListener {
		t.Fatalf("listener application_name = %q", got)
	}
	if got := listenerConfig.RuntimeParams["search_path"]; got != "test_schema" {
		t.Fatalf("factory runtime params changed through copied config: search_path = %q", got)
	}
}

func TestPostgresMigrationPoolConfigIsDedicatedAndSingleConnection(t *testing.T) {
	config, err := postgresMigrationPoolConfig("postgres://n2api:secret@primary.example/n2api?sslmode=require&pool_max_conns=8&pool_min_conns=4&pool_min_idle_conns=3")
	if err != nil {
		t.Fatalf("postgresMigrationPoolConfig returned error: %v", err)
	}
	if config.MaxConns != 1 || config.MinConns != 0 || config.MinIdleConns != 0 {
		t.Fatalf("migration pool bounds = max:%d min:%d min-idle:%d", config.MaxConns, config.MinConns, config.MinIdleConns)
	}
	if got := config.ConnConfig.RuntimeParams["application_name"]; got != PostgresApplicationNameMigration {
		t.Fatalf("migration application_name = %q", got)
	}
	if config.ConnConfig.TLSConfig == nil {
		t.Fatal("migration pool TLS configuration was not preserved")
	}
}

func TestPostgresPoolConfigSetsApplicationNameWithoutDroppingSettings(t *testing.T) {
	config, err := postgresPoolConfig(postgresConfigTestURL)
	if err != nil {
		t.Fatalf("postgresPoolConfig returned error: %v", err)
	}
	if config.MaxConns != 1 {
		t.Fatalf("MaxConns = %d, want 1", config.MaxConns)
	}
	if got := config.ConnConfig.RuntimeParams["application_name"]; got != PostgresApplicationNameAppPool {
		t.Fatalf("application_name = %q", got)
	}
	if got := config.ConnConfig.RuntimeParams["search_path"]; got != "test_schema" {
		t.Fatalf("search_path = %q, want test_schema", got)
	}
	if config.ConnConfig.TLSConfig == nil || len(config.ConnConfig.Fallbacks) != 1 || config.ConnConfig.Fallbacks[0].TLSConfig == nil {
		t.Fatalf("pool TLS/fallback configuration was not preserved: %+v", config.ConnConfig.Fallbacks)
	}
}

func TestPostgresConnectionFactoryRejectsMissingConfiguration(t *testing.T) {
	var factory *PostgresConnectionFactory
	if _, err := factory.connectionConfig(PostgresApplicationNameInstanceLock); err == nil {
		t.Fatal("nil factory connectionConfig returned nil error")
	}
	factory, err := NewPostgresConnectionFactory(postgresConfigTestURL)
	if err != nil {
		t.Fatalf("NewPostgresConnectionFactory returned error: %v", err)
	}
	if _, err := factory.connectionConfig(""); err == nil {
		t.Fatal("empty application name returned nil error")
	}
}

func TestPostgresControlApplicationNamesAreDistinct(t *testing.T) {
	names := []string{
		PostgresApplicationNameAppPool,
		PostgresApplicationNameInstanceLock,
		PostgresApplicationNameMigration,
		PostgresApplicationNameMigrationLock,
		PostgresApplicationNameSystemEventListener,
	}
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			t.Fatal("empty postgres application name")
		}
		if _, exists := seen[name]; exists {
			t.Fatalf("duplicate postgres application name %q", name)
		}
		seen[name] = struct{}{}
	}
}
