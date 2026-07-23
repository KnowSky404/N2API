package store

import (
	"context"
	"encoding/json"

	"github.com/KnowSky404/N2API/backend/internal/gateway"
	"github.com/KnowSky404/N2API/backend/internal/provider"
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
			request_id, upstream_request_id, client_key_id, provider_account_id, provider_account_type,
			provider_account_name, routing_pool_id, routing_pool_name, routing_pool_fallback_depth, routing_pool_fallback_chain, routing_pool_error,
			provider, model, session_id, route, method, status_code, latency_ms, error,
			input_tokens, output_tokens, total_tokens, cached_input_tokens, reasoning_tokens, usage_source,
			estimated_cost_microusd, pricing_snapshot, gateway_attempt_count, gateway_fallback_count, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30)
	`
}

func (r *GatewayRepository) CreateRequestLog(ctx context.Context, entry gateway.RequestLog) error {
	pricingSnapshot, err := json.Marshal(entry.PricingSnapshot)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, createRequestLogSQL(),
		entry.RequestID,
		entry.UpstreamRequestID,
		nullInt64(entry.ClientKeyID),
		nullInt64(entry.ProviderAccountID),
		entry.ProviderAccountType,
		entry.ProviderAccountName,
		nullInt64(entry.RoutingPoolID),
		entry.RoutingPoolName,
		entry.RoutingPoolFallbackDepth,
		entry.RoutingPoolFallbackChain,
		entry.RoutingPoolError,
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

func (r *GatewayRepository) CreateAccountTestRequestLog(ctx context.Context, entry provider.AccountTestRequestLog) error {
	return r.CreateRequestLog(ctx, gateway.RequestLog{
		RequestID:           entry.RequestID,
		Provider:            entry.Provider,
		ProviderAccountID:   entry.ProviderAccountID,
		ProviderAccountType: entry.ProviderAccountType,
		ProviderAccountName: entry.ProviderAccountName,
		Model:               entry.Model,
		Route:               entry.Route,
		Method:              entry.Method,
		StatusCode:          entry.StatusCode,
		Latency:             entry.Latency,
		Error:               entry.Error,
		UsageSource:         "provider_test",
		CreatedAt:           entry.CreatedAt,
	})
}

func nullInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func usageSourceOrDefault(source string) string {
	if source == "" {
		return "missing"
	}
	return source
}
