import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { fetchDatabases } from "../api/databases";
import { runSchemaDiff } from "../api/migrations";
import { StatePanel } from "../components/StatePanel";
import { ApiRequestError } from "../types/auth";
import type { DatabaseTarget } from "../types/database";
import type { SchemaDiffResponse } from "../types/migration";

export function DiffPage() {
    const { id } = useParams();
    const targetId = Number(id);
    const navigate = useNavigate();

    const [targets, setTargets] = useState<DatabaseTarget[]>([]);
    const [sourceId, setSourceId] = useState("");
    const [currentSchema, setCurrentSchema] = useState("public");
    const [desiredSchema, setDesiredSchema] = useState("public");
    const [allowDestructive, setAllowDestructive] = useState(false);

    const [result, setResult] = useState<SchemaDiffResponse | null>(null);
    const [isLoading, setIsLoading] = useState(true);
    const [isComparing, setIsComparing] = useState(false);
    const [error, setError] = useState("");

    useEffect(() => {
        if (!Number.isInteger(targetId) || targetId <= 0) {
            return;
        }

        let active = true;
        fetchDatabases()
            .then((list) => {
                if (!active) return;
                setTargets(list);
                const currentTarget = list.find((item) => item.id === targetId);
                if (currentTarget?.driver === "sqlite") {
                    setCurrentSchema("main");
                    setDesiredSchema("main");
                }
                const firstOther = list.find((item) => item.id !== targetId);
                if (firstOther) {
                    setSourceId(String(firstOther.id));
                }
            })
            .catch((err) => {
                if (!active) return;
                setError(
                    err instanceof ApiRequestError
                        ? err.message
                        : "Gagal memuat daftar database.",
                );
            })
            .finally(() => {
                if (active) setIsLoading(false);
            });

        return () => {
            active = false;
        };
    }, [targetId]);

    const currentTarget = targets.find((item) => item.id === targetId) ?? null;
    const comparableTargets = targets.filter((item) =>
        currentTarget ? item.driver === currentTarget.driver : true,
    );

    async function handleSubmit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        if (!sourceId || !Number.isInteger(targetId)) return;

        setError("");
        setIsComparing(true);
        setResult(null);

        try {
            const diff = await runSchemaDiff(targetId, {
                source_id: Number(sourceId),
                current_schema: currentSchema.trim(),
                desired_schema: desiredSchema.trim(),
                allow_destructive: allowDestructive,
            });
            setResult(diff);
        } catch (err) {
            setError(
                err instanceof ApiRequestError ? err.message : "Gagal membandingkan skema.",
            );
        } finally {
            setIsComparing(false);
        }
    }

    function sendSqlToDraft() {
        if (!result?.sql) return;
        navigate(`/databases/${id}/migrations?sql=${encodeURIComponent(result.sql)}`);
    }

    const diffChanges = result?.changes ?? [];

    return (
        <div className="stack">
            <section className="card">
                <div className="page-header">
                    <div>
                        <p className="breadcrumb">
                            <Link to="/databases">Daftar Database</Link> /{" "}
                            <Link to={`/databases/${id}`}>Explorer</Link> / Schema Diff
                        </p>
                        <h1>
                            Schema Diff: {currentTarget ? currentTarget.name : `Database #${id}`}
                        </h1>
                        <p className="muted">
                            Bandingkan struktur skema target ini terhadap database referensi untuk
                            menghasilkan DDL migration.
                        </p>
                    </div>

                    <div className="action-row">
                        <Link className="btn" to={`/databases/${id}`}>
                            Explorer
                        </Link>
                        <Link className="btn" to={`/databases/${id}/migrations`}>
                            Migrations
                        </Link>
                    </div>
                </div>

                {error && (
                    <p className="alert error" role="alert">
                        {error}
                    </p>
                )}

                {isLoading ? (
                    <StatePanel
                        variant="loading"
                        title="Memuat target perbandingan"
                        message="Mengambil daftar database yang kompatibel..."
                    />
                ) : comparableTargets.length === 0 ? (
                    <StatePanel
                        variant="empty"
                        title="Belum ada target pembanding"
                        message="Anda belum memiliki akses ke database dengan driver yang sama untuk dibandingkan."
                        action={
                            <Link className="btn" to={`/databases/${id}`}>
                                Kembali ke Explorer
                            </Link>
                        }
                    />
                ) : (
                    <form onSubmit={handleSubmit}>
                        <div className="form-grid">
                            <div className="field">
                                <label htmlFor="diff-source">Database Referensi (Desired)</label>
                                <select
                                    id="diff-source"
                                    value={sourceId}
                                    onChange={(e) => setSourceId(e.target.value)}
                                    required
                                >
                                    <option value="">-- Pilih database sumber skema --</option>
                                    {comparableTargets.map((item) => (
                                        <option key={item.id} value={item.id}>
                                            {item.name} ({item.driver})
                                        </option>
                                    ))}
                                </select>
                            </div>

                            <div className="field">
                                <label htmlFor="diff-current-schema">Schema Target</label>
                                <input
                                    id="diff-current-schema"
                                    type="text"
                                    value={currentSchema}
                                    onChange={(e) => setCurrentSchema(e.target.value)}
                                    required
                                />
                            </div>

                            <div className="field">
                                <label htmlFor="diff-desired-schema">Schema Referensi</label>
                                <input
                                    id="diff-desired-schema"
                                    type="text"
                                    value={desiredSchema}
                                    onChange={(e) => setDesiredSchema(e.target.value)}
                                    required
                                />
                            </div>
                        </div>

                        <div className="checkbox-row">
                            <label>
                                <input
                                    type="checkbox"
                                    checked={allowDestructive}
                                    onChange={(e) => setAllowDestructive(e.target.checked)}
                                />
                                Sertakan statement destruktif (DROP TABLE / DROP COLUMN)
                            </label>
                        </div>

                        <button type="submit" className="primary" disabled={isComparing}>
                            {isComparing ? "Membandingkan..." : "Bandingkan Skema"}
                        </button>
                    </form>
                )}
            </section>

            {result && (
                <section className="card stack">
                    <div className="page-header">
                        <div>
                            <h2>Hasil Perbandingan ({diffChanges.length} perubahan)</h2>
                            <p className="muted">
                                Fingerprint: <code>{result.fingerprint}</code>
                            </p>
                        </div>

                        {result.sql && (
                            <button type="button" className="primary" onClick={sendSqlToDraft}>
                                Kirim SQL ke Draft Migration
                            </button>
                        )}
                    </div>

                    {result.manual && (
                        <p className="alert warning" role="alert">
                            Sebagian perubahan memerlukan penyesuaian manual sebelum dijalankan.
                        </p>
                    )}

                    {diffChanges.length === 0 ? (
                        <p className="muted">Tidak ada perbedaan skema antara kedua schema.</p>
                    ) : (
                        <>
                            <div className="table-scroll">
                                <table className="data-table">
                                    <thead>
                                        <tr>
                                            <th>Objek</th>
                                            <th>Perubahan</th>
                                            <th>Sifat</th>
                                            <th>SQL</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {diffChanges.map((ch, idx) => (
                                            <tr key={`${ch.object}-${idx}`}>
                                                <td>
                                                    <code>{ch.object}</code>
                                                </td>
                                                <td>{ch.description}</td>
                                                <td>
                                                    {ch.destructive && (
                                                        <span className="badge danger">
                                                            Destructive
                                                        </span>
                                                    )}{" "}
                                                    {ch.manual && (
                                                        <span className="badge warning">Manual</span>
                                                    )}
                                                </td>
                                                <td>
                                                    <pre className="sql-preview">{ch.sql}</pre>
                                                </td>
                                            </tr>
                                        ))}
                                    </tbody>
                                </table>
                            </div>

                            {result.sql && (
                                <div>
                                    <h3>Gabungan SQL Migration</h3>
                                    <pre className="sql-preview">{result.sql}</pre>
                                </div>
                            )}
                        </>
                    )}
                </section>
            )}
        </div>
    );
}
