package admin

import "time"

// OpsErrorStats summarizes gateway errors over a time window.
type OpsErrorStats struct {
	WindowStart          time.Time        `json:"windowStart"`
	WindowEnd            time.Time        `json:"windowEnd"`
	TotalRequests        int64            `json:"totalRequests"`
	ErrorRequests        int64            `json:"errorRequests"`
	ErrorRate            float64          `json:"errorRate"`
	TopErrors            []OpsErrorBucket `json:"topErrors"`
	TopUpstreamStatuses  []OpsErrorBucket `json:"topUpstreamStatuses"`
	TopRateLimitedModels []OpsErrorBucket `json:"topRateLimitedModels"`
	TopErrorAccounts     []OpsErrorBucket `json:"topErrorAccounts"`
	ClientErrors         int64            `json:"clientErrors"`
	ServerErrors         int64            `json:"serverErrors"`
	RateLimitErrors      int64            `json:"rateLimitErrors"`
	UpstreamErrors       int64            `json:"upstreamErrors"`
}

// OpsErrorBucket is a named count for error distribution charts.
type OpsErrorBucket struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// OpsThroughputTrend contains time-series throughput data.
type OpsThroughputTrend struct {
	Points    []OpsThroughputPoint `json:"points"`
	Interval  string               `json:"interval"`
	WindowEnd time.Time            `json:"windowEnd"`
}

// OpsThroughputPoint is a single time bucket of throughput data.
type OpsThroughputPoint struct {
	Time         time.Time `json:"time"`
	Requests     int64     `json:"requests"`
	InputTokens  int64     `json:"inputTokens"`
	OutputTokens int64     `json:"outputTokens"`
	TotalTokens  int64     `json:"totalTokens"`
	CostMicrousd int64     `json:"costMicrousd"`
	ErrorCount   int64     `json:"errorCount"`
	AvgLatencyMs float64   `json:"avgLatencyMs"`
}

// OpsErrorTrend contains time-series error rate data.
type OpsErrorTrend struct {
	Points    []OpsErrorTrendPoint `json:"points"`
	Interval  string               `json:"interval"`
	WindowEnd time.Time            `json:"windowEnd"`
}

// OpsErrorTrendPoint is a single time bucket of error data.
type OpsErrorTrendPoint struct {
	Time            time.Time `json:"time"`
	Total           int64     `json:"total"`
	ClientErrors    int64     `json:"clientErrors"`
	ServerErrors    int64     `json:"serverErrors"`
	RateLimitErrors int64     `json:"rateLimitErrors"`
	UpstreamErrors  int64     `json:"upstreamErrors"`
	GatewayErrors   int64     `json:"gatewayErrors"`
}

// OpsLatencyDistribution contains latency bucket counts.
type OpsLatencyDistribution struct {
	Buckets []OpsLatencyBucket `json:"buckets"`
}

// OpsLatencyBucket is one latency range.
type OpsLatencyBucket struct {
	Range string `json:"range"`
	MinMs int    `json:"minMs"`
	MaxMs int    `json:"maxMs"`
	Count int64  `json:"count"`
}

// OpsAccountHealth summarizes provider account scheduling and test health.
type OpsAccountHealth struct {
	WindowStart       time.Time `json:"windowStart"`
	WindowEnd         time.Time `json:"windowEnd"`
	TotalAccounts     int64     `json:"totalAccounts"`
	EnabledAccounts   int64     `json:"enabledAccounts"`
	Schedulable       int64     `json:"schedulable"`
	Disabled          int64     `json:"disabled"`
	RateLimited       int64     `json:"rateLimited"`
	CircuitOpen       int64     `json:"circuitOpen"`
	Expired           int64     `json:"expired"`
	TestedAccounts    int64     `json:"testedAccounts"`
	TestPassed        int64     `json:"testPassed"`
	TestFailed        int64     `json:"testFailed"`
	TestMissing       int64     `json:"testMissing"`
	RecentTestFailure int64     `json:"recentTestFailure"`
}

// OpsAccountTest is a provider account probe result with account context for ops review.
type OpsAccountTest struct {
	ID          int64     `json:"id"`
	AccountID   int64     `json:"accountId"`
	Provider    string    `json:"provider"`
	AccountName string    `json:"accountName"`
	AccountType string    `json:"accountType"`
	Status      string    `json:"status"`
	Message     string    `json:"message"`
	CheckedAt   time.Time `json:"checkedAt"`
	CreatedAt   time.Time `json:"createdAt"`
}

// OpsCostBreakdown summarizes estimated gateway cost attribution over a time window.
type OpsCostBreakdown struct {
	WindowStart           time.Time       `json:"windowStart"`
	WindowEnd             time.Time       `json:"windowEnd"`
	EstimatedCostMicrousd int64           `json:"estimatedCostMicrousd"`
	TopModels             []OpsCostBucket `json:"topModels"`
	TopProviderAccounts   []OpsCostBucket `json:"topProviderAccounts"`
	TopClientKeys         []OpsCostBucket `json:"topClientKeys"`
}

// OpsCostBucket is a named estimated-cost bucket with request volume.
type OpsCostBucket struct {
	Key                   string `json:"key"`
	Label                 string `json:"label"`
	Requests              int64  `json:"requests"`
	EstimatedCostMicrousd int64  `json:"estimatedCostMicrousd"`
}
