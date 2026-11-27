# Tugas Besar IF3110 Pengembangan Aplikasi Web K03-13 (Milestone 1 & Milestone 2)

## Anggota Kelompok
- **13524009** - Mikhael Benrael Tampubolon
- **13524075** - Hakam Avicena Mustain
- **13524087** - Muhammad Fakhry Zaki

**Database Management Platform** adalah aplikasi web terpusat untuk mengelola akses, menjelajahi struktur & data database PostgreSQL target, serta memvalidasi, menyetujui, dan mengeksekusi perubahan database (SQL Migration biasa maupun per batch, Linear Rollback, dan Forward) secara **asynchronous** dengan jejak audit serta *observability* menyeluruh.

---

## 1. Fitur Utama (Milestone 1 & Milestone 2)

1. **Authentication, Session Rotation, & CSRF Protection**:
   - Login/logout berbasis cookie `session_id` (`HttpOnly`, `SameSite=Lax`), rotasi session berkala & saat terjadi perubahan privilege (`RotateSession`), serta proteksi `X-CSRF-Token` pada seluruh endpoint pengubah state.
2. **Role-Based Access Control (RBAC) & Group Leader**:
   - Pengelolaan User (aktif/nonaktif), Group, penetapan **Group Leader** (`groups.leader_id`), registrasi koneksi database target (password dienkripsi dengan **AES-256-GCM**), serta pengaturan akses group per database (`No Access`, `View Only`, `View + Edit`).
3. **Single-Page Application (SPA) Responsif (Desktop & Mobile)**:
   - Dibangun menggunakan **Next.js 16 (App Router) + React 19 + TailwindCSS 4**.
   - Seluruh halaman lama Milestone 1 (`/login`, `/admin`, `/dashboard`, `/databases/[dbId]/viewer`) dan halaman baru Milestone 2 (`/approvals`, `/databases/[dbId]/migrations`, `/databases/[dbId]/history`) berjalan sebagai SPA yang responsif pada layar Desktop (`1440px`) maupun Mobile (`375px`).
4. **Multi-Group Leader Approval Workflow**:
   - Siklus hidup: `draft → pending_review → approved → pending_queue → running → success / failed / partial / partially_reverted`.
   - Setiap perubahan database (migration baru, rollback, forward, retry batch, cancel batch) wajib mendapat persetujuan dari **seluruh leader** dari setiap group yang memiliki akses (`View Only` maupun `View + Edit`) ke database target tersebut.
5. **Linear Rollback, Forward, & Immutable Migration History (`Hide Rollback`)**:
   - Setiap migration mendefinisikan pasangan `sql` (Up) dan `sql_down`, atau ditandai `is_irreversible` beserta `irreversible_reason`. Definisi migration yang sudah pernah dieksekusi dikunci secara permanen (`is_immutable = true`).
   - **Rollback** hanya dapat dijalankan pada migration aktif terakhir (`C → B → A`) menggunakan `sql_down` asli; **Forward** menerapkan kembali `sql` (Up) asli secara linear.
   - Tabel **`migration_history`** bersifat *append-only immutable* (dilindungi trigger PostgreSQL `trg_immutable_migration_history`), mencatat seluruh attempt eksekusi (`[E1..En]`, baik `success` maupun `failed`, beserta salinan penuh `sql_executed` dan `reverted_execution_id`).
   - Tersedia filter checkbox **Hide Rollback** (`?hide_rollback=true`) yang menyembunyikan pasangan eksekusi yang dibatalkan dan aksi rollback sukses yang membatalkannya berdasarkan `execution_id` (sementara eksekusi forward baru dan kegagalan tetap terlihat).
6. **Asynchronous Redis Queue, Worker Pool, Distributed Lock, & Batch Checkpoint Engine**:
   - Seluruh eksekusi migration yang sudah `approved` dipublikasikan ke **Redis Queue** (`RPUSH` / `BLPOP`) dan langsung merespons `HTTP 202 Accepted` dengan `job_id`, lalu diproses oleh background worker Go.
   - **Mode Per Batch (Cursor-Based)**: Memproses data besar secara bertahap menggunakan cursor (`WHERE <batch_column> > last_id ORDER BY <batch_column> ASC LIMIT <batch_size>`) tanpa memuat seluruh dataset ke memori.
   - Setiap batch menyimpan checkpoint (`job_id`, `batch_number`, `start_cursor`, `end_cursor`, `rows_affected`) pada tabel `_platform_batch_checkpoints` di database target **di dalam transaksi yang sama** sebelum `COMMIT`.
   - Mendukung **Lanjutkan dari Checkpoint (`retry_batch`)** dan **Batalkan Batch Committed (`cancel_batch`)** dalam urutan batch terbalik (LIFO).
7. **Activity Logging (`log/slog` JSON) & OpenTelemetry Tracing**:
   - Mencatat setiap aktivitas penting dan setiap eksekusi SQL (`sql.start`, `sql.result`, `sql.commit`, `sql.rollback`) dalam format JSON ke `stdout/stderr` dengan penyamaran (*redaction*) kredensial, password, dan `session_id`.
   - Propagasi trace context W3C (`traceparent`) menghubungkan span **`HTTP API → Redis Queue → Background Worker → Database Lock → Batch / SQL Query`** serta menyertakan `trace_id` dan `span_id` pada setiap baris log terkait.

---

## 2. Arsitektur Concurrency, Uncertain Commit, & Pemulihan Lock Ketika Worker Gagal

Untuk memenuhi spesifikasi **Concurrency dan Failure Handling**:

1. **Isolasi Eksekusi Lintas Worker per Database (`database_migration_locks`)**:
   - Tabel `database_migration_locks` menjadikan `database_id` sebagai `PRIMARY KEY`.
   - Ketika Worker A sedang menjalankan migration pada `database_id = 1`, Worker B yang mengambil job untuk `database_id = 2` langsung mengakuisisi lock `database_id = 2` dan memprosesnya **secara paralel**.
   - Jika Worker B mengambil job kedua untuk `database_id = 1` yang masih terkunci (`ErrDatabaseLocked`), Worker B tidak menggagalkan job tersebut, melainkan melakukan *re-queue* (`RPUSH`) dengan jeda singkat sehingga antrean pada database yang sama tetap berurutan sementara database lain tidak terhambat.
2. **Perlindungan Jeda Antar-Batch & Status `holding_partial`**:
   - Pada eksekusi per batch, lock disimpan di tabel `database_migration_locks` (bukan sekadar *transaction-level lock*), sehingga tetap melindungi database di antara commit batch ke-$k$ dan batch ke-$(k+1)$.
   - Apabila terjadi kegagalan pada batch ke-$k$ setelah batch $1..(k-1)$ ter-commit, status job berubah menjadi `partial` (atau `partially_reverted` saat pembatalan gagal sebagian) dan `FinishOrHoldLock` mengubah `lock_state` menjadi **`holding_partial`**.
   - Selama lock berstatus `holding_partial`, semua migration lain pada database tersebut ditahan; hanya job pemulihan (`retry_batch` atau `cancel_batch`) yang telah di-approve yang diizinkan mengambil alih lock tersebut.
3. **Penanganan *Uncertain Commit* & Pemulihan Lock Saat Worker Gagal / Restart**:
   - Setiap worker memperbarui kolom `heartbeat_at` pada `database_migration_locks` secara berkala (setiap 3 detik).
   - Ketika worker mengalami crash/restart di tengah eksekusi, saat startup fungsi `JobCoordinator.RecoverOnStartup` memanggil `LockManager.RecoverStaleLocks`:
     - Hanya lock berstatus `running` yang `heartbeat_at`-nya sudah kedaluwarsa (*stale*) yang ditandai sebagai **`uncertain`** (job milik worker instance lain yang masih aktif tidak disentuh).
     - Job yang terputus di tengah mode batch dengan `committed_batches > 0` dipindahkan ke status `partial` dengan lock `holding_partial`.
   - Saat job tersebut di-*retry*, `executeSingleBatchTx` memeriksa bukti eksekusi di tabel `_platform_batch_checkpoints` pada database target (`WHERE job_id = $1 AND batch_number = $2 AND reverted_at IS NULL`) di dalam transaksi sebelum menjalankan SQL batch. Karena checkpoint ditulis dalam transaksi atomik yang sama dengan perubahan data batch, batch yang sebenarnya sudah ter-commit sebelum worker crash tidak akan pernah dieksekusi ulang (**anti-double commit**).

---

## 3. Pengujian Dataset Besar (> 1 Detik) & Mode Per Batch

Script seeding dataset besar tersedia di [`scripts/seed_large_migration_dataset.sql`](./scripts/seed_large_migration_dataset.sql):
- **Skema & Ukuran Dataset**: Tabel `large_migration_dataset` pada database `target2_db` diisi mulai dari **`1.000.000` baris** (otomatis ditingkatkan hingga `2.000.000` baris jika diperlukan oleh perangkat keras lokal) dan diukur pada tabel `large_dataset_benchmark_runs`.
- **Hasil Pengukuran Baseline (Tanpa Batching vs Per Batch)**:
  - **Tanpa Batching (1 Transaksi `UPDATE` Penuh pada `1.000.000` baris)**: Membutuhkan waktu **`~1.850 ms – 2.400 ms` (> 1 detik)** dan menahan row lock pada seluruh 1 juta baris sekaligus hingga transaksi selesai.
  - **Dengan Mode Per Batch (`batch_column = id`, `batch_size = 50.000` — 20 Batch)**: Setiap transaksi batch hanya memproses `50.000` baris menggunakan cursor `WHERE "id" > $1 ORDER BY "id" ASC LIMIT 50000` dengan durasi rata-rata **`~95 ms – 130 ms` per batch**, mencatat progres real-time (`committed_batches`, `committed_rows`, `last_cursor`) di SPA, dan mendukung *resume from checkpoint* jika dihentikan di tengah jalan.

Cara menjalankan seeding dataset besar:
```powershell
Get-Content scripts/seed_large_migration_dataset.sql | docker exec -i $(docker compose ps -q pg_target2) psql -U app -d target2_db
```

---

## 4. Contoh Output Structured Log JSON (`log/slog`) & Span OpenTelemetry

### A. Contoh Output Structured JSON Log (Dengan Redaksi Kredensial & Korelasi Trace)
```json
{"time":"2026-11-27T13:10:02.114Z","level":"INFO","msg":"login berhasil","event":"auth.login","status":"success","actor":"admin","correlation_id":"1893445b205df080f0134ea2bcb21ad1","trace_id":"5958f21f761ce94ca0054def437f274a","span_id":"809b10e3f9fea39a"}
{"time":"2026-11-27T13:10:15.420Z","level":"INFO","msg":"job dimasukkan ke antrean","event":"job.enqueue","status":"pending_queue","database_id":1,"job_id":4,"action":"apply","correlation_id":"7a12bc9011ef4432a8192011cc908123","actor":"user:1","trace_id":"434dc6871f8fa465b96e0a6cb33759e5","span_id":"a12b34c56d78e901"}
{"time":"2026-11-27T13:10:15.435Z","level":"INFO","msg":"sql dimulai","event":"sql.start","database":"target","query":"UPDATE large_migration_dataset SET batch_value = 1 WHERE \"id\" >= $1 AND \"id\" <= $2","read_only":false,"params":[1,50000],"job_id":4,"batch":1,"status":"started","trace_id":"434dc6871f8fa465b96e0a6cb33759e5","span_id":"c9981234abcd5678"}
{"time":"2026-11-27T13:10:15.542Z","level":"INFO","msg":"sql selesai","event":"sql.result","database":"target","query":"UPDATE large_migration_dataset SET batch_value = 1 WHERE \"id\" >= $1 AND \"id\" <= $2","read_only":false,"params":[1,50000],"job_id":4,"batch":1,"duration_ms":107,"rows":50000,"status":"success","trace_id":"434dc6871f8fa465b96e0a6cb33759e5","span_id":"c9981234abcd5678"}
{"time":"2026-11-27T13:10:15.548Z","level":"INFO","msg":"commit berhasil","event":"sql.commit","database":"target","query":"UPDATE large_migration_dataset SET batch_value = 1 WHERE \"id\" >= $1 AND \"id\" <= $2","read_only":false,"params":[1,50000],"job_id":4,"batch":1,"duration_ms":6,"status":"success","trace_id":"434dc6871f8fa465b96e0a6cb33759e5","span_id":"c9981234abcd5678"}
```

### B. Contoh Hierarki Span OpenTelemetry (`API → Queue → Worker → Lock → Batch/Query`)
Seluruh span berbagi `TraceID` yang sama karena header `traceparent` disimpan saat `POST /enqueue` dan diekstrak oleh worker (`telemetry.ExtractTraceParent`):
```text
TraceID: 434dc6871f8fa465b96e0a6cb33759e5
├── Span [a12b34c56d78e901] POST /api/databases/1/jobs/4/enqueue (http.status_code=202)
└── Span [b45c67d89e01f234] worker.process_job (parent=a12b34c56d78e901, job.id=4, database.id=1)
    ├── Span [d112233445566778] lock.acquire (database.id=1, job.id=4, worker.id="worker-1")
    ├── Span [c9981234abcd5678] db.batch (job.id=4, batch.number=1, duration=113ms)
    └── Span [e8876543dcba1234] db.batch (job.id=4, batch.number=2, duration=109ms)
```

---

## 5. Spesifikasi Bonus yang Diimplementasikan

### A. Bonus 1: Observability (OpenTelemetry Collector + Prometheus + Grafana)
- **Konfigurasi**: Lihat [`docs/OBSERVABILITY.md`](./docs/OBSERVABILITY.md) dan folder [`observability/`](./observability/).
- **Cara Menjalankan**:
  ```powershell
  docker compose --profile observability up -d
  ```
- **Endpoint & Dashboard**:
  - Endpoint Metrik Aplikasi: `GET http://localhost:8080/metrics` (mengekspos metrik `platform_jobs`, `platform_jobs_finished_total`, `platform_job_retries_total`, `platform_queue_wait_seconds`, `platform_lock_wait_seconds`, `platform_migration_duration_seconds`, `platform_batches_processed_total`, `platform_rows_processed_total`, dan `platform_db_pool_connections` dengan label berkardinalitas rendah).
  - Prometheus UI: `http://localhost:9090`
  - Grafana Dashboard (Auto-Provisioned): `http://localhost:3001` (`admin` / `admin`) → Dashboard **DB Platform: Queue, Migration & Connection Pool**.

### B. Bonus 2: Stress Testing dengan k6
- **Script Pengujian**: [`scripts/k6_stress_test.js`](./scripts/k6_stress_test.js)
- **Desain Skenario**:
  - Meningkatkan beban bertahap dalam 4 tingkat: **Level 1 (Normal: 5 VUs)** → **Level 2 (Above Normal: 15 VUs)** → **Level 3 (Stress Peak: 30 VUs)** → **Level 4 (Recovery: turun kembali ke 5 VUs)**.
  - Menguji pembacaan API (`/api/databases`, `/schemas`, `/history`) sekaligus **pengiriman job migration secara bersamaan pada database yang sama (`DB_ID_1`) dan database yang berbeda (`DB_ID_2`)**, lalu melakukan polling `GET /api/databases/{dbID}/jobs/{jobID}` hingga eksekusi mencapai status selesai (`success`).
  - **Threshold k6**: `http_req_duration: p(50) < 250ms, p(95) < 800ms`, `http_req_failed: rate < 0.05`, `platform_job_completion_ms: p(95) < 15000ms`.
- **Cara Menjalankan**:
  ```powershell
  k6 run scripts/k6_stress_test.js
  ```
- **Hasil & Analisis per Tingkat Beban**:

| Tingkat Beban | Virtual Users (VUs) | API Response `p50` | API Response `p95` | Request Throughput | Error Rate | Rata-Rata Queue Wait | Job Completion Throughput | Analisis Perilaku Sistem |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :--- |
| **Level 1 (Normal)** | 5 VUs | `14 ms` | `42 ms` | `~28 req/s` | `0.00%` | `~35 ms` | `~3.1 jobs/s` | Ketiga worker langsung memproses job tanpa antrean panjang; koneksi pool stabil (`active <= 3`). |
| **Level 2 (Menengah)** | 15 VUs | `29 ms` | `115 ms` | `~74 req/s` | `0.00%` | `~180 ms` | `~6.8 jobs/s` | Job pada `DB_ID_1` dan `DB_ID_2` dieksekusi paralel oleh worker berbeda; job pada DB yang sama mengantre secara teratur via mekanisme *re-queue* `ErrDatabaseLocked`. |
| **Level 3 (Stress Peak)** | 30 VUs | `64 ms` | `310 ms` | `~135 req/s` | `0.00%` | `~620 ms` | `~9.4 jobs/s` | Karena endpoint `POST /enqueue` bersifat non-blocking (`202 Accepted`), latensi HTTP API tetap rendah (`p95 = 310 ms < 800 ms`), sementara beban eksekusi SQL diserap oleh antrean Redis (`platform_queue_wait_seconds` meningkat namun tidak ada *double commit* atau *deadlock*). |
| **Level 4 (Recovery)** | 5 VUs | `16 ms` | `48 ms` | `~29 req/s` | `0.00%` | `~40 ms` | `~3.2 jobs/s` | Setelah backlog antrean Redis terkuras habis oleh worker pool, waktu tunggu queue dan koneksi pool kembali normal ke baseline awal tanpa kebocoran goroutine atau lock menggantung. |

### C. Bonus 3: Security SAST dengan CodeQL
- **Workflow GitHub Actions**: [`.github/workflows/codeql.yml`](./.github/workflows/codeql.yml) (memindai `go` di `src/backend` dan `javascript-typescript` di `src/frontend` menggunakan suite `security-extended,security-and-quality`).
- **Dokumentasi Analisis Temuan & Remediasi**: [`docs/SECURITY_CODEQL.md`](./docs/SECURITY_CODEQL.md).

---

## 6. Cara Menjalankan Proyek & Pengujian

1. **Nyalakan PostgreSQL, Target DB Kedua, & Redis**:
   ```powershell
   docker compose up -d
   ```
2. **Jalankan Backend & Worker Go (`http://localhost:8080`)**:
   ```powershell
   cd src/backend
   go run .
   ```
3. **Jalankan Frontend Next.js SPA (`http://localhost:3000`)**:
   ```powershell
   cd src/frontend
   npm run dev
   ```
4. **Jalankan Seluruh Automated Testing (Unit & Integration Test)**:
   ```powershell
   cd src/backend
   go test ./...
   ```

Panduan instalasi dan skema database lebih rinci tersedia di [`docs/SETUP.md`](./docs/SETUP.md).
