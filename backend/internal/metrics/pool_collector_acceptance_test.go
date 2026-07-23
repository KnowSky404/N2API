package metrics

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func TestMetricsAcceptanceScrapesUnavailablePostgresWithoutQueries(t *testing.T) {
	for _, test := range []struct {
		name              string
		closeBeforeScrape bool
	}{
		{name: "unreachable pool"},
		{name: "closed pool", closeBeforeScrape: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := pgxpool.ParseConfig("postgres://metrics:unused@unreachable.invalid/n2api?sslmode=disable")
			if err != nil {
				t.Fatal(err)
			}
			config.MinConns = 0
			config.MinIdleConns = 0
			config.MaxConns = 4
			var dialCalls atomic.Int32
			config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
				dialCalls.Add(1)
				return nil, errors.New("unexpected postgres dial")
			}
			pool, err := pgxpool.NewWithConfig(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			if test.closeBeforeScrape {
				pool.Close()
			} else {
				t.Cleanup(pool.Close)
			}

			registry := New(pool)
			server := httptest.NewServer(NewHTTPServer("127.0.0.1:0", "", registry.Handler(), context.Background()).Handler)
			t.Cleanup(server.Close)
			client := &http.Client{Timeout: time.Second}
			response, err := client.Get(server.URL + "/metrics")
			if err != nil {
				t.Fatalf("scrape unavailable pool metrics: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(response.Body)
				t.Fatalf("scrape status = %d body=%q", response.StatusCode, body)
			}
			parser := expfmt.NewTextParser(model.LegacyValidation)
			families, err := parser.TextToMetricFamilies(response.Body)
			if err != nil {
				t.Fatalf("parse metrics scrape: %v", err)
			}
			family := families["n2api_database_pool_connections"]
			if family == nil || len(family.Metric) != 4 {
				t.Fatalf("database pool connection metrics = %+v, want four states", family)
			}
			if got := dialCalls.Load(); got != 0 {
				t.Fatalf("metrics scrape attempted %d PostgreSQL dials, want zero", got)
			}
		})
	}
}
