import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { fetchDatabases } from "../api/databases";
import { StatePanel } from "../components/StatePanel";
import { TablePaginationBar } from "../components/TablePager";
import { usePagedList } from "../utils/usePagedList";
import { ApiRequestError } from "../types/auth";
import type { DatabasePermission, DatabaseTarget } from "../types/database";

function permissionBadgeClass(permission: DatabasePermission) {
    if (permission === "EDIT") return "badge success";
    if (permission === "VIEW") return "badge info";
    return "badge";
}

export function DatabasesPage() {
    const [targets, setTargets] = useState<DatabaseTarget[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [error, setError] = useState("");
    const pager = usePagedList(targets, (t) => `${t.name} ${t.driver} ${t.permission}`);

    const loadTargets = useCallback(async () => {
        setError("");
        try {
            const data = await fetchDatabases();
            setTargets(data);
        } catch (err) {
            setError(
                err instanceof ApiRequestError
                    ? err.message
                    : "Gagal memuat daftar database.",
            );
        } finally {
            setIsLoading(false);
        }
    }, []);

    useEffect(() => {
        let active = true;
        Promise.resolve().then(() => {
            if (active) return loadTargets();
        });
        return () => {
            active = false;
        };
    }, [loadTargets]);

    function handleRetry() {
        setIsLoading(true);
        void loadTargets();
    }

    return (
        <section className="card">
            <div className="page-header">
                <div>
                    <h1>Daftar Database</h1>
                    <p className="muted">Database target yang dapat diakses sesuai grant grup Anda.</p>
                </div>

                <div className="action-row">
                    {pager.shouldShowSearch && (
                        <input
                            type="search"
                            aria-label="Cari database"
                            placeholder="Cari database..."
                            value={pager.query}
                            onChange={(e) => pager.setQuery(e.target.value)}
                            style={{ maxInlineSize: "220px" }}
                        />
                    )}
                    <button type="button" onClick={handleRetry} disabled={isLoading}>
                        {isLoading ? "Memuat..." : "Muat Ulang"}
                    </button>
                </div>
            </div>

            {isLoading && targets.length === 0 ? (
                <StatePanel
                    variant="loading"
                    title="Memuat daftar database"
                    message="Mengambil target database sesuai izin grup Anda..."
                />
            ) : error ? (
                <StatePanel
                    variant="error"
                    title="Gagal memuat daftar database"
                    message={error}
                    onRetry={handleRetry}
                />
            ) : targets.length === 0 ? (
                <StatePanel
                    variant="empty"
                    title="Belum ada database"
                    message="Belum ada database target yang diberikan akses ke akun Anda."
                    onRetry={handleRetry}
                    retryLabel="Cek Ulang"
                />
            ) : pager.filteredTotal === 0 ? (
                <StatePanel
                    variant="empty"
                    title="Database tidak ditemukan"
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
                                    <th>Izin</th>
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
                                            <span className={permissionBadgeClass(target.permission)}>
                                                {target.permission}
                                            </span>
                                        </td>
                                        <td>
                                            <Link className="btn" to={`/databases/${target.id}`}>
                                                Buka Explorer
                                            </Link>
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
    );
}