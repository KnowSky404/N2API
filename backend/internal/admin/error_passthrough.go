package admin

import "time"

// ErrorPassthroughRule defines a rule for passing through upstream error responses.
type ErrorPassthroughRule struct {
	ID          int64     `json:"id"`
	Pattern     string    `json:"pattern"`
	MatchType   string    `json:"matchType"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	Priority    int       `json:"priority"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ErrorPassthroughRuleInput is the write payload.
type ErrorPassthroughRuleInput struct {
	Pattern     string `json:"pattern"`
	MatchType   string `json:"matchType"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Priority    int    `json:"priority"`
}
