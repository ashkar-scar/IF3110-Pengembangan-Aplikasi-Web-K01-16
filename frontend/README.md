# DBManager Frontend (SPA)

Antarmuka Single-Page Application untuk DBManager menggunakan React, TypeScript, dan Vite.

## Pengembangan Lokal

1. Jalankan backend Go terlebih dahulu di port bawaan `:8080` (atau set `VITE_API_TARGET` jika memakai port lain).
2. Masuk ke direktori `frontend/` dan jalankan dev server:

```powershell
npm install
npm run dev
```

Dev server berjalan di `http://localhost:5173` dan meneruskan request `/api/*` ke backend Go agar cookie session dan header `X-CSRF-Token` tetap berada pada origin yang sama.

## Build dan Lint

```powershell
npm run build
npm run lint
```

Hasil build produksi ditulis ke `../web/dist`.
