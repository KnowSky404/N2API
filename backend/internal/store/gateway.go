package store

import (
	"context"
	"encoding/json"

	"github.com/KnowSky404/N2API/backend/internal/gateway"
	"github.com/jackc/pgx/v5/pgxpool"
)

type GatewayRepository struct {
	pool *pgxpool.Pool
}

func NewGatewayRepository(pool *pgxpool.Pool) *GatewayRepository {
	return &GatewayRepository{pool: pool}
}

func createRequestLogSQL() string {
	return `
		INSERT INTO request_logs (
			request_id, client_key_id, provider_account_id, provider_account_type,
			provider_account_name, provider, model, session_id, route, method, status_code, latency_ms, error,
			input_tokens, output_tokens, total_tokens, cached_input_tokens, reasoning_tokens, usage_source,
			estimated_cost_microusd, pricing_snapshot, gateway_attempt_count, gateway_fallback_count, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
	`
}

func (r *GatewayRepository) CreateRequestLog(ctx context.Context, entry gateway.RequestLog) error {
	pricingSnapshot, err := json.Marshal(entry.PricingSnapshot)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, createRequestLogSQL(),
		entry.RequestID,
		entry.ClientKeyID,
		entry.ProviderAccountID,
		entry.ProviderAccountType,
		entry.ProviderAccountName,
		entry.Provider,
		entry.Model,
		entry.SessionID,
		entry.Route,
		entry.Method,
		entry.StatusCode,
		entry.Latency.Milliseconds(),
		entry.Error,
		entry.InputTokens,
		entry.OutputTokens,
		entry.TotalTokens,
		entry.CachedInputTokens,
		entry.ReasoningTokens,
		usageSourceOrDefault(entry.UsageSource),
		entry.EstimatedCostMicrousd,
		pricingSnapshot,
		entry.GatewayAttemptCount,
		entry.GatewayFallbackCount,
		entry.CreatedAt,
	)
	return err
}

func usageSourceOrDefault(source string) string {
	if source == "" {
		return "missing"
	}
	return source
}
