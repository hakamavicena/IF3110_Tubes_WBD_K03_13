package telemetry

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type metricFamily interface{ write(w io.Writer) }

type Registry struct {
	mu       sync.Mutex
	families []metricFamily
	pools    map[string]func() PoolStat
}

func NewRegistry() *Registry { return &Registry{pools: map[string]func() PoolStat{}} }

func (r *Registry) add(f metricFamily) {
	r.mu.Lock()
	r.families = append(r.families, f)
	r.mu.Unlock()
}

func escapeLabel(v string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`).Replace(v)
}

func labelString(names, values []string) string {
	if len(names) == 0 {
		return ""
	}
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf(`%s="%s"`, n, escapeLabel(values[i]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func formatFloat(f float64) string {
	if math.IsInf(f, 1) {
		return "+Inf"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

type CounterVec struct {
	name, help string
	labels     []string
	mu         sync.Mutex
	values     map[string]*float64
	lv         map[string][]string
}

func (r *Registry) NewCounterVec(name, help string, labels ...string) *CounterVec {
	c := &CounterVec{name: name, help: help, labels: labels, values: map[string]*float64{}, lv: map[string][]string{}}
	r.add(c)
	return c
}

func (c *CounterVec) Add(v float64, labelValues ...string) {
	if v < 0 || len(labelValues) != len(c.labels) {
		return
	}
	key := strings.Join(labelValues, "\xff")
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.values[key]
	if !ok {
		p = new(float64)
		c.values[key] = p
		c.lv[key] = append([]string(nil), labelValues...)
	}
	*p += v
}

func (c *CounterVec) Inc(labelValues ...string) { c.Add(1, labelValues...) }

func (c *CounterVec) write(w io.Writer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name)
	keys := sortedKeys(c.values)
	for _, k := range keys {
		fmt.Fprintf(w, "%s%s %s\n", c.name, labelString(c.labels, c.lv[k]), formatFloat(*c.values[k]))
	}
}

type GaugeVec struct {
	name, help string
	labels     []string
	mu         sync.Mutex
	values     map[string]*float64
	lv         map[string][]string
}

func (r *Registry) NewGaugeVec(name, help string, labels ...string) *GaugeVec {
	g := &GaugeVec{name: name, help: help, labels: labels, values: map[string]*float64{}, lv: map[string][]string{}}
	r.add(g)
	return g
}

func (g *GaugeVec) slot(labelValues []string) *float64 {
	key := strings.Join(labelValues, "\xff")
	p, ok := g.values[key]
	if !ok {
		p = new(float64)
		g.values[key] = p
		g.lv[key] = append([]string(nil), labelValues...)
	}
	return p
}

func (g *GaugeVec) Set(v float64, labelValues ...string) {
	if len(labelValues) != len(g.labels) {
		return
	}
	g.mu.Lock()
	*g.slot(labelValues) = v
	g.mu.Unlock()
}

func (g *GaugeVec) Add(v float64, labelValues ...string) {
	if len(labelValues) != len(g.labels) {
		return
	}
	g.mu.Lock()
	*g.slot(labelValues) += v
	g.mu.Unlock()
}

func (g *GaugeVec) write(w io.Writer) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", g.name, g.help, g.name)
	for _, k := range sortedKeys(g.values) {
		fmt.Fprintf(w, "%s%s %s\n", g.name, labelString(g.labels, g.lv[k]), formatFloat(*g.values[k]))
	}
}

// ---- Histogram ----

type histSeries struct {
	counts []uint64
	sum    float64
	count  uint64
}

type HistogramVec struct {
	name, help string
	labels     []string
	buckets    []float64
	mu         sync.Mutex
	series     map[string]*histSeries
	lv         map[string][]string
}

func (r *Registry) NewHistogramVec(name, help string, buckets []float64, labels ...string) *HistogramVec {
	b := append([]float64(nil), buckets...)
	sort.Float64s(b)
	h := &HistogramVec{name: name, help: help, labels: labels, buckets: b, series: map[string]*histSeries{}, lv: map[string][]string{}}
	r.add(h)
	return h
}

func (h *HistogramVec) Observe(v float64, labelValues ...string) {
	if len(labelValues) != len(h.labels) {
		return
	}
	key := strings.Join(labelValues, "\xff")
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.series[key]
	if !ok {
		s = &histSeries{counts: make([]uint64, len(h.buckets))}
		h.series[key] = s
		h.lv[key] = append([]string(nil), labelValues...)
	}
	for i, ub := range h.buckets {
		if v <= ub {
			s.counts[i]++
		}
	}
	s.sum += v
	s.count++
}

func (h *HistogramVec) write(w io.Writer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name)
	keys := make([]string, 0, len(h.series))
	for k := range h.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s := h.series[k]
		names := append(append([]string(nil), h.labels...), "le")
		for i, ub := range h.buckets {
			vals := append(append([]string(nil), h.lv[k]...), formatFloat(ub))
			fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, labelString(names, vals), s.counts[i])
		}
		vals := append(append([]string(nil), h.lv[k]...), "+Inf")
		fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, labelString(names, vals), s.count)
		fmt.Fprintf(w, "%s_sum%s %s\n", h.name, labelString(h.labels, h.lv[k]), formatFloat(s.sum))
		fmt.Fprintf(w, "%s_count%s %d\n", h.name, labelString(h.labels, h.lv[k]), s.count)
	}
}

func sortedKeys(m map[string]*float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}


type PoolStat struct {
	Acquired     int32 
	Idle         int32
	Max          int32
	AcquireCount int64
	WaitCount    int64         
	WaitDuration time.Duration 
}

type Metrics struct {
	Registry *Registry

	jobsInState   *GaugeVec
	jobsFinished  *CounterVec
	jobRetries    *CounterVec
	queueWait     *HistogramVec
	lockWait      *HistogramVec
	migDuration   *HistogramVec
	batches       *CounterVec
	rows          *CounterVec
	httpRequests  *CounterVec
	httpDuration  *HistogramVec
	poolConns     *GaugeVec
	poolWaitSecs  *GaugeVec
	poolWaitCount *GaugeVec
	poolAcquires  *GaugeVec
}

var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 300, 900}

func NewMetrics() *Metrics {
	r := NewRegistry()
	return &Metrics{
		Registry:      r,
		jobsInState:   r.NewGaugeVec("platform_jobs", "Jumlah job migration per status saat ini.", "status"),
		jobsFinished:  r.NewCounterVec("platform_jobs_finished_total", "Total job migration yang selesai per status akhir.", "status"),
		jobRetries:    r.NewCounterVec("platform_job_retries_total", "Total retry job akibat kegagalan sementara.", "reason"),
		queueWait:     r.NewHistogramVec("platform_queue_wait_seconds", "Waktu job menunggu di queue sebelum diambil worker.", durationBuckets),
		lockWait:      r.NewHistogramVec("platform_lock_wait_seconds", "Waktu job menunggu lock database.", durationBuckets),
		migDuration:   r.NewHistogramVec("platform_migration_duration_seconds", "Durasi eksekusi migration.", durationBuckets, "mode", "action"),
		batches:       r.NewCounterVec("platform_batches_processed_total", "Total batch yang berhasil di-commit.", "action"),
		rows:          r.NewCounterVec("platform_rows_processed_total", "Total row yang diproses migration.", "action"),
		httpRequests:  r.NewCounterVec("platform_http_requests_total", "Total request HTTP API.", "method", "code_class"),
		httpDuration:  r.NewHistogramVec("platform_http_request_duration_seconds", "Durasi request HTTP API.", durationBuckets, "method"),
		poolConns:     r.NewGaugeVec("platform_db_pool_connections", "Koneksi pool database per state (active/idle).", "pool", "state"),
		poolWaitSecs:  r.NewGaugeVec("platform_db_pool_wait_seconds_total", "Total waktu menunggu koneksi dari pool (kumulatif).", "pool"),
		poolWaitCount: r.NewGaugeVec("platform_db_pool_wait_total", "Total acquire yang harus menunggu koneksi (kumulatif).", "pool"),
		poolAcquires:  r.NewGaugeVec("platform_db_pool_acquire_total", "Total acquire koneksi (kumulatif).", "pool"),
	}
}

var jobStatuses = map[string]bool{
	"draft": true, "pending_review": true, "approved": true, "pending_queue": true, "running": true,
	"success": true, "failed": true, "partial": true, "partially_reverted": true,
}

func safeStatus(s string) string {
	if jobStatuses[s] {
		return s
	}
	return "other"
}

func (m *Metrics) JobEntered(status string) { m.jobsInState.Add(1, safeStatus(status)) }
func (m *Metrics) JobLeft(status string)    { m.jobsInState.Add(-1, safeStatus(status)) }

func (m *Metrics) SetJobsInState(status string, n int) {
	m.jobsInState.Set(float64(n), safeStatus(status))
}

func (m *Metrics) JobFinished(status string) { m.jobsFinished.Inc(safeStatus(status)) }

func (m *Metrics) JobRetried(reason string) {
	switch reason {
	case "transient", "uncertain_commit", "lock_busy":
	default:
		reason = "other"
	}
	m.jobRetries.Inc(reason)
}

func (m *Metrics) ObserveQueueWait(d time.Duration) { m.queueWait.Observe(d.Seconds()) }
func (m *Metrics) ObserveLockWait(d time.Duration)  { m.lockWait.Observe(d.Seconds()) }

func normAction(a string) string {
	switch a {
	case "apply", "rollback", "forward", "retry_batch", "cancel_batch":
		return a
	}
	return "other"
}

func normMode(mode string) string {
	if mode == "batch" {
		return "batch"
	}
	return "standard"
}

func (m *Metrics) ObserveMigration(mode, action string, d time.Duration) {
	m.migDuration.Observe(d.Seconds(), normMode(mode), normAction(action))
}

func (m *Metrics) BatchCommitted(action string, rows int64) {
	m.batches.Inc(normAction(action))
	if rows > 0 {
		m.rows.Add(float64(rows), normAction(action))
	}
}

func (m *Metrics) RowsProcessed(action string, rows int64) {
	if rows > 0 {
		m.rows.Add(float64(rows), normAction(action))
	}
}

func (m *Metrics) ObserveHTTP(method string, status int, d time.Duration) {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
	default:
		method = "OTHER"
	}
	m.httpRequests.Inc(method, strconv.Itoa(status/100)+"xx")
	m.httpDuration.Observe(d.Seconds(), method)
}

func (m *Metrics) RegisterPool(name string, stat func() PoolStat) {
	m.Registry.mu.Lock()
	m.Registry.pools[name] = stat
	m.Registry.mu.Unlock()
}

func (m *Metrics) collectPools() {
	m.Registry.mu.Lock()
	pools := make(map[string]func() PoolStat, len(m.Registry.pools))
	for k, v := range m.Registry.pools {
		pools[k] = v
	}
	m.Registry.mu.Unlock()
	for name, f := range pools {
		s := f()
		m.poolConns.Set(float64(s.Acquired), name, "active")
		m.poolConns.Set(float64(s.Idle), name, "idle")
		m.poolWaitSecs.Set(s.WaitDuration.Seconds(), name)
		m.poolWaitCount.Set(float64(s.WaitCount), name)
		m.poolAcquires.Set(float64(s.AcquireCount), name)
	}
}

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.collectPools()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		m.Registry.mu.Lock()
		fams := append([]metricFamily(nil), m.Registry.families...)
		m.Registry.mu.Unlock()
		for _, f := range fams {
			f.write(w)
		}
	})
}
