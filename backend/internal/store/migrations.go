package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func MigrationSQL(name string) (string, error) {
	data, err := migrationFS.ReadFile("migrations/" + name)
	if err != nil {
		return "", fmt.Errorf("read migration %s: %w", name, err)
	}
	return string(data), nil
}

func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	return runMigrations(ctx, db)
}

func runMigrations(ctx context.Context, db *sql.DB) error {
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrationFS,
		goose.WithTableName("schema_migrations"),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
