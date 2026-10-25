# Database Engine (Bagian B)

Implementasi Go untuk pengelolaan koneksi PostgreSQL target, viewer read-only,
dan riwayat migration berada di [`src/backend/dbengine`](./src/backend/dbengine).
Modul A (autentikasi/session/CSRF), modul C (pengelolaan target dan access
control), serta frontend utama belum ada di scaffold ini. Bagian B menyediakan
interface agar implementasi A/C dapat dipasang tanpa memberikan izin implisit.

## Fitur

- Pool PostgreSQL lazy per target, ping saat dibuat, invalidasi pool, dan
  klasifikasi kegagalan koneksi.
- Password target dienkripsi dengan AES-256-GCM (`nonce || ciphertext`).
- Viewer schema/table/column/constraint/index dan data berhalaman melalui
  transaksi `READ ONLY`, `statement_timeout`, serta quoting identifier PostgreSQL.
- Migration disimpan di database internal: nomor versi unik, claim serial per
  database, urutan linear, eksekusi transaksi penuh, status hasil, dan recovery
  migration yang tertinggal `running`.
- Handler Go `net/http` memakai `html/template`; seluruh route perlu middleware
  autentikasi dan CSRF dari aplikasi pemanggil.

## Kontrak integrasi A/C

Implementasikan `ports.Authorizer` di modul C dan panggil `CanView`,
`CanEdit`, serta `AccessibleDatabases`; tidak ada bypass Platform Admin.
Middleware A harus mengisi user ID melalui `httpdb.WithUserID` atau adaptor
konteks ekuivalen dan memvalidasi token CSRF pada semua POST. Berikan fungsi
token CSRF kepada `httpdb.NewHandler` (field form bernama `csrf_token`).

Modul C perlu menyediakan tabel `database_targets` sesuai kolom di
[`001_migrations.sql`](./src/backend/dbengine/migrations_internal/001_migrations.sql),
mengenkripsi password dengan `dbtarget.SecretBox`, menguji koneksi lewat
`Manager.TestConnection`, serta memanggil `Manager.Invalidate` setelah target
diubah/dihapus. Modul A harus menyediakan tabel `users` dengan kolom `id` dan
`username` sebelum DDL migration dipasang; tabel `migrations` mereferensikan
`users` tersebut.

`cmd/server` sengaja hanya berjalan dalam `DEV_MODE=true`, menggunakan user ID
fiktif 1 dan `AllowAll`, serta menolak bind selain IP loopback. Jangan gunakan
mode tersebut di deployment. Server normal harus dirakit oleh aplikasi utama
dengan authentikasi, CSRF, dan authorizer A/C yang sesungguhnya.

## Menjalankan untuk pengembangan

1. Jalankan PostgreSQL internal: `docker compose up -d pg` (port host 5433;
   kredensial lokal `app/app`, database `platform`).
2. Siapkan skema `users` dari modul A, lalu jalankan
   [`001_migrations.sql`](./src/backend/dbengine/migrations_internal/001_migrations.sql)
   terhadap database internal. Tambahkan target melalui modul C atau seed lokal
   terenkripsi menggunakan `SecretBox`.
3. Dari `src/backend/dbengine`, set environment:

   ```powershell
   $env:INTERNAL_DB_URL = "postgres://app:app@localhost:5433/platform?sslmode=disable"
   $env:SECRET_KEY = "<base64 dari 32 byte acak>"
   $env:DEV_MODE = "true"
   $env:MIGRATION_TIMEOUT = "30s"
   go run ./cmd/server
   ```

   Buat key dengan `openssl rand -base64 32`. Server development tersedia di
   `http://127.0.0.1:8080`.

4. Tes unit: `go test ./...`. Integration tests untuk transaction commit/rollback:

   ```powershell
   $env:TEST_PG_ADMIN_URL = "postgres://app:app@localhost:5433/postgres?sslmode=disable"
   go test -tags=integration ./...
   ```

   Integration test membuat dan menghapus database sementara; akun test perlu
   hak `CREATEDB`. Tanpa `TEST_PG_ADMIN_URL`, integration tests dilewati.

## Keputusan desain dan batasan

- Akses efektif multi-group ditentukan C (usulan: hak tertinggi menang);
  parameter sesi idle/absolute expiry, rotasi, dan grace period ditentukan A.
- Riwayat migration disimpan internal. Versi ditetapkan saat create dengan
  advisory lock. Versi N hanya dapat dieksekusi jika seluruh versi lebih rendah
  sukses; satu migration berjalan per database.
- Draft/failed dapat diedit. Migration sukses immutable. SQL gagal di-rollback
  lalu disimpan sebagai `failed`; dapat diedit dan dicoba ulang. `executed_at`
  menunjukkan waktu claim.
- Migration dijalankan dalam satu transaction; `CREATE INDEX CONCURRENTLY`,
  `VACUUM`, dan `CREATE DATABASE` tidak dapat dijalankan dalam transaction dan
  akan gagal.
- Pembatasan `COMMIT`/`ROLLBACK` oleh validator bersifat best-effort, bukan SQL
  parser. Jangan izinkan pengguna tidak tepercaya mengirim migration SQL.
- `Manager` memegang mutex global selama lookup/dekripsi/ping koneksi baru;
  membuat pool untuk target lain dapat menunggu sampai 8 detik.
- `count(*)` dapat mahal pada tabel besar. Data tabel dibatasi 200 baris per
  halaman dan hasil query viewer memiliki timeout lima detik.
- `RecoverStuck` menandai semua migration `running` sebagai failed saat startup;
  deployment multi-instance perlu koordinasi leader sebelum memanggilnya.