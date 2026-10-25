# Panduan Pengaturan dan Menjalankan Sistem (SETUP.md)

Dokumen ini memuat panduan lengkap untuk mengatur, menjalankan, menguji, dan memverifikasi platform Database Management Platform (Milestone 1 IF3110).

---

## 1. Prasyarat Sistem

- **Go**: Minimal versi **1.25.0** (wajib, karena driver PostgreSQL `pgx/v5 v5.11.0` mensyaratkan Go >= 1.25).
- **Docker & Docker Compose**: Untuk menjalankan database internal PostgreSQL dan target database testing.
- **PowerShell & curl.exe**: Untuk menjalankan pengujian otomatis end-to-end (`scripts/smoke.ps1`).

---

## 2. Variabel Environment

Sistem membaca variabel environment dari file `src/backend/.env` atau environment OS. Buat salinan `.env` di folder `src/backend/.env`:

```env
# Koneksi Database Internal Platform
INTERNAL_DB_URL=postgres://app:app@localhost:5433/platform?sslmode=disable

# Enkripsi Kredensial Database Target (AES-256-GCM, 32 bytes base64)
# Contoh generate di PowerShell:
# [Convert]::ToBase64String((1..32 | ForEach-Object { Get-Random -Minimum 0 -Maximum 256 }))
SECRET_KEY=yvF3cQ37J6g1k9xL2nP4sR6uV8wX0yZ2aB4cD6eF8gA=

# Kredensial Bootstrap Admin (Wajib diisi jika database belum memiliki akun admin, min 8 char)
ADMIN_USERNAME=admin
ADMIN_PASSWORD=rahasia123

# Konfigurasi Server HTTP
LISTEN_ADDR=:8080
SECURE_COOKIE=false
MIGRATION_TIMEOUT=30s
```

### Daftar Variabel Environment

| Variabel | Deskripsi | Wajib? | Contoh Nilai |
| :--- | :--- | :--- | :--- |
| `INTERNAL_DB_URL` | URL PostgreSQL platform internal | Ya | `postgres://app:app@localhost:5433/platform?sslmode=disable` |
| `SECRET_KEY` | Kunci simetris 32 byte base64 untuk SecretBox AES-GCM | Ya | 32 byte base64 string |
| `ADMIN_USERNAME` | Username bootstrap platform admin | Ya (saat init) | `admin` |
| `ADMIN_PASSWORD` | Password bootstrap admin (min. 8 karakter) | Ya (saat init) | `rahasia123` |
| `LISTEN_ADDR` | Alamat port HTTP listener | Tidak (default `:8080`) | `:8080` |
| `SECURE_COOKIE` | Flag secure pada cookie sesi (`true` untuk HTTPS) | Tidak (default `false`) | `false` |
| `MIGRATION_TIMEOUT` | Timeout eksekusi statement migrasi DDL | Tidak (default `30s`) | `30s` |

---

## 3. Langkah Menjalankan dari Nol (Clean Setup)

### Langkah 3.1: Jalankan Database via Docker Compose
Di root repository, jalankan:
```bash
docker compose up -d
```
Perintah ini akan menjalankan dua container:
1. `pg` pada port `5433` (Database platform internal: `platform`, user `app`, password `app`).
2. `pg_target2` pada port `5434` (Database target pengujian kedua: `target2_db`, user `app`, password `targetpass`).

### Langkah 3.2: Terapkan Skema Platform Internal
Terapkan skema tabel platform internal ke database `platform` di container `pg`:
```powershell
Get-Content src/backend/auth/schema.sql | docker exec -i if3110_tubes_wbd_k03_13-pg-1 psql -U app -d platform
```

### Langkah 3.3: Jalankan Server Backend
**PENTING**: Server wajib dijalankan dari folder `src/backend`:
```powershell
cd src/backend
go run .
```
Saat pertama kali startup, server akan mendeteksi apakah admin sudah ada di database. Jika belum ada, server akan membuat user admin otomatis dari `ADMIN_USERNAME` dan `ADMIN_PASSWORD` yang didefinisikan pada `.env`.

Server kini siap diakses melalui peramban web di: `http://localhost:8080`.

---

## 4. Memuat Data Demo (DEMO ONLY)

> [!WARNING]
> Kredensial dan data demo berikut ditujukan **HANYA UNTUK PENGUJIAN DAN DEMONSTRASI (DEMO ONLY)**. Jangan gunakan password default ini pada lingkungan produksi.

### Langkah 4.1: Siapkan Database Target `demo_target`
Jalankan skrip pembentukan database target demo pada container `pg`:
```powershell
Get-Content scripts/demo_target.sql | docker exec -i if3110_tubes_wbd_k03_13-pg-1 psql -U app -d postgres
```
Skrip ini menyiapkan:
- Tabel `customers` dengan 35 baris data (> 30 baris), constraint PK, UNIQUE `email`, CHECK `age >= 18`, serta minimal satu nilai `NULL` (pada kolom `phone`).
- Tabel `orders` dengan foreign key ke `customers` dan index.
- Tabel dengan nama khusus berjarak spasi: `"Odd Table With Space"`.

### Langkah 4.2: Masukkan Pengguna & Grup Demo
Terapkan skrip pengguna dan grup demo ke database platform:
```powershell
Get-Content scripts/demo_users.sql | docker exec -i if3110_tubes_wbd_k03_13-pg-1 psql -U app -d platform
```
Daftar Akun Pengguna Demo (Password: `password123`):
- `admin` (Platform Admin, dibuat bootstrap dengan password dari env)
- `editor` (Anggota grup `g_edit`)
- `viewer` (Anggota grup `g_view`)
- `multi` (Anggota grup `g_view` dan `g_edit`)
- `outsider` (Pengguna aktif tanpa grup)

### Langkah 4.3: Daftarkan Koneksi dan Hak Akses Melalui Admin UI
1. Login sebagai `admin` di `http://localhost:8080`.
2. Buka menu **Admin** (`http://localhost:8080/admin`).
3. Pada bagian **3. Koneksi Database Target**, daftarkan koneksi baru:
   - Nama Display: `demo_target`
   - Host: `localhost` (atau `pg` bila internal network)
   - Port: `5433`
   - Nama Database: `demo_target`
   - Username Database: `app`
   - Password: `app`
   - SSL Mode: `disable`
4. Pada bagian **2. Manajemen Grup & Hak Akses**, atur hak akses:
   - Pilih Grup `g_view` -> Database `demo_target` -> Level: `View Only (Level 1)`.
   - Pilih Grup `g_edit` -> Database `demo_target` -> Level: `View + Edit (Level 2)`.

---

## 5. Menjalankan Pengujian

### 5.1 Unit Tests (Semua Paket)
```powershell
cd src/backend
go test -count=1 ./...
```

### 5.2 Unit Tests dengan Acak (Shuffle)
```powershell
cd src/backend
go test -count=1 -shuffle=on ./...
```

### 5.3 Integration Tests
Jalankan pengujian integrasi database nyata:
```powershell
cd src/backend
$env:TEST_PG_ADMIN_URL="postgres://app:app@localhost:5433/platform?sslmode=disable"
go test -v -count=1 -tags=integration ./...
```

### 5.4 Pengujian Race Condition via Docker Container
Jalankan pengujian deteksi data race dalam lingkungan Linux container:
```powershell
docker run --rm -v "${PWD}/src/backend:/app" -w /app golang:latest go test -race -count=1 ./...
```

### 5.5 Smoke Test End-to-End Otomatis (10 Skenario Kunci)
Pastikan server sedang berjalan di `http://localhost:8080`, lalu jalankan skrip smoke test:
```powershell
powershell -ExecutionPolicy Bypass -File scripts/smoke.ps1
```
Skrip ini memverifikasi secara langsung:
1. Akses tanpa cookie -> 401
2. Outsider ditolak -> 403
3. Admin tanpa grup ditolak -> 403
4. Viewer: GET 200, POST migrasi -> 403
5. Editor: migrasi sukses, versi loncat -> 409, atomisitas rollback saat gagal
6. Multi dengan dua grup -> level akses tertinggi menang (level 2)
7. IDOR pencegahan -> 404
8. POST tanpa token CSRF -> 403
9. Logout lalu replay cookie lama -> 401
10. Admin menonaktifkan user -> session lama langsung hangus 401

---

## 6. Struktur Direktori Utama

```
IF3110_Tubes_WBD_K03_13/
├── docker-compose.yml              # Konfigurasi PostgreSQL platform & target test
├── docs/
│   └── SETUP.md                   # Dokumentasi instalasi, konfigurasi, dan pengujian
├── scripts/
│   ├── demo_target.sql            # Skema dan data dummy database demo target
│   ├── demo_users.sql             # User, grup, dan keanggotaan demo
│   └── smoke.ps1                  # Otomatisasi pengujian 10 skenario end-to-end
└── src/
    ├── frontend/
    │   └── templates/             # Template server-side HTML (admin, dashboard, login)
    └── backend/
        ├── auth/                  # Modul A: Autentikasi, Sesi, CSRF, Password hashing
        ├── accesscontrol/         # Modul C: RBAC, Manajemen Grup, Platform Admin CRUD
        ├── dbengine/              # Modul B: Viewer skema/data, Migrator DDL, Pool manager
        ├── go.mod                 # Dependensi Go modul tunggal (Go >= 1.25.0)
        └── main.go                # Inisialisasi platform, bootstrap admin, dan router
```

---

## 7. Ringkasan Endpoint / Rute Sistem

| Path | Metode | Hak Akses | Keterangan |
| :--- | :--- | :--- | :--- |
| `/` | `GET` | Publik | Tampilan login |
| `/login` | `POST` | Publik | Autentikasi pengguna, menerbitkan sesi & token CSRF |
| `/logout` | `POST` | Terautentikasi | Mengakhiri sesi, memerlukan CSRF valid |
| `/dashboard` | `GET` | Terautentikasi | Halaman utama daftar database yang dapat diakses |
| `/admin` | `GET` | Platform Admin | Dashboard manajemen pengguna, grup, dan koneksi |
| `/admin/users` | `POST` | Platform Admin | Tambah pengguna baru (CSRF) |
| `/admin/users/status` | `POST` | Platform Admin | Aktifkan / nonaktifkan pengguna (CSRF) |
| `/admin/groups` | `POST` | Platform Admin | Buat grup baru (CSRF) |
| `/admin/users/groups` | `POST` | Platform Admin | Tambahkan pengguna ke dalam grup (CSRF) |
| `/admin/users/groups/remove` | `POST` | Platform Admin | Keluarkan pengguna dari grup (CSRF) |
| `/admin/groups/access` | `POST` | Platform Admin | Atur hak akses grup ke database target (CSRF) |
| `/admin/databases` | `POST` | Platform Admin | Daftarkan koneksi database target baru (CSRF) |
| `/admin/databases/delete` | `POST` | Platform Admin | Hapus koneksi database & invalidate pool (CSRF) |
| `/databases` | `GET` | Terautentikasi | Daftar ringkas database |
| `/databases/{id}` | `GET` | Terautentikasi (View) | Schema viewer database target |
| `/databases/{id}/schemas/{s}/tables` | `GET` | Terautentikasi (View) | Daftar tabel dalam skema |
| `/databases/{id}/schemas/{s}/tables/{t}` | `GET` | Terautentikasi (View) | Detail struktur kolom, tipe, constraint tabel |
| `/databases/{id}/schemas/{s}/tables/{t}/data` | `GET` | Terautentikasi (View) | Viewer isi data tabel dengan pagination |
| `/databases/{id}/migrations` | `GET` | Terautentikasi (View) | Riwayat migrasi DDL pada target |
| `/databases/{id}/migrations/new` | `GET` | Terautentikasi (Edit) | Form draft migrasi baru |
| `/databases/{id}/migrations` | `POST` | Terautentikasi (Edit) | Simpan migrasi DDL baru (CSRF) |
| `/databases/{id}/migrations/{m_id}` | `GET` | Terautentikasi (View) | Detail status & SQL migrasi |
| `/databases/{id}/migrations/{m_id}/execute` | `POST` | Terautentikasi (Edit) | Eksekusi migrasi DDL (CSRF) |

---

## 8. Batasan yang Diketahui (Known Limitations)

1. **Ubah Koneksi (Edit Connection)**: Sesuai spesifikasi time-box batch akhir, operasi ubah data koneksi database dilewati. Pengguna dapat menghapus koneksi lama (yang otomatis membersihkan pool target via `Manager.Invalidate`) dan mendaftarkannya kembali.
2. **Kesesuaian SECRET_KEY**: Kredensial database target dienkripsi menggunakan AES-256-GCM dengan `SECRET_KEY`. Jika server dijalankan terhadap database yang sudah memiliki koneksi target terdaftar, nilai `SECRET_KEY` pada `.env` harus sama persis dengan kunci saat target didaftarkan; jika kunci berbeda, dekripsi password target akan ditolak dengan error otentikasi cipher dan koneksi harus didaftarkan ulang.
3. **Direktori Eksekusi Server**: Aplikasi mengandalkan penelusuran template dari root relatif `src/backend`. Menjalankan aplikasi harus dilakukan dengan `cd src/backend && go run .`.
4. **Koneksi Database SSL**: Driver mendukung mode SSL `disable`, `allow`, `prefer`, dan `require`. Pengujian lokal menggunakan `disable` atau `prefer`.
