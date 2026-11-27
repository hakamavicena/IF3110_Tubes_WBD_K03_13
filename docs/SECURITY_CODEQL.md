# Dokumentasi Bonus Security SAST (CodeQL) — Milestone 2

Dokumen ini mencatat konfigurasi workflow **GitHub CodeQL SAST (*Static Application Security Testing*)**, kendala teknis eksekusi hosted runner pada akun pemilik repositori utama, serta pemetaan analisis statis alur data (*Taint Tracking: Source → Sanitizer → Sink*) berdasarkan katalog rule resmi CodeQL pada kode **IF3110 Tugas Besar WBD K03-13**.

---

## 1. Konfigurasi Workflow CodeQL ([`.github/workflows/codeql.yml`](../.github/workflows/codeql.yml))

Workflow SAST telah diaktifkan pada repositori melalui file [`.github/workflows/codeql.yml`](../.github/workflows/codeql.yml) dengan rancangan khusus untuk struktur monorepo proyek ini:
- **Matrix Bahasa yang Dianalisis**:
  1. **`go`** (`build-mode: manual` dengan `working-directory: src/backend` dan perintah `go build ./...` agar extractor CodeQL memindai modul utama `src/backend/go.mod`, bukan file `go.mod` kosong di root).
  2. **`javascript-typescript`** (`build-mode: none` untuk memindai langsung AST seluruh kode TypeScript/React Next.js SPA di `src/frontend/`).
- **Query Suite**: `security-extended,security-and-quality`.
- **Event Trigger**: `push`, `pull_request`, `schedule` (mingguan), dan `workflow_dispatch` (manual trigger).

---

## 2. Catatan Transparansi Status Runner GitHub Actions pada Repositori Utama

Pada saat workflow dipicu di GitHub Actions pada repositori utama (`hakamavicena/IF3110_Tubes_WBD_K03_13`), GitHub Hosted Runner tidak dapat memulai VM kontainer dengan pesan sistem dari GitHub:
> `The job was not started because your account is locked due to a billing issue.`

Kendala ini murni disebabkan oleh status *billing lock* pada akun GitHub personal pemilik repositori utama (sehingga seluruh GitHub Actions hosted runner pada akun tersebut ditahan oleh GitHub), bukan karena kesalahan sintaks atau kegagalan kompilasi pada [`.github/workflows/codeql.yml`](../.github/workflows/codeql.yml).

Untuk memastikan aspek keamanan SAST tetap dievaluasi secara menyeluruh dan dapat dipertanggungjawabkan secara jujur saat demo:
1. Konfigurasi [`.github/workflows/codeql.yml`](../.github/workflows/codeql.yml) tetap disertakan secara lengkap dan valid di repositori.
2. Tim melakukan pemeriksaan statis kode secara langsung menggunakan aturan *Taint Tracking* (*Source → Sanitizer → Sink*) dari katalog rule resmi CodeQL untuk Go dan TypeScript/React, serta mendokumentasikan setiap titik rawan beserta mitigasinya pada Tabel Bagian 3 di bawah ini.

---

## 3. Evaluasi Titik Rawan & Mitigasi Berdasarkan Katalog Rule CodeQL

Tabel berikut memetakan titik-titik kode pada `src/backend` dan `src/frontend` yang menjadi sasaran query utama CodeQL (`security-extended`), lengkap dengan analisis dampak dan keputusan penanganannya:

| No | Rule ID CodeQL | Severity | Lokasi File di Proyek | Analisis Alur Data (*Source → Sink*) & Potensi Dampak | Keputusan & Mitigasi pada Kode |
| :- | :--- | :--- | :--- | :--- | :--- |
| 1 | `go/sql-injection` | High | `src/backend/dbengine/pkg/dbviewer/service.go`<br>`src/backend/dbengine/pkg/migration/batch_executor.go` | **Source**: Parameter path/input HTTP (`schema`, `table`, `batch_column`, `last_cursor`).<br>**Sink**: Pembentukan query SQL dinamis (`SELECT * FROM ...`, `WHERE <batch_column> >= $1`).<br>**Dampak**: Jika identifier tabel/kolom digabung langsung dengan string concatenation tanpa sanitasi, penyerang dapat menyisipkan perintah SQL berbahaya. | **Mitigated (Diperbaiki di Kode)**:<br>1. Seluruh identifier tabel dan schema pada `dbviewer` maupun `batch_executor` disanitasi menggunakan **`pgx.Identifier.Sanitize()`** dan divalidasi dengan regex whitelist ketat `^[a-zA-Z_][a-zA-Z0-9_]*$`.<br>2. Seluruh nilai batas cursor (`start_cursor`, `end_cursor`) dan pagination dikirim sebagai *parameterized query* (`$1`, `$2`). |
| 2 | `go/sql-injection` (Intentional Raw Migration SQL) | High | `src/backend/dbengine/pkg/migration/executor.go` | **Source**: Field `sql` dan `sql_down` yang diinput pengguna pada pembuatan migration.<br>**Sink**: `tx.Exec(ctx, sql, pgx.QueryExecModeSimpleProtocol)`.<br>**Dampak**: Eksekusi DDL/DML arbitrer pada database target. | **Accepted by Design (Dengan 4 Lapis Kontrol)**:<br>Menjalankan SQL migration pada database target adalah fungsi bisnis utama platform. Risiko dikendalikan melalui:<br>1. Otorisasi RBAC `CanEdit` (`access_level = 2`).<br>2. Validasi `ValidateInput` (`validate.go`) yang memblokir perintah kontrol transaksi (`COMMIT`, `ROLLBACK`, `BEGIN`, `SAVEPOINT`).<br>3. Kewajiban **Multi-Group Leader Approval** sebelum job dapat di-enqueue.<br>4. Pembatasan `SET LOCAL statement_timeout` & `lock_timeout = '5s'`. |
| 3 | `go/clear-text-logging` | High | `src/backend/dbengine/pkg/telemetry/logger.go`<br>`src/backend/dbengine/pkg/telemetry/redact.go`<br>`src/backend/dbengine/pkg/telemetry/sqllog.go` | **Source**: Objek request, DSN database target, header cookie/authorization, dan parameter query SQL.<br>**Sink**: Penulisan structured log JSON (`slog.NewJSONHandler(os.Stdout, ...)`).<br>**Dampak**: Kebocoran password database target atau `session_id` aktif ke output log. | **Mitigated (Diperbaiki di Kode)**:<br>Handler `slog` dilengkapi fungsi `replaceAttr` (`redact.go`) yang secara otomatis menyamarkan (`[REDACTED]`) setiap atribut dengan kunci sensitif (`password`, `secret`, `token`, `session_id`, `cookie`, `authorization`) serta menyamarkan pola `postgres://user:pass@` dan `PASSWORD '...'` di dalam string log maupun query SQL. |
| 4 | `go/request-forgery` (SSRF / Dynamic TCP Dial) | Medium | `src/backend/dbengine/pkg/dbtarget/manager.go` | **Source**: Input `host` dan `port` saat registrasi database target.<br>**Sink**: Pembukaan koneksi TCP PostgreSQL (`pgxpool.New`).<br>**Dampak**: Server dapat diarahkan untuk membuka koneksi ke layanan internal jika endpoint registrasi dapat diakses user biasa. | **Accepted by Design (Mitigated)**:<br>Endpoint `/api/admin/targets` dibatasi secara ketat hanya untuk **Platform Admin** (`RequireAdmin` middleware) + validasi `CSRFMiddleware`, serta password koneksi disimpan terenkripsi di database menggunakan **AES-256-GCM** (`SecretBox`). |
| 5 | `js/xss` | Medium | `src/frontend/components/table-inspector.tsx`<br>`src/frontend/app/(app)/databases/[dbId]/migrations/` | **Source**: Isi sel data dari tabel database target, pesan error `SQLSTATE`, dan script SQL.<br>**Sink**: Rendering DOM pada browser.<br>**Dampak**: Stored XSS jika data tabel mengandung payload `<script>` dan dirender sebagai raw HTML. | **Mitigated (Aman di Kode)**:<br>Seluruh halaman Next.js SPA merender data tabel, nama kolom, script SQL, dan pesan error murni melalui ekspresi JSX React standar (otomatis di-*escape* oleh React) dan tidak ada penggunaan `dangerouslySetInnerHTML`. |

---

## 4. Cakupan Kode yang Dianalisis
- **Backend Go (`src/backend/...`)**:
  - `auth/` (Session, Cookie `HttpOnly`, CSRF Constant-Time Compare, CORS)
  - `accesscontrol/` (RBAC, Admin Handler, Group Leader & Multi-Group Approval)
  - `dbengine/pkg/dbtarget/` (AES-256-GCM `SecretBox` & Pool Manager)
  - `dbengine/pkg/dbviewer/` (Read-Only Introspection & Paginated Table Data)
  - `dbengine/pkg/migration/` (SQL Validation, Distributed Lock, Batch Cursor Executor, Atomic Checkpoints, Worker Coordinator)
  - `dbengine/pkg/queue/` (Redis RESP Queue & MemoryQueue)
  - `dbengine/pkg/telemetry/` (Structured JSON Logger + Credential Redaction, OpenTelemetry Tracing, Prometheus Metrics)
- **Frontend Next.js SPA (`src/frontend/...`)**:
  - Seluruh komponen dan halaman di bawah `src/frontend/app/` dan `src/frontend/components/`.
