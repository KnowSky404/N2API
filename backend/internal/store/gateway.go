package store

import (
	"context"

	"github.com/KnowSky404/N2API/backend/internal/gateway"
	"github.com/jackc/pgx/v5/pgxpool"
)

type GatewayRepository struct {
	pool *pgxpool.Pool
}

func NewGatewayRepository(pool *pgxpool.Pool) *GatewayRepository {
	return &GatewayRepository{pool: pool}
}

func (r *GatewayRepository) CreateRequestLog(ctx context.Context, entry gateway.RequestLog) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO request_logs (request_id, client_key_id, provider, route, method, status_code, latency_ms, error, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, entry.RequestID,
		entry.ClientKeyID,
		entry.Provider,
		entry.Route,
		entry.Method,
		entry.StatusCode,
		entry.Latency.Milliseconds(),
		entry.Error,
		entry.CreatedAt,
	)
	return err
}
