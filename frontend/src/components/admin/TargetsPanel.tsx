import { useState, type FormEvent } from "react";
import {
    createAdminTarget,
    disableAdminTarget,
    testAdminTarget,
    updateAdminTarget,
} from "../../api/admin";
import { StatePanel } from "../StatePanel";
import { TablePaginationBar } from "../TablePager";
import { usePagedList } from "../../utils/usePagedList";
import { ApiRequestError } from "../../types/auth";
import type { AdminDatabaseTarget } from "../../types/admin";

interface TargetsPanelProps {
    targets: AdminDatabaseTarget[];
    onRefresh: () => Promise<void>;
}

const SSL_MODES = ["disable", "require", "verify-ca", "verify-full"];

export function TargetsPanel({ targets, onRefresh }: TargetsPanelProps) {
    const [editingId, setEditingId] = useState<number | null>(null);
    const [name, setName] = useState("");
    const [driver, setDriver] = useState("postgres");
    const [host, setHost] = useState("127.0.0.1");
    const [port, setPort] = useState("5432");
    const [database, setDatabase] = useState("");
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [sslmode, setSslmode] = useState("disable");
    const [isActive, setIsActive] = useState(true);
    const [createDatabase, setCreateDatabase] = useState(false);
    const [creatorUsername, setCreatorUsername] = useState("");
    const [creatorPassword, setCreatorPassword] = useState("");

    const [testingId, setTestingId] = useState<number | null>(null);
    const [error, setError] = useState("");
    const [notice, setNotice] = useState("");
    const pager = usePagedList(targets, (t) => `${t.name} ${t.driver} ${t.host} ${t.database}`);
    const [isSubmitting, setIsSubmitting] = useState(false);

    function resetForm() {
        setEditingId(null);
        setName("");
        setDriver("postgres");
        setHost("127.0.0.1");
        setPort("5432");
        setDatabase("");
        setUsername("");
        setPassword("");
        setSslmode("disable");
        setIsActive(true);
        setCreateDatabase(false);
        setCreatorUsername("");
        setCreatorPassword("");
    }

    function startEdit(target: AdminDatabaseTarget) {
        setEditingId(target.id);
        setName(target.name);
        setDriver(target.driver);
        setHost(target.host || "127.0.0.1");
        setPort(String(target.port || 5432));
        setDatabase(target.database);
        setUsername(target.username);
        // Mengosongkan password saat edit biar backend tetep pakai ciphertext lama.
        setPassword("");
        setSslmode(target.sslmode || "disable");
        setIsActive(target.is_active);
        setCreateDatabase(false);
        setCreatorUsername("");
        setCreatorPassword("");
        setError("");
        setNotice("");
    }

    async function handleSubmit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        setError("");
        setNotice("");
        setIsSubmitting(true);

        const payload =
            driver === "sqlite"
                ? {
                    name: name.trim(),
                    driver: "sqlite",
                    host: "",
                    port: 1,
                    database: "",
                    username: "",
                    password: "",
                    sslmode: "disable",
                    is_active: isActive,
                }
                : {
                    name: name.trim(),
                    driver: "postgres",
                    host: host.trim(),
                    port: Number(port) || 5432,
                    database: database.trim(),
                    username: username.trim(),
                    password,
                    sslmode,
                    is_active: isActive,
                    ...(editingId === null && createDatabase
                        ? {
                            create_database: true,
                            creator_username: creatorUsername.trim(),
                            creator_password: creatorPassword,
                        }
                        : {}),
                };

        try {
            if (editingId === null) {
                await createAdminTarget(payload);
                setNotice(
                    `Target "${payload.name}" berhasil ditambahkan. Jangan lupa beri akses grup di tab Grants agar muncul di Daftar Database.`,
                );
            } else {
                await updateAdminTarget(editingId, payload);
                setNotice(`Target "${payload.name}" berhasil diperbarui.`);
            }
            resetForm();
            await onRefresh();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal menyimpan target.");
        } finally {
            setIsSubmitting(false);
        }
    }

    async function handleTest(target: AdminDatabaseTarget) {
        setError("");
        setNotice("");
        setTestingId(target.id);

        try {
            await testAdminTarget(target.id);
            setNotice(`Koneksi ke "${target.name}" berhasil.`);
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Uji koneksi gagal.");
        } finally {
            setTestingId(null);
        }
    }

    async function handleDisable(target: AdminDatabaseTarget) {
        if (!window.confirm(`Nonaktifkan "${target.name}"? Sesi user terkait target ini akan dicabut.`)) {
            return;
        }
        setError("");
        setNotice("");
        setIsSubmitting(true);

        try {
            await disableAdminTarget(target.id);
            setNotice(`Target "${target.name}" berhasil dinonaktifkan.`);
            await onRefresh();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal menonaktifkan target.");
        } finally {
            setIsSubmitting(false);
        }
    }

    return (
        <div className="stack">
            <section className="card">
                <h2>{editingId === null ? "Tambah Database Target" : `Edit Target #${editingId}`}</h2>

                {error && (
                    <p className="alert error" role="alert">
                        {error}
                    </p>
                )}
                {notice && (
                    <p className="alert success" role="status">
                        {notice}
                    </p>
                )}

                <form onSubmit={handleSubmit}>
                    <div className="form-grid">
                        <div className="field">
                            <label htmlFor="target-name">Nama Target</label>
                            <input
                                id="target-name"
                                type="text"
                                maxLength={100}
                                value={name}
                                onChange={(e) => setName(e.target.value)}
                                required
                            />
                        </div>

                        <div className="field">
                            <label htmlFor="target-driver">Driver</label>
                            {/* Mengunci driver pas edit soalnya backend nolak ganti postgres <-> sqlite. */}
                            <select
                                id="target-driver"
                                value={driver}
                                disabled={editingId !== null}
                                onChange={(e) => setDriver(e.target.value)}
                            >
                                <option value="postgres">postgres</option>
                                <option value="sqlite">sqlite</option>
                            </select>
                        </div>
                    </div>

                    {driver === "sqlite" ? (
                        <p className="muted">
                            File SQLite akan dibuat dan dikelola otomatis oleh backend (tidak perlu isi host/port).
                        </p>
                    ) : (
                        <>
                            <div className="action-row" style={{ marginBottom: "0.75rem" }}>
                                <button
                                    type="button"
                                    className="btn-sm"
                                    onClick={() => {
                                        setHost("127.0.0.1");
                                        setPort("54329");
                                        setUsername("target_user");
                                        setSslmode("disable");
                                        if (!database) {
                                            setDatabase("sales");
                                        }
                                    }}
                                >
                                    Isi Preset Postgres Lokal
                                </button>
                                <span className="muted" style={{ fontSize: "0.85rem" }}>
                                    Database PostgreSQL harus sudah ada di server target (contoh compose lokal: <code>sales</code>, <code>hr</code>, <code>target_test</code>).
                                </span>
                            </div>
                            <div className="form-grid">
                            <div className="field">
                                <label htmlFor="target-host">Host</label>
                                <input
                                    id="target-host"
                                    type="text"
                                    value={host}
                                    onChange={(e) => setHost(e.target.value)}
                                    required
                                />
                            </div>

                            <div className="field">
                                <label htmlFor="target-port">Port</label>
                                <input
                                    id="target-port"
                                    type="number"
                                    min={1}
                                    max={65535}
                                    value={port}
                                    onChange={(e) => setPort(e.target.value)}
                                    required
                                />
                            </div>

                            <div className="field">
                                <label htmlFor="target-db">Nama Database</label>
                                <input
                                    id="target-db"
                                    type="text"
                                    value={database}
                                    onChange={(e) => setDatabase(e.target.value)}
                                    required
                                />
                            </div>

                            <div className="field">
                                <label htmlFor="target-user">Username DB</label>
                                <input
                                    id="target-user"
                                    type="text"
                                    value={username}
                                    onChange={(e) => setUsername(e.target.value)}
                                    required
                                />
                            </div>

                            <div className="field">
                                <label htmlFor="target-pass">
                                    {editingId === null
                                        ? "Password DB"
                                        : "Password Baru (kosongkan jika tetap)"}
                                </label>
                                <input
                                    id="target-pass"
                                    type="password"
                                    value={password}
                                    onChange={(e) => setPassword(e.target.value)}
                                />
                            </div>

                            <div className="field">
                                <label htmlFor="target-sslmode">SSL Mode</label>
                                <select
                                    id="target-sslmode"
                                    value={sslmode}
                                    onChange={(e) => setSslmode(e.target.value)}
                                >
                                    {SSL_MODES.map((mode) => (
                                        <option key={mode} value={mode}>
                                            {mode}
                                        </option>
                                    ))}
                                </select>
                            </div>
                            </div>
                        </>
                    )}

                    <div className="checkbox-row">
                        <label>
                            <input
                                type="checkbox"
                                checked={isActive}
                                onChange={(e) => setIsActive(e.target.checked)}
                            />
                            Aktif
                        </label>
                    </div>

                    {editingId === null && driver === "postgres" && (
                        <div className="stack" style={{ marginBottom: "1rem" }}>
                            <div className="checkbox-row">
                                <label>
                                    <input
                                        type="checkbox"
                                        checked={createDatabase}
                                        onChange={(e) => setCreateDatabase(e.target.checked)}
                                    />
                                    Buat database PostgreSQL baru jika belum ada
                                </label>
                            </div>

                            {createDatabase && (
                                <div className="details-box" style={{ marginBlockStart: 0 }}>
                                    <p className="muted" style={{ marginTop: 0 }}>
                                        Kosongkan field di bawah jika <strong>Username DB</strong> di
                                        atas sudah punya izin <code>CREATEDB</code>. Jika memakai user
                                        biasa (misal <code>target_user</code> di compose lokal), isi
                                        kredensial admin/superuser PostgreSQL (misal{" "}
                                        <code>platform</code>) hanya untuk pembuatan database ini.
                                    </p>
                                    <div className="form-grid">
                                        <div className="field">
                                            <label htmlFor="target-creator-user">
                                                Admin DB Pembuat (opsional)
                                            </label>
                                            <input
                                                id="target-creator-user"
                                                type="text"
                                                value={creatorUsername}
                                                onChange={(e) => setCreatorUsername(e.target.value)}
                                                placeholder="Kosongkan atau isi misal: platform"
                                            />
                                        </div>
                                        <div className="field">
                                            <label htmlFor="target-creator-pass">
                                                Password Admin DB (opsional)
                                            </label>
                                            <input
                                                id="target-creator-pass"
                                                type="password"
                                                value={creatorPassword}
                                                onChange={(e) => setCreatorPassword(e.target.value)}
                                            />
                                        </div>
                                    </div>
                                </div>
                            )}
                        </div>
                    )}

                    <div className="action-row">
                        <button type="submit" className="primary" disabled={isSubmitting}>
                            {isSubmitting
                                ? "Menyimpan..."
                                : editingId === null
                                    ? "Simpan Target"
                                    : "Update Target"}
                        </button>
                        {editingId !== null && (
                            <button type="button" onClick={resetForm} disabled={isSubmitting}>
                                Batal
                            </button>
                        )}
                    </div>
                </form>
            </section>

            <section className="card">
                <div className="page-header">
                    <h2>Daftar Database Target ({targets.length})</h2>
                    {pager.shouldShowSearch && (
                        <input
                            type="search"
                            aria-label="Cari target"
                            placeholder="Cari target / driver / host..."
                            value={pager.query}
                            onChange={(e) => pager.setQuery(e.target.value)}
                            style={{ maxInlineSize: "240px" }}
                        />
                    )}
                </div>
                {targets.length === 0 ? (
                    <p className="muted">
                        Belum ada database target yang terdaftar. Tambahkan lewat form di atas.
                    </p>
                ) : pager.filteredTotal === 0 ? (
                    <StatePanel
                        variant="empty"
                        title="Target tidak ditemukan"
                        message={`Tidak ada target yang cocok dengan "${pager.query}".`}
                        action={
                            <button type="button" onClick={() => pager.setQuery("")}>
                                Reset Pencarian
                            </button>
                        }
                    />
                ) : (
                    <>
                        <div className="table-scroll">
                            <table className="data-table">
                                <thead>
                                    <tr>
                                        <th>Nama</th>
                                        <th>Driver</th>
                                        <th>Koneksi</th>
                                        <th>Status</th>
                                        <th>Aksi</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {pager.pageItems.map((target) => (
                                        <tr key={target.id}>
                                            <td>{target.name}</td>
                                            <td>
                                                <code>{target.driver}</code>
                                            </td>
                                            <td>
                                                {target.driver === "sqlite" ? (
                                                    <code>{target.database}</code>
                                                ) : (
                                                    <code>
                                                        {target.username}@{target.host}:{target.port}/
                                                        {target.database} ({target.sslmode})
                                                    </code>
                                                )}
                                            </td>
                                            <td>
                                                <span
                                                    className={`badge ${target.is_active ? "success" : "danger"}`}
                                                >
                                                    {target.is_active ? "Aktif" : "Nonaktif"}
                                                </span>
                                            </td>
                                            <td>
                                                <div className="action-row">
                                                    <button
                                                        type="button"
                                                        className="btn-sm"
                                                        disabled={!target.is_active || testingId === target.id}
                                                        onClick={() => handleTest(target)}
                                                    >
                                                        {testingId === target.id
                                                            ? "Menguji..."
                                                            : "Uji Koneksi"}
                                                    </button>
                                                    <button
                                                        type="button"
                                                        className="btn-sm"
                                                        onClick={() => {
                                                            startEdit(target);
                                                            window.scrollTo({ top: 0, behavior: "smooth" });
                                                        }}
                                                    >
                                                        Edit
                                                    </button>
                                                    {target.is_active && (
                                                        <button
                                                            type="button"
                                                            className="btn-sm danger"
                                                            disabled={isSubmitting}
                                                            onClick={() => handleDisable(target)}
                                                        >
                                                            Nonaktifkan
                                                        </button>
                                                    )}
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                        <TablePaginationBar
                            page={pager.page}
                            totalPages={pager.totalPages}
                            totalItems={pager.filteredTotal}
                            pageSize={pager.pageSize}
                            onPageChange={pager.setPage}
                        />
                    </>
                )}
            </section>
        </div>
    );
}