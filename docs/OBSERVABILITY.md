# Observability: Logging, Tracing, Metrik

Paket: `src/backend/dbengine/pkg/telemetry`. Bonus (Collector + Prometheus + Grafana): folder `observability/` dan profile `observability` di `docker-compose.yml`.

## 1. Structured log JSON (`log/slog`, stdout)

Diinisialisasi di `main.go` (`telemetry.SetupLogging()`), sehingga `slog.*` **dan** `log.Printf` lama keluar sebagai JSON. Atribut standar: `time`, `level`, `msg`, `event`, `actor`, `database`, `status`, `correlation_id`, serta `trace_id`/`span_id` otomatis bila ada span aktif di `ctx`. Selalu gunakan varian `*Context` (`slog.InfoContext(ctx, ...)`) agar korelasi ikut tercatat.

Contoh keluaran nyata (login, submit-review):

```json
{"time":"2026-11-25T23:32:08.080Z","level":"INFO","msg":"login berhasil","event":"auth.login","status":"success","actor":"admin","correlation_id":"1893445b205df080f0134ea2bcb21ad1","trace_id":"5958f21f761ce94ca0054def437f274a","span_id":"809b10e3f9fea39a"}
{"time":"2026-11-25T23:32:08.586Z","level":"INFO","msg":"job diajukan untuk review","event":"approval.submit_review","status":"success","actor_id":2,"database_id":1,"job_id":1,"required_groups":1,"correlation_id":"59a503213373572d0667d0d58626b0db","actor":"user:2","trace_id":"8f6f8fd90ea0b9886ea597fb77cadcc7","span_id":"882fc64a7dff78be"}
```

**Redaksi otomatis** (`redact.go`): atribut berkunci `password`, `secret`, `token`, `session_id`, `cookie`, `authorization`, `credential`, `api_key` menjadi `[REDACTED]`; teks bebas disamarkan untuk URL berkredensial (`postgres://u:***@`), pasangan `password=...`, header `Bearer/Basic`, dan cookie `session_id`. Query string request tidak pernah dicatat. `X-Correlation-ID` dari klien hanya diterima bila `[A-Za-z0-9._-]{8,64}` (anti log-injection); selain itu dibuat baru dan dikembalikan di header respons.

### Log eksekusi SQL (untuk Orang 2 & 3)
Panggil empat fungsi terpisah di sekitar setiap eksekusi (termasuk read-only, batch, retry):

```go
ev := telemetry.SQLEvent{Database: name, Query: q, Params: args, JobID: j.ID, ExecutionID: e, Attempt: n, Batch: b, RowCount: -1}
telemetry.SQLStart(ctx, ev)
// ... exec ...
ev.Duration, ev.RowCount, ev.Err = time.Since(t0), tag.RowsAffected(), err
telemetry.SQLResult(ctx, ev)   // event sql.result (+ SQLSTATE bila PgError)
telemetry.SQLCommit(ctx, ev)   // atau telemetry.SQLRollback(ctx, ev)
```
Event: `sql.start`, `sql.result`, `sql.commit`, `sql.rollback`. Query dan parameter disamarkan (`PASSWORD '...'`, parameter pada query yang menyentuh kolom sensitif).

## 2. Tracing OpenTelemetry

`telemetry.InitTracing` dipanggil di `main.go`; exporter lewat `OTEL_TRACES_EXPORTER`: `console` (default, stderr), `none`, `otlp` (butuh `go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@v1.37.0` lalu `go build -tags otlp`, endpoint dari `OTEL_EXPORTER_OTLP_ENDPOINT`).

Middleware HTTP membuat span root per request (menghormati `traceparent` masuk). Menyambung **API → queue → worker → lock → batch/query**:

```go
// 1) API (handler enqueue): simpan ke migration_jobs.trace_parent / payload Redis
tp := telemetry.InjectTraceParent(ctx)

// 2) Worker (proses lain): pulihkan konteks, lalu buat span anak
ctx = telemetry.ExtractTraceParent(context.Background(), job.TraceParent)
ctx, span := telemetry.StartSpan(ctx, "worker.run", attribute.Int64("job_id", job.ID)); defer func(){ telemetry.EndSpan(span, err) }()
lockCtx, lockSpan := telemetry.StartSpan(ctx, "lock.acquire")   // lalu "db.batch", "db.query" dst.
```
`EndSpan` mencatat error + status pada span. Terverifikasi oleh `TestTraceParentPropagationThroughQueue` (satu trace, hubungan parent-child benar).

Contoh span (exporter console, ringkas):
```
"Name": "POST /api/databases/1/jobs/1/approve", "TraceID": "434dc6871f8fa465b96e0a6cb33759e5",
"Attributes": http.request.method=POST, url.path=/api/databases/1/jobs/1/approve, correlation_id=..., http.response.status_code=200
```

## 3. Metrik Prometheus (`GET /metrics`)

Implementasi format teks Prometheus tanpa dependensi eksternal (`metrics.go`). Hanya label berkardinalitas rendah (tanpa user ID, job ID, atau SQL).

| Metrik | Label | Pemanggil |
| :--- | :--- | :--- |
| `platform_jobs` (gauge) | `status` | `m.JobEntered(s)` / `m.JobLeft(s)` / `m.SetJobsInState` |
| `platform_jobs_finished_total` | `status` | `m.JobFinished(s)` |
| `platform_job_retries_total` | `reason` (`transient`, `uncertain_commit`, `lock_busy`) | `m.JobRetried(r)` |
| `platform_queue_wait_seconds`, `platform_lock_wait_seconds` (histogram) | – | `m.ObserveQueueWait(d)`, `m.ObserveLockWait(d)` |
| `platform_migration_duration_seconds` (histogram) | `mode`, `action` | `m.ObserveMigration(mode, action, d)` |
| `platform_batches_processed_total`, `platform_rows_processed_total` | `action` | `m.BatchCommitted(action, rows)`, `m.RowsProcessed` |
| `platform_http_requests_total`, `platform_http_request_duration_seconds` | `method`, `code_class` | otomatis (middleware) |
| `platform_db_pool_connections` | `pool` (`internal`,`targets`), `state` (`active`,`idle`) | otomatis |
| `platform_db_pool_wait_seconds_total`, `platform_db_pool_wait_total`, `platform_db_pool_acquire_total` | `pool` | otomatis |

> **Catatan integrasi:** metrik HTTP dan connection pool sudah aktif. Metrik queue/migration baru terisi setelah modul queue/worker (Orang 3) memanggil method `*telemetry.Metrics` di atas (instance `metrics` dibuat di `main.go`; berikan ke worker via konstruktor).

## 4. Menjalankan stack bonus (Collector + Prometheus + Grafana)

```bash
docker compose --profile observability up -d
cd src/backend
# alirkan stdout backend ke file yang dibaca Collector (filelog):
go run . 2>>../../observability/logs/backend.err | tee ../../observability/logs/backend.log      # PowerShell: ... | Tee-Object -FilePath ..\..\observability\logs\backend.log
```
- Prometheus: http://localhost:9090 (scrape `host.docker.internal:8080/metrics`; bila `METRICS_TOKEN` diset, isi `authorization` di `observability/prometheus.yml`).
- Grafana: http://localhost:3001 (admin/admin) → folder **DB Platform** → dashboard *Queue, Migration & Connection Pool* (provisioning otomatis dari `observability/grafana/`).
- Collector: log JSON dibaca dari `observability/logs/*.log` (trace_id/span_id di-parse ke konteks trace), trace diterima via OTLP (`:4318`) bila backend dibangun dengan `-tags otlp`; keluaran ke console dan `/var/lib/otelcol/{traces,logs}.jsonl` (volume `otel_data`). Ganti exporter `file` dengan Tempo/Loki/Jaeger bila ingin backend terpusat.

> Konfigurasi Compose/Collector/Grafana divalidasi sintaksnya (YAML/JSON) tetapi belum dijalankan end-to-end pada lingkungan pengembangan ini (tanpa Docker). Jalankan sekali untuk memverifikasi sebelum demo.
