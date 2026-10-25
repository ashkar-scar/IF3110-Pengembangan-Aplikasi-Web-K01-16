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
