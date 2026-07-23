package metrics

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

type poolCollector struct {
	pool                                                                                                          *pgxpool.Pool
	connections, acquires, acquireDuration, emptyAcquires, canceledAcquires, newConnections, destroyedConnections *prometheus.Desc
}

func newPoolCollector(pool *pgxpool.Pool) *poolCollector {
	return &poolCollector{
		pool:                 pool,
		connections:          prometheus.NewDesc("n2api_database_pool_connections", "Current PostgreSQL pool connections.", []string{"state"}, nil),
		acquires:             prometheus.NewDesc("n2api_database_pool_acquires_total", "Total successful PostgreSQL pool acquires.", nil, nil),
		acquireDuration:      prometheus.NewDesc("n2api_database_pool_acquire_duration_seconds_total", "Total PostgreSQL pool acquire duration in seconds.", nil, nil),
		emptyAcquires:        prometheus.NewDesc("n2api_database_pool_empty_acquires_total", "Total PostgreSQL pool acquires that waited for a connection.", nil, nil),
		canceledAcquires:     prometheus.NewDesc("n2api_database_pool_canceled_acquires_total", "Total canceled PostgreSQL pool acquires.", nil, nil),
		newConnections:       prometheus.NewDesc("n2api_database_pool_new_connections_total", "Total PostgreSQL pool connections created.", nil, nil),
		destroyedConnections: prometheus.NewDesc("n2api_database_pool_destroyed_connections_total", "Total PostgreSQL pool connections destroyed.", []string{"reason"}, nil),
	}
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{c.connections, c.acquires, c.acquireDuration, c.emptyAcquires, c.canceledAcquires, c.newConnections, c.destroyedConnections} {
		ch <- desc
	}
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	var total, acquired, idle, maxConnections, acquires, empty, canceled, created, lifetime, idleDestroyed int64
	var acquireSeconds float64
	if c.pool != nil {
		stat := c.pool.Stat()
		total, acquired, idle, maxConnections = int64(stat.TotalConns()), int64(stat.AcquiredConns()), int64(stat.IdleConns()), int64(stat.MaxConns())
		acquires, empty, canceled, created = stat.AcquireCount(), stat.EmptyAcquireCount(), stat.CanceledAcquireCount(), stat.NewConnsCount()
		lifetime, idleDestroyed = stat.MaxLifetimeDestroyCount(), stat.MaxIdleDestroyCount()
		acquireSeconds = stat.AcquireDuration().Seconds()
	}
	for state, value := range map[string]int64{"total": total, "acquired": acquired, "idle": idle, "max": maxConnections} {
		ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(value), state)
	}
	ch <- prometheus.MustNewConstMetric(c.acquires, prometheus.CounterValue, float64(acquires))
	ch <- prometheus.MustNewConstMetric(c.acquireDuration, prometheus.CounterValue, acquireSeconds)
	ch <- prometheus.MustNewConstMetric(c.emptyAcquires, prometheus.CounterValue, float64(empty))
	ch <- prometheus.MustNewConstMetric(c.canceledAcquires, prometheus.CounterValue, float64(canceled))
	ch <- prometheus.MustNewConstMetric(c.newConnections, prometheus.CounterValue, float64(created))
	ch <- prometheus.MustNewConstMetric(c.destroyedConnections, prometheus.CounterValue, float64(lifetime), "max_lifetime")
	ch <- prometheus.MustNewConstMetric(c.destroyedConnections, prometheus.CounterValue, float64(idleDestroyed), "max_idle")
	otherDestroyed := max(0, created-total-lifetime-idleDestroyed)
	ch <- prometheus.MustNewConstMetric(c.destroyedConnections, prometheus.CounterValue, float64(otherDestroyed), "other")
}
