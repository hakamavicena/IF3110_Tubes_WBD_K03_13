# Tugas Besar IF3110 Pengembangan Aplikasi Web K03-13

## Anggota Kelompok
- 13524009 - Mikhael Benrael Tampubolon
- 13524075 - Hakam Avicena Mustain
- 13524087 - Muhammad Fakhry Zaki

Database Management Platform adalah aplikasi web untuk mengelola dan melihat
database PostgreSQL melalui satu platform. Aplikasi ini dibuat menggunakan Go,
PostgreSQL, dan template HTML server-side.

## Fitur Utama

- Login, logout, session, dan perlindungan CSRF.
- Manajemen user dan status aktif/nonaktif.
- Manajemen group dan keanggotaan user.
- Role-based access control untuk setiap database target.
- Registrasi dan pengujian koneksi database target.
- Database viewer untuk schema, tabel, kolom, constraint, index, dan data.
- Tampilan data dengan pagination.
- Pembuatan, pengeditan, dan eksekusi migration SQL.
- Eksekusi migration secara berurutan dan transaksional.
- Enkripsi password database target menggunakan AES-256-GCM.
- Antarmuka web responsif menggunakan Bootstrap.

## Teknologi

- Go 1.26+
- PostgreSQL 16
- Docker Compose
- `net/http` dan `html/template`
- Bootstrap 5

## Struktur Direktori

```text
.
├── docker-compose.yml
├── docs/
│   └── SETUP.md
├── scripts/
│   ├── demo_target.sql
│   ├── demo_users.sql
│   └── smoke.ps1
└── src/
    ├── backend/
    │   ├── accesscontrol/
    │   ├── auth/
    │   ├── dbengine/
    │   ├── .env
    │   ├── go.mod
    │   └── main.go
    └── frontend/
        └── templates/
```

Panduan setup dan testing tersedia di
[`docs/SETUP.md`](./docs/SETUP.md).
