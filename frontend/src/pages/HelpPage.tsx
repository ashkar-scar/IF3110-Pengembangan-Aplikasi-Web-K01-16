import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { useAuth } from "../context/useAuth";

type DocTopicId =
    | "overview"
    | "databases"
    | "explorer"
    | "diff"
    | "drafts"
    | "reviews"
    | "jobs"
    | "history"
    | "admin"
    | "roles";

interface DocTopic {
    id: DocTopicId;
    group: "Mulai Cepat" | "Perubahan Skema" | "Referensi";
    title: string;
    summary: string;
    keywords: string;
}

const TOPICS: DocTopic[] = [
    {
        id: "overview",
        group: "Mulai Cepat",
        title: "Cara Kerja DBManager",
        summary: "Alur singkat mulai dari memeriksa isi database hingga menjalankan perubahan skema.",
        keywords: "alur workflow mulai awal login workspace database",
    },
    {
        id: "databases",
        group: "Mulai Cepat",
        title: "Daftar Database",
        summary: "Melihat database yang dapat diakses akun Anda beserta tingkat izinnya.",
        keywords: "target driver view edit izin daftar database",
    },
    {
        id: "explorer",
        group: "Mulai Cepat",
        title: "Explorer (Struktur & Data)",
        summary: "Melihat daftar schema, kolom, aturan tabel, index, dan isi baris data.",
        keywords: "schema tabel kolom constraint index primary foreign key data baris halaman versi",
    },
    {
        id: "diff",
        group: "Perubahan Skema",
        title: "Schema Diff",
        summary: "Membandingkan dua database sejenis untuk membuat SQL perubahan otomatis.",
        keywords: "diff bandingkan referensi desired allow destructive drop",
    },
    {
        id: "drafts",
        group: "Perubahan Skema",
        title: "Drafts & Explain Plan",
        summary: "Menulis rencana perubahan SQL, mengecek estimasi query, lalu mengajukan review.",
        keywords: "draft sql up down irreversible batch normal explain plan kunci",
    },
    {
        id: "reviews",
        group: "Perubahan Skema",
        title: "Review & Approval",
        summary: "Persetujuan leader setiap grup sebelum perubahan dijalankan ke database.",
        keywords: "approve reject leader group catatan enqueue antrean setujui tolak",
    },
    {
        id: "jobs",
        group: "Perubahan Skema",
        title: "Jobs & Pemantauan",
        summary: "Memantau proses eksekusi yang sedang berjalan, mengulang job gagal, atau membatalkan batch.",
        keywords: "pending running success failed retry checkpoint cancel batch",
    },
    {
        id: "history",
        group: "Perubahan Skema",
        title: "History, Rollback & Forward",
        summary: "Melihat catatan perubahan yang sudah dijalankan serta mengajukan pembatalan berurutan.",
        keywords: "history riwayat rollback forward versi urutan",
    },
    {
        id: "admin",
        group: "Referensi",
        title: "Administrasi Platform",
        summary: "Panduan khusus admin untuk mengatur pengguna, grup, izin database, dan target.",
        keywords: "admin user group leader member grant target koneksi test postgres sqlite",
    },
    {
        id: "roles",
        group: "Referensi",
        title: "Hak Akses & Kendala Umum",
        summary: "Perbedaan hak tiap peran dan jawaban untuk kendala yang paling sering ditemui.",
        keywords: "izin view edit leader admin terkunci wallpaper kendala",
    },
];

const GROUPS: DocTopic["group"][] = ["Mulai Cepat", "Perubahan Skema", "Referensi"];

export function HelpPage() {
    const { user } = useAuth();
    const [activeId, setActiveId] = useState<DocTopicId>("overview");
    const [search, setSearch] = useState("");

    const filteredTopics = useMemo(() => {
        const q = search.trim().toLowerCase();
        if (!q) return TOPICS;
        return TOPICS.filter(
            (topic) =>
                topic.title.toLowerCase().includes(q) ||
                topic.summary.toLowerCase().includes(q) ||
                topic.keywords.includes(q),
        );
    }, [search]);

    const currentTopic = TOPICS.find((item) => item.id === activeId) ?? TOPICS[0];
    const currentIndex = TOPICS.findIndex((item) => item.id === currentTopic.id);
    const prevTopic = currentIndex > 0 ? TOPICS[currentIndex - 1] : null;
    const nextTopic = currentIndex < TOPICS.length - 1 ? TOPICS[currentIndex + 1] : null;

    function selectTopic(id: DocTopicId) {
        setActiveId(id);
        window.scrollTo({ top: 0, behavior: "smooth" });
    }

    const content: Record<DocTopicId, ReactNode> = {
        overview: (
            <>
                <p>
                    DBManager membantu tim melihat struktur database sekaligus mengelola perubahan tabel
                    secara aman. Anda dapat melihat isi tabel kapan saja di Explorer, sedangkan setiap
                    perubahan struktur harus dibuat sebagai draft dan disetujui terlebih dahulu sebelum
                    dijalankan.
                </p>

                <h2>Urutan penggunaan dari awal sampai selesai</h2>
                <ol className="docs-steps">
                    <li>
                        <strong>Pilih database</strong> pada halaman{" "}
                        <Link className="docs-link-chip" to="/databases">
                            Daftar Database
                        </Link>
                        , lalu klik tombol <strong>Buka Explorer</strong>.
                    </li>
                    <li>
                        <strong>Periksa tabel</strong> lewat tab <strong>Struktur</strong> atau{" "}
                        <strong>Data Baris</strong>. Bila ingin menyamakan struktur dengan database
                        referensi, buka menu <strong>Schema Diff</strong>.
                    </li>
                    <li>
                        <strong>Siapkan rancangan perubahan</strong> di menu <strong>Migrations</strong>{" "}
                        pada tab <strong>Drafts</strong>. Cek rencana query dengan{" "}
                        <strong>Explain Plan</strong>, lalu tekan <strong>Ajukan Review</strong>.
                    </li>
                    <li>
                        <strong>Tunggu persetujuan leader grup</strong> pada tab{" "}
                        <strong>Review &amp; Approval</strong>. Setelah status berubah menjadi{" "}
                        <strong>APPROVED</strong>, tekan{" "}
                        <strong>Jalankan via Antrean Worker</strong>.
                    </li>
                    <li>
                        <strong>Pantau hasilnya</strong> di tab <strong>Jobs</strong> selama proses
                        berjalan dan lihat arsipnya di tab <strong>History</strong>.
                    </li>
                </ol>

                <div className="docs-note">
                    <p className="docs-note-title">Mengapa perubahan harus lewat proses review?</p>
                    <p>
                        Agar tidak ada perintah SQL yang langsung mengubah data penting tanpa sepengetahuan
                        tim. Draft yang sudah diajukan otomatis dikunci sehingga perintah yang disetujui
                        sama persis dengan yang dijalankan.
                    </p>
                </div>

                <h2>Pintasan topik penting</h2>
                <div className="docs-jump">
                    <button type="button" className="btn-sm" onClick={() => selectTopic("explorer")}>
                        Cara Memakai Explorer
                    </button>
                    <button type="button" className="btn-sm" onClick={() => selectTopic("drafts")}>
                        Cara Membuat Draft SQL
                    </button>
                    <button type="button" className="btn-sm" onClick={() => selectTopic("roles")}>
                        Cek Perbedaan Hak Akses
                    </button>
                </div>
            </>
        ),

        databases: (
            <>
                <p>
                    Setelah masuk ke aplikasi, Anda akan langsung diarahkan ke halaman{" "}
                    <Link className="docs-link-chip" to="/databases">
                        Daftar Database
                    </Link>
                    . Halaman ini hanya menampilkan database yang diizinkan untuk grup akun Anda.
                </p>

                <h2>Arti informasi pada daftar database</h2>
                <dl className="docs-fields">
                    <div>
                        <dt>Driver</dt>
                        <dd>
                            Jenis sistem database yang digunakan, yaitu PostgreSQL (<code>postgres</code>)
                            atau SQLite (<code>sqlite</code>).
                        </dd>
                    </div>
                    <div>
                        <dt>Izin</dt>
                        <dd>
                            <strong>VIEW</strong> berarti Anda hanya bisa melihat struktur dan data tabel.{" "}
                            <strong>EDIT</strong> berarti Anda juga bisa membuat rancangan perubahan
                            (draft) dan mengajukannya ke tim.
                        </dd>
                    </div>
                </dl>

                <p>
                    Tekan tombol <strong>Buka Explorer</strong> pada salah satu baris untuk mulai
                    memeriksa isi database tersebut.
                </p>
            </>
        ),

        explorer: (
            <>
                <p>
                    Halaman Explorer dipakai untuk melihat susunan tabel dan contoh isi data secara
                    aman tanpa risiko mengubah isi database.
                </p>

                <h2>Bagian-bagian di halaman Explorer</h2>
                <ol className="docs-steps">
                    <li>
                        <strong>Baris info di bagian atas</strong> menampilkan <strong>Driver</strong>,{" "}
                        <strong>Izin</strong> Anda, dan <strong>Versi Skema</strong> saat ini (misalnya{" "}
                        <code>v0</code>, <code>v1</code>), beserta tombol pintas ke{" "}
                        <strong>Migrations</strong> dan <strong>Schema Diff</strong>.
                    </li>
                    <li>
                        <strong>Panel kiri</strong> dipakai untuk mengganti <strong>Schema</strong> dan
                        mencari nama tabel menggunakan kotak pencarian.
                    </li>
                    <li>
                        <strong>Tab Struktur</strong> menampilkan daftar kolom, tipe data, status boleh
                        kosong (<em>Nullable</em>), nilai bawaan, aturan relasi/unik (<em>Constraint</em>
                        ), dan <em>Index</em>.
                    </li>
                    <li>
                        <strong>Tab Data Baris</strong> menampilkan isi tabel per halaman. Gunakan tombol{" "}
                        <strong>Sebelumnya</strong> dan <strong>Berikutnya</strong> di bagian bawah tabel
                        untuk berpindah halaman data.
                    </li>
                </ol>

                <div className="docs-note">
                    <p className="docs-note-title">Struktur tabel belum berubah setelah migration selesai?</p>
                    <p>
                        Tekan tombol <strong>Muat Ulang</strong> di pojok kanan atas halaman Explorer untuk
                        mengambil susunan tabel terbaru.
                    </p>
                </div>
            </>
        ),

        diff: (
            <>
                <p>
                    Fitur <strong>Schema Diff</strong> membantu membandingkan database target dengan
                    database referensi yang jenisnya sama, lalu otomatis menyusun perintah SQL agar
                    keduanya selaras. Menu ini hanya muncul bagi pengguna berizin <strong>EDIT</strong>.
                </p>

                <h2>Langkah membandingkan skema</h2>
                <ol className="docs-steps">
                    <li>
                        Dari halaman Explorer database yang ingin diperbarui, klik tombol{" "}
                        <strong>Schema Diff</strong>.
                    </li>
                    <li>
                        Pilih <strong>Database Referensi (Desired)</strong> sebagai patokan bentuk tabel
                        yang diinginkan.
                    </li>
                    <li>
                        Isi nama <strong>Current Schema</strong> dan <strong>Desired Schema</strong>{" "}
                        (umumnya sudah terisi otomatis sebagai <code>public</code> atau <code>main</code>).
                    </li>
                    <li>
                        Biarkan kotak <strong>Allow Destructive</strong> tetap kosong jika Anda tidak
                        ingin menyertakan perintah penghapusan tabel atau kolom (<code>DROP</code>).
                    </li>
                    <li>
                        Tekan <strong>Bandingkan Skema</strong>. Bila hasilnya sudah sesuai, klik tombol
                        kirim ke draft untuk langsung membuka halaman pengisian <strong>Drafts</strong>.
                    </li>
                </ol>
            </>
        ),

        drafts: (
            <>
                <p>
                    Tab <strong>Drafts</strong> di menu <strong>Migrations</strong> dipakai pengguna
                    berizin <strong>EDIT</strong> untuk menyusun perintah SQL perubahan sebelum dikirim
                    ke review.
                </p>

                <h2>Mengisi formulir draft</h2>
                <dl className="docs-fields">
                    <div>
                        <dt>Deskripsi Migration</dt>
                        <dd>
                            Tulis keterangan singkat tujuan perubahan (2–200 karakter) agar mudah dibaca
                            rekan tim saat review.
                        </dd>
                    </div>
                    <div>
                        <dt>Mode Eksekusi</dt>
                        <dd>
                            Pilih <strong>NORMAL</strong> untuk perubahan struktur biasa (seperti tambah
                            tabel/kolom). Pilih <strong>BATCH</strong> bila mengubah data dalam jumlah
                            sangat besar secara bertahap; isi nama kolom acuan pada{" "}
                            <strong>Kolom Cursor Batch</strong> dan jumlah baris per tahap pada{" "}
                            <strong>Batch Size</strong>.
                        </dd>
                    </div>
                    <div>
                        <dt>SQL Up &amp; SQL Down</dt>
                        <dd>
                            Isi <strong>SQL Up</strong> dengan perintah perubahan utama, dan{" "}
                            <strong>SQL Down</strong> dengan perintah kebalikannya untuk berjaga-jaga
                            jika perlu dibatalkan (rollback). Jika perubahan memang tidak dapat
                            dibatalkan, centang <strong>Irreversible</strong> dan tuliskan alasannya.
                        </dd>
                    </div>
                </dl>

                <h2>Contoh penulisan SQL Up dan SQL Down</h2>
                <pre className="sql-preview">
                    {`-- SQL Up (perubahan yang diterapkan)\nALTER TABLE customers ADD COLUMN tier TEXT NOT NULL DEFAULT 'free';\n\n-- SQL Down (kebalikan untuk rollback)\nALTER TABLE customers DROP COLUMN tier;`}
                </pre>

                <h2>Periksa query sebelum diajukan</h2>
                <p>
                    Gunakan tombol <strong>Explain Plan</strong> untuk melihat perkiraan biaya eksekusi
                    query tanpa mengubah isi database. Jika sudah yakin, simpan draft lalu klik{" "}
                    <strong>Ajukan Review</strong>.
                </p>

                <div className="docs-note">
                    <p className="docs-note-title">Draft terkunci setelah diajukan</p>
                    <p>
                        Begitu dikirim ke review, draft menjadi baca-saja agar isi pengajuan tidak
                        berubah saat diperiksa leader. Jika perlu mengubah isi SQL, tekan{" "}
                        <strong>+ Draft Baru</strong> untuk membuat rancangan baru.
                    </p>
                </div>
            </>
        ),

        reviews: (
            <>
                <p>
                    Setiap pengajuan perubahan—baik draft baru, pembatalan (rollback), pengajuan ulang
                    (forward), maupun pembatalan batch—akan masuk ke tab{" "}
                    <strong>Review &amp; Approval</strong>.
                </p>

                <h2>Alur persetujuan</h2>
                <ol className="docs-steps">
                    <li>
                        Pengajuan perlu disetujui oleh <strong>Leader</strong> dari tiap grup yang
                        memiliki izin pada database tersebut.
                    </li>
                    <li>
                        Buka <strong>Lihat SQL Snapshot</strong> pada kartu pengajuan untuk mengecek
                        persis perintah yang akan dijalankan.
                    </li>
                    <li>
                        Jika Anda adalah leader yang berwenang, kolom catatan beserta tombol{" "}
                        <strong>Setujui</strong> dan <strong>Tolak</strong> akan tampil. Penolakan wajib
                        disertai alasan pada kolom catatan.
                    </li>
                    <li>
                        Setelah seluruh leader menyetujui dan statusnya menjadi <strong>APPROVED</strong>,
                        pengguna berizin <strong>EDIT</strong> dapat menekan{" "}
                        <strong>Jalankan via Antrean Worker</strong>.
                    </li>
                </ol>

                <div className="docs-note">
                    <p className="docs-note-title">Muncul pesan grup belum memiliki leader?</p>
                    <p>
                        Hubungi Admin Platform agar menunjuk leader aktif untuk grup yang terhubung ke
                        database tersebut melalui menu Administrasi.
                    </p>
                </div>
            </>
        ),

        jobs: (
            <>
                <p>
                    Pengajuan yang sudah dikirim ke antrean akan diproses di latar belakang dan muncul di
                    tab <strong>Jobs</strong>. Status pada halaman ini diperbarui otomatis selama proses
                    masih antre atau berjalan.
                </p>

                <h2>Arti status &amp; tombol tindakan</h2>
                <dl className="docs-fields">
                    <div>
                        <dt>Status Job</dt>
                        <dd>
                            <strong>PENDING</strong> berarti menunggu giliran diproses,{" "}
                            <strong>RUNNING</strong> sedang dijalankan, <strong>SUCCESS</strong> selesai
                            berhasil, sedangkan <strong>FAILED</strong> / <strong>PARTIAL</strong> berarti
                            terhenti karena kendala.
                        </dd>
                    </div>
                    <div>
                        <dt>Detail Checkpoint</dt>
                        <dd>
                            Menampilkan rincian setiap tahap pengerjaan pada mode batch, termasuk jumlah
                            baris yang sudah selesai diproses.
                        </dd>
                    </div>
                    <div>
                        <dt>Retry dari Checkpoint &amp; Cancel Batch</dt>
                        <dd>
                            Pada job batch yang terhenti, pengguna berizin <strong>EDIT</strong> dapat
                            melanjutkan proses dari titik terakhir (<strong>Retry dari Checkpoint</strong>)
                            atau mengajukan pembatalan tahap yang sudah berjalan (<strong>Cancel Batch</strong>
                            , tetap melewati persetujuan review).
                        </dd>
                    </div>
                </dl>

                <div className="docs-note">
                    <p className="docs-note-title">Status masih PENDING cukup lama?</p>
                    <p>
                        Artinya permintaan antrean sudah masuk ke daftar tunggu, tetapi belum diambil
                        untuk dijalankan. Jika tidak kunjung berubah menjadi RUNNING, hubungi Admin
                        Platform untuk mengecek layanan pemroses antrean.
                    </p>
                </div>
            </>
        ),

        history: (
            <>
                <p>
                    Tab <strong>History</strong> menampilkan seluruh catatan perubahan yang pernah
                    dijalankan pada database tersebut secara kronologis dan permanen.
                </p>

                <h2>Membaca riwayat &amp; mengajukan Rollback / Forward</h2>
                <ol className="docs-steps">
                    <li>
                        Setiap baris menampilkan versi hasil perubahan, jenis aksi, deskripsi, nama
                        pelaksana, dan status akhir. Klik <strong>Lihat SQL</strong> untuk membaca
                        perintah yang dijalankan.
                    </li>
                    <li>
                        Centang <strong>Sembunyikan yang sudah di-rollback</strong> bila ingin
                        menyembunyikan perubahan yang sudah pernah dibatalkan agar daftar lebih ringkas.
                    </li>
                    <li>
                        Tombol <strong>Ajukan Rollback</strong> muncul pada perubahan aktif paling akhir
                        yang masih bisa dibatalkan. Setelah di-rollback, tombol{" "}
                        <strong>Ajukan Forward</strong> tersedia jika Anda ingin mengajukannya kembali.
                    </li>
                </ol>
            </>
        ),

        admin: (
            <>
                <p>
                    Menu{" "}
                    {user?.is_platform_admin ? (
                        <Link className="docs-link-chip" to="/admin">
                            Administrasi
                        </Link>
                    ) : (
                        <strong>Administrasi</strong>
                    )}{" "}
                    khusus digunakan oleh <strong>Platform Admin</strong> untuk mengatur pengguna dan
                    koneksi database.
                </p>

                <dl className="docs-fields">
                    <div>
                        <dt>Users</dt>
                        <dd>
                            Menambahkan akun baru, mengatur apakah pengguna berstatus admin platform, dan
                            mengaktifkan atau menonaktifkan akun.
                        </dd>
                    </div>
                    <div>
                        <dt>Groups</dt>
                        <dd>
                            Membuat grup akses, memasukkan atau mengeluarkan anggota, serta menetapkan satu
                            anggota sebagai <strong>Leader</strong> grup untuk menyetujui review.
                        </dd>
                    </div>
                    <div>
                        <dt>Grants</dt>
                        <dd>
                            Mengatur grup mana saja yang boleh membuka suatu database beserta izinnya (
                            <strong>VIEW</strong> atau <strong>EDIT</strong>), serta mencabut akses jika
                            diperlukan.
                        </dd>
                    </div>
                    <div>
                        <dt>Targets</dt>
                        <dd>
                            Mendaftarkan koneksi database baru (PostgreSQL atau SQLite), mengubah pengaturan
                            koneksi, menonaktifkan target, serta memeriksa koneksi lewat tombol{" "}
                            <strong>Test</strong>.
                        </dd>
                    </div>
                </dl>
            </>
        ),

        roles: (
            <>
                <h2>Perbandingan hak tiap peran</h2>
                <div className="table-scroll">
                    <table className="data-table">
                        <thead>
                            <tr>
                                <th>Peran / Izin</th>
                                <th>Explorer</th>
                                <th>Diff &amp; Draft</th>
                                <th>Review &amp; Jobs</th>
                                <th>Administrasi</th>
                            </tr>
                        </thead>
                        <tbody>
                            <tr>
                                <td>
                                    <strong>Viewer (VIEW)</strong>
                                </td>
                                <td>Lihat struktur &amp; baris</td>
                                <td>Tidak</td>
                                <td>Lihat review, jobs, history</td>
                                <td>Tidak</td>
                            </tr>
                            <tr>
                                <td>
                                    <strong>Editor (EDIT)</strong>
                                </td>
                                <td>Lihat struktur &amp; baris</td>
                                <td>Buat diff, draft, explain</td>
                                <td>Ajukan review, jalankan antrean, retry</td>
                                <td>Tidak</td>
                            </tr>
                            <tr>
                                <td>
                                    <strong>Group Leader</strong>
                                </td>
                                <td>Sesuai izin grupnya</td>
                                <td>Sesuai izin grupnya</td>
                                <td>Menyetujui / menolak review grupnya</td>
                                <td>Tidak</td>
                            </tr>
                            <tr>
                                <td>
                                    <strong>Platform Admin</strong>
                                </td>
                                <td>Sesuai izin grupnya</td>
                                <td>Sesuai izin grupnya</td>
                                <td>Sesuai izin &amp; status leader</td>
                                <td>Kelola Users, Groups, Grants, Targets</td>
                            </tr>
                        </tbody>
                    </table>
                </div>

                <h2>Kendala yang sering ditemui</h2>
                <dl className="docs-fields">
                    <div>
                        <dt>Tombol Schema Diff atau buat Draft tidak tampil</dt>
                        <dd>
                            Akun Anda pada database tersebut baru berizin <strong>VIEW</strong>. Minta
                            Admin Platform mengubah izin grup Anda menjadi <strong>EDIT</strong>.
                        </dd>
                    </div>
                    <div>
                        <dt>Draft tidak bisa diubah lagi</dt>
                        <dd>
                            Draft terkunci begitu dikirim ke review. Tekan tombol{" "}
                            <strong>+ Draft Baru</strong> di bagian atas formulir untuk menulis revisi
                            baru.
                        </dd>
                    </div>
                    <div>
                        <dt>Ingin mematikan gambar latar belakang</dt>
                        <dd>
                            Tekan tombol ikon gambar (<strong>Wallpaper</strong>) di bagian kanan atas
                            layar. Pilihan ini otomatis tersimpan di browser Anda.
                        </dd>
                    </div>
                </dl>
            </>
        ),
    };

    return (
        <div className="docs-shell">
            <aside className="card docs-sidebar" aria-label="Daftar topik panduan">
                <div className="field">
                    <label htmlFor="docs-search">Cari panduan</label>
                    <input
                        id="docs-search"
                        type="search"
                        placeholder="Misal: rollback, leader, diff..."
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                    />
                </div>

                {filteredTopics.length === 0 ? (
                    <div className="stack">
                        <p className="muted">Topik "{search}" tidak ditemukan.</p>
                        <button type="button" className="btn-sm" onClick={() => setSearch("")}>
                            Tampilkan Semua Topik
                        </button>
                    </div>
                ) : (
                    <nav className="docs-nav">
                        {GROUPS.map((group) => {
                            const items = filteredTopics.filter((item) => item.group === group);
                            if (items.length === 0) return null;
                            return (
                                <div key={group} className="docs-nav-group">
                                    <p className="docs-nav-heading">{group}</p>
                                    <ul>
                                        {items.map((item) => (
                                            <li key={item.id}>
                                                <button
                                                    type="button"
                                                    className={`docs-nav-item ${activeId === item.id ? "is-active" : ""}`}
                                                    onClick={() => selectTopic(item.id)}
                                                >
                                                    {item.title}
                                                </button>
                                            </li>
                                        ))}
                                    </ul>
                                </div>
                            );
                        })}
                    </nav>
                )}
            </aside>

            <article className="card docs-article">
                <div className="docs-prose">
                    <p className="docs-kicker">{currentTopic.group}</p>
                    <h1>{currentTopic.title}</h1>
                    <p className="docs-lead">{currentTopic.summary}</p>

                    <div className="docs-body">{content[currentTopic.id]}</div>

                    <footer className="docs-pager">
                        {prevTopic ? (
                            <button
                                type="button"
                                className="docs-pager-btn"
                                onClick={() => selectTopic(prevTopic.id)}
                            >
                                <span className="muted">Sebelumnya</span>
                                <strong>{prevTopic.title}</strong>
                            </button>
                        ) : (
                            <span />
                        )}

                        {nextTopic && (
                            <button
                                type="button"
                                className="docs-pager-btn is-next"
                                onClick={() => selectTopic(nextTopic.id)}
                            >
                                <span className="muted">Selanjutnya</span>
                                <strong>{nextTopic.title}</strong>
                            </button>
                        )}
                    </footer>
                </div>
            </article>
        </div>
    );
}
