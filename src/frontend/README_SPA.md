# Struktur Frontend SPA Milestone 2 (Next.js + React + TailwindCSS)

Folder `src/frontend` digunakan untuk mengimplementasikan Single-Page Application (SPA) yang responsif (desktop & mobile). Setiap anggota tim mengerjakan tepat **3 modul UI**:

## Pembagian Modul Halaman per Anggota (3 : 3 : 3)

1. **Orang 1 (3 Modul UI — Auth, Admin, & Approval)**:
   - **Modul 1**: Inisialisasi project Next.js + TailwindCSS, API client (`credentials: 'include'` + header `X-CSRF-Token`), Layout Navigasi Responsif, & Halaman Login (`/login`).
   - **Modul 2**: Halaman Admin Panel (`/admin`: Users, Groups + Penetapan Group Leader, Target Databases, Group Access).
   - **Modul 3**: Komponen & Inbox Approval Group Leader (daftar group yang wajib approve, status tiap leader, tombol *Submit Review* & *Approve*).
2. **Orang 2 (3 Modul UI — Dashboard, Database Viewer, & History)**:
   - **Modul 1**: Halaman Dashboard Pilih Database & Explorer Schema/Table (`/dashboard`, `/databases/[dbId]/viewer`).
   - **Modul 2**: Halaman Detail Tabel (Columns, Constraints, Indexes) & Paginated Data Viewer (dengan horizontal scroll internal).
   - **Modul 3**: Halaman Migration History (`/databases/[dbId]/history`: daftar `[E1..En]`, checkbox **Hide Rollback**, serta pengajuan Rollback & Forward).
3. **Orang 3 (3 Modul UI — Migration List/Detail, Form Editor, & Job Monitor)**:
   - **Modul 1**: Halaman Daftar & Detail Migration (`/databases/[dbId]/migrations`, `/databases/[dbId]/migrations/[migId]`).
   - **Modul 2**: Halaman Form Create/Edit Migration (SQL Up, SQL Down / Irreversible, mode Biasa vs Per Batch `batch_column` & `batch_size`).
   - **Modul 3**: Komponen Real-Time Job & Batch Progress Monitor (progres batch, jumlah row committed, Retry dari Checkpoint, & Cancel/Revert Batch).
