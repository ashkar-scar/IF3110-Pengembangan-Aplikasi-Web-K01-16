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

## Stress Testing (`k6`)

Skrip utama stress testing berada pada `test/k6/stress-migration-recovery.js` dan menguji sistem dari beban normal yang ditetapkan tim (`6 VUs`), dinaikkan bertahap melewati batas nyaman hingga beban puncak (`36 VUs`), lalu diturunkan kembali ke beban normal untuk mengamati pemulihan sistem.

### Ringkasan Skrip dan Skenario

1. **Dua Skenario Bersamaan (`api_access` & `migration_jobs`)**:
   - `api_access` (`ramping-vus`: `4 -> 12 -> 28 -> 4 VUs`) mengakses `GET /api/databases`, `GET /api/databases/{id}/schema`, `GET /api/databases/{id}/rows`, dan `GET /api/databases/{id}/jobs` secara bergilir ke target PostgreSQL maupun SQLite.
   - `migration_jobs` (`ramping-vus`: `2 -> 5 -> 8 -> 2 VUs`) menyiapkan request migration yang sudah berstatus `APPROVED` (termasuk persetujuan seluruh leader grup terkait dengan tag terpisah `{ kind: "prep" }`), mengirim job melalui `POST /api/databases/{id}/reviews/{reqID}/enqueue`, lalu memantau `GET /api/jobs/{jobID}` sampai mencapai status terminal (`SUCCESS`, `FAILED`, `PARTIAL`, `REVERTED`, atau `PARTIALLY_REVERTED`).
2. **Database Sama vs Berbeda**:
   - Iterasi genap (`same_db`) menargetkan database PostgreSQL yang sama secara bersamaan (`Demo Sales`, `target_id=1`) untuk menguji antrean target lock dan deteksi konflik versi (*optimistic concurrency control* / OCC).
   - Iterasi ganjil (`diff_db`) meratakan pengiriman ke beberapa database PostgreSQL berbeda (`DIFF_TARGET_IDS`) yang dieksekusi paralel oleh worker (`WORKER_CONCURRENCY` bawaan 4).
3. **Pemisahan Status Pengiriman vs Eksekusi**:
   - Respons `HTTP 202 Accepted` dari endpoint `enqueue` hanya dicatat sebagai `job_enqueue_accepted`, bukan keberhasilan migration.
   - Keberhasilan eksekusi (`job_exec_success`) hanya bernilai sukses apabila hasil polling job menunjukkan `status === "SUCCESS"`. Waktu tunggu queue dihitung dari `started_at - created_at` dan durasi eksekusi dari `finished_at - started_at`.
4. **Threshold `k6`**:
   - Latensi baca API pada fase normal (`api_read_duration{stage:1_normal}`): `p(95) < 500 ms`
   - Latensi baca API pada fase pemulihan (`api_read_duration{stage:5_recovery}`): `p(95) < 750 ms`
   - Latensi API global sepanjang uji (`api_read_duration` & `api_req_duration`): `p(95) < 2500 ms`
   - Error rate API (`api_error_rate`, `http_req_failed{kind:api_read}`, `http_req_failed{kind:api_enqueue}`): `rate < 0.01` (`< 1%`)
   - Kegagalan sistem / timeout worker non-OCC (`job_system_error`): `rate < 0.01` (`< 1%`)

Contoh eksekusi menggunakan Docker `grafana/k6`:

```powershell
. .\scripts\load-env.ps1
docker run --rm --add-host=host.docker.internal:host-gateway -v "${PWD}:/workspace" `
  -e BASE_URL=http://host.docker.internal:8080 `
  -e K6_USER="$env:ADMIN_USERNAME" -e K6_PASSWORD="$env:ADMIN_PASSWORD" `
  -e SECONDARY_USER="demo-viewer" -e SECONDARY_PASSWORD="$env:DEMO_PASSWORD" `
  -e SAME_TARGET_ID="1" -e DIFF_TARGET_IDS="2,9,10,11" -e API_TARGET_IDS="1,2,3" `
  grafana/k6:latest run /workspace/test/k6/stress-migration-recovery.js
```

### Hasil Pengujian per Tingkat Beban

Konfigurasi uji: `cmd/server` + `cmd/worker` (`WORKER_CONCURRENCY=4`), PostgreSQL target untuk `same_db` (`id=1`) dan 4 target PostgreSQL untuk `diff_db` (`id=2,9,10,11`), serta akses baca API pada `id=1,2,3` (PostgreSQL dan SQLite).

| Tingkat Beban | Rentang & VU (`API + Job`) | API `p50` | API `p95` | API `req/s` | API Error | Queue Wait `p50` / `p95` | Job Dur `p50` / `p95` | Job Selesai (`Job/s`) | Job Succ | OCC Fail | Sys Err |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `1_normal` (Normal) | `0-20s` (`4 + 2 = 6`) | `54.21 ms` | `134.07 ms` | `22.95` | `0.00%` | `242.00 / 562.20 ms` | `197.00 / 603.60 ms` | `35` (`1.75`) | `100.00%` | `0.00%` | `0.00%` |
| `2_stress` (Meningkat) | `20-45s` (`12 + 5 = 17`) | `350.42 ms` | `865.80 ms` | `24.12` | `0.00%` | `505.00 / 1006.90 ms` | `212.00 / 269.90 ms` | `39` (`1.56`) | `71.79%` | `28.21%` | `0.00%` |
| `3_peak` (Peak Stress) | `45-70s` (`28 + 8 = 36`) | `1558.91 ms` | `2433.15 ms` | `18.00` | `0.00%` | `775.00 / 1230.85 ms` | `183.50 / 207.75 ms` | `22` (`0.88`) | `63.64%` | `36.36%` | `0.00%` |
| `4_cooldown` (Ramp Down) | `70-78s` (`36 -> 6`) | `996.87 ms` | `2042.87 ms` | `8.88` | `0.00%` | `820.50 / 1063.45 ms` | `151.50 / 181.55 ms` | `4` (`0.50`) | `75.00%` | `25.00%` | `0.00%` |
| `5_recovery` (Pemulihan) | `78-103s` (`4 + 2 = 6`) | `208.08 ms` | `485.63 ms` | `11.76` | `0.00%` | `273.00 / 758.10 ms` | `172.50 / 199.75 ms` | `22` (`0.88`) | `100.00%` | `0.00%` | `0.00%` |

Perbandingan `same_db` vs `diff_db` pada setiap tingkat beban:

| Tingkat Beban | `same_db` Jobs | `same_db` Succ | `same_db` QWait (`p50 / p95`) | `diff_db` Jobs | `diff_db` Succ | `diff_db` QWait (`p50 / p95`) |
| --- | --- | --- | --- | --- | --- | --- |
| `1_normal` | `18` | `100.00%` | `258.50 / 562.50 ms` | `17` | `100.00%` | `242.00 / 487.20 ms` |
| `2_stress` | `19` | `42.11%` | `473.00 / 1006.90 ms` | `20` | `100.00%` | `534.00 / 940.50 ms` |
| `3_peak` | `11` | `27.27%` | `756.00 / 1229.50 ms` | `11` | `100.00%` | `795.00 / 1137.00 ms` |
| `4_cooldown` | `2` | `50.00%` | `635.50 / 672.85 ms` | `2` | `100.00%` | `1022.50 / 1075.15 ms` |
| `5_recovery` | `11` | `100.00%` | `233.00 / 701.00 ms` | `11` | `100.00%` | `369.00 / 638.50 ms` |

Ringkasan global: `2683` total HTTP request (`25.10 req/s`), `api_read` `p50 = 316.24 ms` / `p95 = 2072.17 ms`, `api_all` `p50 = 317.86 ms` / `p95 = 2089.86 ms`, `api_error_rate = 0.00%`, `job_enqueue_accepted = 100.00%`, `job_exec_success = 83.61%`, `job_system_error = 0.00%`, dan **seluruh 8 threshold k6 berstatus PASS**.

### Analisis per Tingkat Beban

1. **`1_normal` (`6 VUs`: `4 API + 2 Migration`)**: Sistem berada jauh di bawah batas kapasitas. API merespons sangat cepat (`p50 = 54.21 ms`, `p95 = 134.07 ms`) dengan `0.00%` error, serta seluruh `35` job (`same_db` maupun `diff_db`) selesai `100.00%` karena hampir tidak terjadi tumpang-tindih pengiriman ke `Demo Sales`.
2. **`2_stress` (`17 VUs`: `12 API + 5 Migration`, ~2.8x beban normal)**: Throughput API masih naik sedikit (`24.12 req/s`), namun `p95` melonjak menjadi `865.80 ms` dan waktu tunggu queue `p50` berlipat dua menjadi `505.00 ms`. Pada fase ini perbedaan arsitektur terlihat jelas: `diff_db` tetap `100.00%` sukses karena 4 worker mengeksekusi target berbeda secara paralel, sedangkan pada `same_db` hanya `42.11%` job yang sukses karena beberapa request berversi awal sama masuk ke antrean hampir bersamaan (semuanya mendapat `202 Accepted` saat `enqueue`, lalu yang kalah balapan ditolak worker dengan `versi database telah berubah`).
3. **`3_peak` (`36 VUs`: `28 API + 8 Migration`, 6x beban normal)**: Sistem mencapai titik saturasi. Throughput API turun menjadi `18.00 req/s` dan latensi naik ke `p50 = 1558.91 ms` / `p95 = 2433.15 ms`, sementara queue wait `p95` naik ke `1230.85 ms`. Penyebab utamanya adalah pembukaan koneksi target baru di setiap request Explorer (`targetdb.OpenPostgres` tanpa pool persisten antar-request), kueri izin N+1 pada `GET /api/databases`, serta serialisasi kunci per-target pada worker. Meski jenuh, API tidak mengembalikan error (`0.00%`) dan `diff_db` tetap `100.00%` sukses.
4. **`4_cooldown` (`8s` ramp down `36 -> 6 VUs`)**: Sisa request dan antrean dari fase puncak dikuras. Latensi `p95` mulai turun (`2433.15 ms -> 2042.87 ms`) dan `API error rate` tetap `0.00%`.
5. **`5_recovery` (`6 VUs`: `4 API + 2 Migration`)**: Setelah antrean terkuras, sistem pulih tanpa perlu restart: API `p95` turun drastis `80.0%` dari puncak menjadi `485.63 ms` (`p50 = 208.08 ms`, `api_read p95 = 497.48 ms`), `queue_wait p50` kembali normal ke `273.00 ms`, dan tingkat keberhasilan eksekusi migration kembali menjadi `100.00%` baik pada `same_db` maupun `diff_db`. Sedikit sisa selisih dibanding kondisi dingin (`134.07 ms -> 485.63 ms`) dipicu oleh overhead penulisan log SQL terstruktur dan ekspor trace/log OTLP pasca-lonjakan beban.

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
