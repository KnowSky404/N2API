package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
)

func (r *AdminRepository) GetOpsErrorStats(ctx context.Context, since time.Time) (admin.OpsErrorStats, error) {
	now := time.Now()
	stats := admin.OpsErrorStats{
		WindowStart: since,
		WindowEnd:   now,
	}

	row := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN l.status_code >= 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN l.status_code >= 400 AND l.status_code < 500 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN l.status_code >= 500 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN l.status_code = 429 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN l.status_code >= 502 THEN 1 ELSE 0 END), 0)
		FROM request_logs l
		WHERE l.created_at >= $1
	`, since)
	if err := row.Scan(
		&stats.TotalRequests,
		&stats.ErrorRequests,
		&stats.ClientErrors,
		&stats.ServerErrors,
		&stats.RateLimitErrors,
		&stats.UpstreamErrors,
	); err != nil {
		return stats, err
	}
	if stats.TotalRequests > 0 {
		stats.ErrorRate = float64(stats.ErrorRequests) / float64(stats.TotalRequests)
	}

	topErrors, err := r.opsErrorBuckets(ctx, since, "error", 5)
	if err != nil {
		return stats, err
	}
	stats.TopErrors = topErrors

	topUpstreamStatuses, err := r.opsErrorBuckets(ctx, since, "status", 5)
	if err != nil {
		return stats, err
	}
	stats.TopUpstreamStatuses = topUpstreamStatuses

	topRateLimited, err := r.opsErrorBuckets(ctx, since, "rate_limited_model", 5)
	if err != nil {
		return stats, err
	}
	stats.TopRateLimitedModels = topRateLimited

	topAccounts, err := r.opsErrorBuckets(ctx, since, "account", 5)
	if err != nil {
		return stats, err
	}
	stats.TopErrorAccounts = topAccounts

	return stats, nil
}

func (r *AdminRepository) opsErrorBuckets(ctx context.Context, since time.Time, kind string, limit int) ([]admin.OpsErrorBucket, error) {
	var query string
	switch kind {
	case "error":
		query = `
			SELECT COALESCE(NULLIF(l.error, ''), 'unknown'), COUNT(*)
			FROM request_logs l
			WHERE l.created_at >= $1 AND l.status_code >= 400
			GROUP BY 1 ORDER BY 2 DESC LIMIT ` + strconv.Itoa(limit)
	case "status":
		query = `
			SELECT l.status_code::text, COUNT(*)
			FROM request_logs l
			WHERE l.created_at >= $1 AND l.status_code >= 400
			GROUP BY 1 ORDER BY 2 DESC LIMIT ` + strconv.Itoa(limit)
	case "rate_limited_model":
		query = `
			SELECT COALESCE(NULLIF(l.model, ''), 'unknown'), COUNT(*)
			FROM request_logs l
			WHERE l.created_at >= $1 AND l.status_code = 429
			GROUP BY 1 ORDER BY 2 DESC LIMIT ` + strconv.Itoa(limit)
	case "account":
		query = `
			SELECT l.provider_account_id::text, COALESCE(NULLIF(l.provider_account_name, ''), 'unknown'), COUNT(*)
			FROM request_logs l
			WHERE l.created_at >= $1 AND l.status_code >= 400
				AND l.provider_account_id > 0
			GROUP BY 1, 2 ORDER BY 3 DESC LIMIT ` + strconv.Itoa(limit)
	default:
		return nil, fmt.Errorf("unknown ops error bucket kind: %s", kind)
	}

	rows, err := r.pool.Query(ctx, query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	buckets := make([]admin.OpsErrorBucket, 0, limit)
	for rows.Next() {
		var bucket admin.OpsErrorBucket
		if kind == "account" {
			if err := rows.Scan(&bucket.Key, &bucket.Label, &bucket.Count); err != nil {
				return nil, err
			}
		} else {
			if err := rows.Scan(&bucket.Key, &bucket.Count); err != nil {
				return nil, err
			}
			bucket.Label = bucket.Key
		}
		buckets = append(buckets, bucket)
	}
	return buckets, rows.Err()
}

func (r *AdminRepository) GetOpsThroughputTrend(ctx context.Context, since time.Time, interval string) (admin.OpsThroughputTrend, error) {
	bucketExpr := opsBucketExpr(interval)
	orderExpr := opsOrderExpr(interval)

	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT
			%s,
			COUNT(*),
			COALESCE(SUM(l.input_tokens), 0),
			COALESCE(SUM(l.output_tokens), 0),
			COALESCE(SUM(l.total_tokens), 0),
			COALESCE(SUM(l.estimated_cost_microusd), 0),
			COALESCE(SUM(CASE WHEN l.status_code >= 400 THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(l.latency_ms) FILTER (WHERE l.status_code < 400), 0)
		FROM request_logs l
		WHERE l.created_at >= $1
		GROUP BY 1
		ORDER BY %s ASC
	`, bucketExpr, orderExpr), since)
	if err != nil {
		return admin.OpsThroughputTrend{}, err
	}
	defer rows.Close()

	trend := admin.OpsThroughputTrend{
		Interval:  interval,
		WindowEnd: time.Now(),
	}
	for rows.Next() {
		var point admin.OpsThroughputPoint
		if err := rows.Scan(
			&point.Time,
			&point.Requests,
			&point.InputTokens,
			&point.OutputTokens,
			&point.TotalTokens,
			&point.CostMicrousd,
			&point.ErrorCount,
			&point.AvgLatencyMs,
		); err != nil {
			return trend, err
		}
		trend.Points = append(trend.Points, point)
	}
	return trend, rows.Err()
}

func (r *AdminRepository) GetOpsErrorTrend(ctx context.Context, since time.Time, interval string) (admin.OpsErrorTrend, error) {
	bucketExpr := opsBucketExpr(interval)
	orderExpr := opsOrderExpr(interval)

	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT
			%s,
			COUNT(*),
			COALESCE(SUM(CASE WHEN l.status_code >= 400 AND l.status_code < 500 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN l.status_code >= 500 AND l.status_code < 600 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN l.status_code = 429 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN l.status_code >= 502 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN l.error <> '' AND l.status_code >= 400 THEN 1 ELSE 0 END), 0)
		FROM request_logs l
		WHERE l.created_at >= $1
		GROUP BY 1
		ORDER BY %s ASC
	`, bucketExpr, orderExpr), since)
	if err != nil {
		return admin.OpsErrorTrend{}, err
	}
	defer rows.Close()

	trend := admin.OpsErrorTrend{
		Interval:  interval,
		WindowEnd: time.Now(),
	}
	for rows.Next() {
		var point admin.OpsErrorTrendPoint
		var upstream, gateway int64
		if err := rows.Scan(
			&point.Time,
			&point.Total,
			&point.ClientErrors,
			&point.ServerErrors,
			&point.RateLimitErrors,
			&upstream,
			&gateway,
		); err != nil {
			return trend, err
		}
		point.UpstreamErrors = upstream
		point.GatewayErrors = gateway
		trend.Points = append(trend.Points, point)
	}
	return trend, rows.Err()
}

func (r *AdminRepository) GetOpsLatencyDistribution(ctx context.Context, since time.Time) (admin.OpsLatencyDistribution, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			l.latency_ms,
			COUNT(*)
		FROM request_logs l
		WHERE l.created_at >= $1
			AND l.status_code >= 200
			AND l.status_code < 400
			AND l.latency_ms > 0
		GROUP BY 1
	`, since)
	if err != nil {
		return admin.OpsLatencyDistribution{}, err
	}
	defer rows.Close()

	buckets := map[string]int64{
		"0-500ms":  0,
		"500ms-1s": 0,
		"1s-2s":    0,
		"2s-5s":    0,
		"5s-10s":   0,
		"10s-30s":  0,
		">30s":     0,
	}
	for rows.Next() {
		var latencyMs int
		var count int64
		if err := rows.Scan(&latencyMs, &count); err != nil {
			return admin.OpsLatencyDistribution{}, err
		}
		switch {
		case latencyMs <= 500:
			buckets["0-500ms"] += count
		case latencyMs <= 1000:
			buckets["500ms-1s"] += count
		case latencyMs <= 2000:
			buckets["1s-2s"] += count
		case latencyMs <= 5000:
			buckets["2s-5s"] += count
		case latencyMs <= 10000:
			buckets["5s-10s"] += count
		case latencyMs <= 30000:
			buckets["10s-30s"] += count
		default:
			buckets[">30s"] += count
		}
	}
	if err := rows.Err(); err != nil {
		return admin.OpsLatencyDistribution{}, err
	}

	rangeOrder := []struct {
		label string
		minMs int
		maxMs int
	}{
		{"0-500ms", 0, 500},
		{"500ms-1s", 500, 1000},
		{"1s-2s", 1000, 2000},
		{"2s-5s", 2000, 5000},
		{"5s-10s", 5000, 10000},
		{"10s-30s", 10000, 30000},
		{">30s", 30000, 0},
	}

	dist := admin.OpsLatencyDistribution{}
	for _, rng := range rangeOrder {
		if count, ok := buckets[rng.label]; ok {
			dist.Buckets = append(dist.Buckets, admin.OpsLatencyBucket{
				Range: rng.label,
				MinMs: rng.minMs,
				MaxMs: rng.maxMs,
				Count: count,
			})
		}
	}
	return dist, nil
}

func (r *AdminRepository) GetOpsAccountHealth(ctx context.Context, since time.Time) (admin.OpsAccountHealth, error) {
	now := time.Now()
	health := admin.OpsAccountHealth{
		WindowStart: since,
		WindowEnd:   now,
	}

	row := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN enabled THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN enabled
				AND (
					status = ''
					OR status = 'active'
					OR (status = 'rate_limited' AND rate_limited_until IS NOT NULL AND rate_limited_until <= $2)
					OR (status = 'circuit_open' AND circuit_open_until IS NOT NULL AND circuit_open_until <= $2)
				)
				AND (rate_limited_until IS NULL OR rate_limited_until <= $2)
				AND (circuit_open_until IS NULL OR circuit_open_until <= $2)
				THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN NOT enabled OR status = 'disabled' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'rate_limited'
				AND (rate_limited_until IS NULL OR rate_limited_until > $2)
				THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'circuit_open'
				AND (circuit_open_until IS NULL OR circuit_open_until > $2)
				THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'expired' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN last_test_at IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN last_test_status = 'passed' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN last_test_status = 'failed' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN last_test_at IS NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN last_test_at >= $1 AND last_test_status = 'failed' THEN 1 ELSE 0 END), 0)
		FROM provider_accounts
	`, since, now)
	if err := row.Scan(
		&health.TotalAccounts,
		&health.EnabledAccounts,
		&health.Schedulable,
		&health.Disabled,
		&health.RateLimited,
		&health.CircuitOpen,
		&health.Expired,
		&health.TestedAccounts,
		&health.TestPassed,
		&health.TestFailed,
		&health.TestMissing,
		&health.RecentTestFailure,
	); err != nil {
		return health, err
	}
	return health, nil
}

func opsBucketExpr(interval string) string {
	switch interval {
	case "minute":
		return "date_trunc('minute', l.created_at)::timestamptz"
	case "hour":
		return "date_trunc('hour', l.created_at)::timestamptz"
	case "day":
		return "date_trunc('day', l.created_at)::timestamptz"
	default:
		return "date_trunc('hour', l.created_at)::timestamptz"
	}
}

func opsOrderExpr(interval string) string {
	switch interval {
	case "minute":
		return "date_trunc('minute', l.created_at)"
	case "hour":
		return "date_trunc('hour', l.created_at)"
	case "day":
		return "date_trunc('day', l.created_at)"
	default:
		return "date_trunc('hour', l.created_at)"
	}
}
