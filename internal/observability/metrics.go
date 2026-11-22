package observability

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry   *prometheus.Registry
	pool       *pgxpool.Pool
	QueueWait  *prometheus.HistogramVec
	LockWait   *prometheus.HistogramVec
	Duration   *prometheus.HistogramVec
	Retries    *prometheus.CounterVec
	Batches    *prometheus.CounterVec
	Rows       *prometheus.CounterVec
	mu         sync.Mutex
	targets    map[string]*sql.DB
	closedWait time.Duration
}

func NewMetrics(pool *pgxpool.Pool) *Metrics {
	m := &Metrics{registry: prometheus.NewRegistry(), pool: pool, targets: map[string]*sql.DB{}}
	labels := []string{"action_type", "execution_mode"}
	histogram := func(name, help string) *prometheus.HistogramVec {
		return prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: help, Buckets: []float64{.01, .05, .1, .5, 1, 5, 15, 60, 300}}, labels)
	}
	m.QueueWait = histogram("ruangdb_queue_wait_seconds", "Waktu menunggu pickup job.")
	m.LockWait = histogram("ruangdb_lock_wait_seconds", "Waktu memperoleh lock target.")
	m.Duration = histogram("ruangdb_migration_duration_seconds", "Durasi pemrosesan satu attempt worker.")
	m.Retries = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "ruangdb_retries_total", Help: "Retry otomatis worker."}, labels)
	m.Batches = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "ruangdb_batches_total", Help: "Batch committed atau compensated yang diamati worker."}, labels)
	m.Rows = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "ruangdb_rows_total", Help: "Row committed atau compensated yang diamati worker."}, labels)
	m.registry.MustRegister(m.QueueWait, m.LockWait, m.Duration, m.Retries, m.Batches, m.Rows, m)
	return m
}
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
func (m *Metrics) Target(name string, db *sql.DB) func() {
	m.mu.Lock()
	m.targets[name] = db
	m.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() { m.mu.Lock(); m.closedWait += db.Stats().WaitDuration; delete(m.targets, name); m.mu.Unlock() })
	}
}
func (m *Metrics) Describe(ch chan<- *prometheus.Desc) {}
func (m *Metrics) Collect(ch chan<- prometheus.Metric) {
	emit := func(name, help string, value float64, labels []string, values ...string) {
		kind := prometheus.GaugeValue
		if strings.HasSuffix(name, "_total") {
			kind = prometheus.CounterValue
		}
		ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(name, help, labels, nil), kind, value, values...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := m.pool.Query(ctx, `SELECT action_type,execution_mode,status,COUNT(*) FROM migration_jobs GROUP BY action_type,execution_mode,status`)
	type jobLabels struct{ action, mode, status string }
	counts := map[jobLabels]int64{}
	if err == nil {
		for rows.Next() {
			var action, mode, status string
			var count int64
			if err = rows.Scan(&action, &mode, &status, &count); err != nil {
				break
			}
			counts[jobLabels{action, mode, status}] = count
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
	}
	if err == nil {
		for _, action := range []string{"UP", "ROLLBACK", "FORWARD", "CANCEL_BATCH"} {
			for _, mode := range []string{"NORMAL", "BATCH"} {
				for _, status := range []string{"PENDING", "RUNNING", "SUCCESS", "FAILED", "PARTIAL", "PARTIALLY_REVERTED", "REVERTED", "UNKNOWN"} {
					emit("ruangdb_jobs", "Jumlah job persisten per status.", float64(counts[jobLabels{action, mode, status}]), []string{"action_type", "execution_mode", "status"}, action, mode, status)
				}
			}
		}
	}
	up := float64(1)
	if err != nil {
		up = 0
	}
	emit("ruangdb_metrics_storage_up", "Ketersediaan storage untuk scrape.", up, nil)
	stat := m.pool.Stat()
	emit("ruangdb_pool_active", "Koneksi pool aktif.", float64(stat.AcquiredConns()), []string{"pool"}, "platform")
	emit("ruangdb_pool_idle", "Koneksi pool idle.", float64(stat.IdleConns()), []string{"pool"}, "platform")
	emit("ruangdb_pool_wait_seconds_total", "Waktu akumulasi menunggu koneksi.", stat.EmptyAcquireWaitTime().Seconds(), []string{"pool"}, "platform")
	m.mu.Lock()
	defer m.mu.Unlock()
	var active, idle int
	wait := m.closedWait
	for _, db := range m.targets {
		s := db.Stats()
		active += s.InUse
		idle += s.Idle
		wait += s.WaitDuration
	}
	emit("ruangdb_pool_active", "Koneksi pool aktif.", float64(active), []string{"pool"}, "target")
	emit("ruangdb_pool_idle", "Koneksi pool idle.", float64(idle), []string{"pool"}, "target")
	emit("ruangdb_pool_wait_seconds_total", "Waktu akumulasi menunggu koneksi.", wait.Seconds(), []string{"pool"}, "target")
}
