# IF3110-Pengembangan-Aplikasi-Web-K01-16

RuangDB menggunakan Go untuk server aplikasi, PostgreSQL untuk database platform, serta PostgreSQL dan SQLite sebagai database target.

## Menjalankan aplikasi di Windows

Prasyarat: Go sesuai `go.mod`, PowerShell, dan Docker Desktop dengan Linux engine aktif. Jalankan dari folder repository:

```powershell
.\scripts\setup-local.ps1
. .\scripts\load-env.ps1
go run ./cmd/server
```

Skrip setup menjalankan PostgreSQL melalui Docker Compose, menyiapkan schema platform, membuat admin pertama, dan menambahkan data demo. Server Go berjalan langsung pada komputer. Buka `http://localhost:8080/login`; sesuaikan alamat jika `APP_ADDR` diubah.

Username dan password admin terdapat pada `ADMIN_USERNAME` dan `ADMIN_PASSWORD` di `.env`. Akun `demo-editor` dan `demo-viewer` menggunakan `DEMO_PASSWORD`. Setup membuat password acak dan kunci enkripsi ketika nilainya masih kosong. Nilai yang sudah tersedia dipertahankan, dan password admin yang sudah ada tidak diganti.

Pada terminal baru, cukup load konfigurasi lalu jalankan server:

```powershell
. .\scripts\load-env.ps1
go run ./cmd/server
```

Go tidak membaca `.env` secara otomatis. File `.env` berisi konfigurasi lokal dan rahasia; file tersebut diabaikan Git. `.env.example` adalah contoh konfigurasi yang boleh dibagikan.

## Program pada folder cmd

| Program | Fungsi |
| --- | --- |
| `go run ./cmd/setup` | Menerapkan schema platform dan membuat akun admin pertama, lalu berhenti. |
| `go run ./cmd/demo` | Menambahkan akun, group, grant, koneksi, dan data contoh, lalu berhenti. |
| `go run ./cmd/server` | Menjalankan server HTTP untuk website dan API sampai dihentikan. |

Skrip `setup-local.ps1` menjalankan program setup dan demo secara berurutan. Ketiga program tersebut tidak perlu dijalankan bersamaan.

## Konfigurasi lokal

Compose menyediakan PostgreSQL pada `127.0.0.1:54329`. Database `platform` menyimpan metadata aplikasi; `sales` dan `hr` merupakan target demo. SQLite demo disimpan di dalam `DATA_DIR`, dengan nilai bawaan `.local/data`.

`TARGET_CREDENTIAL_KEY` adalah base64 dari 32 byte acak untuk enkripsi password koneksi target. Pertahankan kunci yang sama selama menggunakan registry yang sama agar credential lama tetap dapat dibaca.

Setup dapat diulang tanpa menghapus data yang sudah tersedia. Fixture PostgreSQL pada `fixtures/postgres/init.sql` dijalankan ketika volume Compose pertama kali dibuat. Hindari `docker compose down -v` bila data lokal masih diperlukan.

## CodeQL SAST

CodeQL dikonfigurasi melalui [`.github/workflows/codeql.yml`](.github/workflows/codeql.yml) untuk menganalisis kode Go dan JavaScript/TypeScript. Workflow berjalan pada pull request menuju `main`, push ke `main`

### History CodeQL Scan

| Tanggal (WIB) | Action | Branch/commit | Hasil | Bukti |
| --- | --- | --- | --- | --- |
| 22 Nov 2026 | Pull request | `feat/draft_migration` / `fd32a17` | Success | [Run 45003876597](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45003876597) |
| 22 Nov 2026 | Push ke `main` | `1b8bace` | Success | [Run 45004325525](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45004325525) |
| 22 Nov 2026 | Pull request | `feat/spa-frontend` / `6d0d6ee` | Success | [Run 45024465923](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45024465923) |
| 22 Nov 2026 | Push ke `main` | `ce34a85` | Success | [Run 45024558568](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45024558568) |
| 22 Nov 2026 | Pull request | `feat/worker` / `55ea3c7` | Success | [Run 45028920628](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45028920628) |
| 22 Nov 2026 | Pull request | `feat/worker` / `9dc152c` | Success | [Run 45031008601](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45031008601) |
| 22 Nov 2026 | Push ke `main` | `a3dd1a1` | Success | [Run 45031021359](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45031021359) |
| 22 Nov 2026 | Pull request | `feat/spa-frontend` / `f9725e3` | Success | [Run 45035985409](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45035985409) |
| 22 Nov 2026 | Push ke `main` | `c1c97f2` | Success | [Run 45036173107](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45036173107) |
| 23 Nov 2026 | Pull request | `feat/spa-frontend` / `f3ff65b` | Success | [Run 45060367748](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45060367748) |
| 23 Nov 2026 | Pull request | `feat/spa-frontend` / `f3629e6` | Success | [Run 45061001306](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45061001306) |
| 23 Nov 2026 | Pull request | `feat/spa-frontend` / `24c30b5` | Success | [Run 45062716805](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45062716805) |
| 23 Nov 2026 | Push ke `main` | `6b07bf3` | Success | [Run 45063104964](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45063104964) |
| 23 Nov 2026 | Pull request | `fix/draft_migration` / `27a8b62` | Success | [Run 45102403746](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45102403746) |
| 23 Nov 2026 | Push ke `main` | `4d7dd55` | Success | [Run 45105977800](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/runs/45105977800) |

History ada di [Actions CodeQL](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/actions/workflows/codeql.yml)

### CodeQL Scanning Alerts

| Alert | Lokasi | Potensi dampak | Perlu diperbaiki? | Alasan |
| --- | --- | --- | --- | --- |
| [#7](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/security/code-scanning/7) | `internal/targetdb/statement.go:11` | SQL buatan pengguna mencapai PostgreSQL dan dapat mengubah data/schema target. | Tidak | False positive. SQL telah melewati parser AST PostgreSQL, allowlist statement dan fungsi, permission, approval lintas group, snapshot immutable, revalidation worker, serta transaksi sebelum dieksekusi. |
| [#6](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/security/code-scanning/6) | `internal/targetdb/migration.go:119` | SQL dinamis pada jalur SQLite dapat menjalankan perubahan yang tidak diinginkan jika validator dapat dilewati. | Tidak | False positive. SQL melewati `sqlguard.Migration`, allowlist operasi, pembatasan jumlah statement, pemblokiran operasi administratif, serta transaksi yang dibatalkan jika satu statement gagal. Test injection memastikan input berbahaya ditolak. |
| [#4](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/security/code-scanning/4) | `internal/targetdb/plan.go:67` | Input pengguna digabungkan ke `EXPLAIN (FORMAT JSON)` PostgreSQL. | Tidak | False positive. Input melewati `sqlguard.Plan`, hanya menerima satu statement yang diizinkan, hanya digunakan untuk `EXPLAIN`, dan transaksi selalu di-rollback. |
| [#3](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/security/code-scanning/3) | `internal/targetdb/plan.go:40` | Input pengguna digabungkan ke `EXPLAIN QUERY PLAN` SQLite. | Tidak | False positive. Input melewati validasi satu statement, hanya digunakan untuk execution plan, dan transaksi tidak pernah di-commit. |
| [#1](https://github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/security/code-scanning/1) | `internal/targetdb/rows.go:52` | Nama schema atau tabel dari request dapat memengaruhi query pembacaan data. | Tidak | False positive. Tabel harus ada pada snapshot, identifier di-escape oleh `Quote`, sedangkan `LIMIT` dan `OFFSET` dikirim sebagai parameter. Test injection memastikan nama tabel berbahaya ditolak. |
