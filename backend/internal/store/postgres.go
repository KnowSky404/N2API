package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	PostgresApplicationNameAppPool             = "n2api-app-pool"
	PostgresApplicationNameInstanceLock        = "n2api-instance-lock"
	PostgresApplicationNameMigration           = "n2api-migration"
	PostgresApplicationNameMigrationLock       = "n2api-migration-lock"
	PostgresApplicationNameSystemEventListener = "n2api-system-event-listener"
)

// PostgresConnectFunc returns a new connection whose ownership transfers to the
// caller. It must not return a shared connection.
type PostgresConnectFunc func(context.Context) (*pgx.Conn, error)

// PostgresConnectionFactory derives independent connections from one parsed
// PostgreSQL configuration. It is safe for concurrent use.
type PostgresConnectionFactory struct {
	config *pgx.ConnConfig
}

func NewPostgresConnectionFactory(databaseURL string) (*PostgresConnectionFactory, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse postgres configuration: %w", err)
	}
	return &PostgresConnectionFactory{config: poolConfig.ConnConfig.Copy()}, nil
}

func (f *PostgresConnectionFactory) Connect(ctx context.Context, applicationName string) (*pgx.Conn, error) {
	config, err := f.connectionConfig(applicationName)
	if err != nil {
		return nil, err
	}
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect postgres control connection: %w", err)
	}
	return conn, nil
}

func (f *PostgresConnectionFactory) Connector(applicationName string) PostgresConnectFunc {
	return func(ctx context.Context) (*pgx.Conn, error) {
		return f.Connect(ctx, applicationName)
	}
}

func (f *PostgresConnectionFactory) connectionConfig(applicationName string) (*pgx.ConnConfig, error) {
	if f == nil || f.config == nil {
		return nil, errors.New("postgres connection factory is not configured")
	}
	if applicationName == "" {
		return nil, errors.New("postgres application name is required")
	}
	config := f.config.Copy()
	if config.RuntimeParams == nil {
		config.RuntimeParams = make(map[string]string)
	}
	config.RuntimeParams["application_name"] = applicationName
	return config, nil
}

func OpenPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := postgresPoolConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	return openPostgresPool(ctx, config)
}

// OpenMigrationPool opens a temporary single-connection pool for Goose. It is
// separate from both the application pool and the migration-lock connection.
func OpenMigrationPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := postgresMigrationPoolConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	return openPostgresPool(ctx, config)
}

func openPostgresPool(ctx context.Context, config *pgxpool.Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

func postgresPoolConfig(databaseURL string) (*pgxpool.Config, error) {
	return postgresPoolConfigWithApplicationName(databaseURL, PostgresApplicationNameAppPool)
}

func postgresMigrationPoolConfig(databaseURL string) (*pgxpool.Config, error) {
	config, err := postgresPoolConfigWithApplicationName(databaseURL, PostgresApplicationNameMigration)
	if err != nil {
		return nil, err
	}
	config.MaxConns = 1
	config.MinConns = 0
	config.MinIdleConns = 0
	return config, nil
}

func postgresPoolConfigWithApplicationName(databaseURL, applicationName string) (*pgxpool.Config, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse postgres pool configuration: %w", err)
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["application_name"] = applicationName
	return config, nil
}
