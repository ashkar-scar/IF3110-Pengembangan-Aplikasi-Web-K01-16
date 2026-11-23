import { useState, type FormEvent } from "react";
import { removeGroupGrant, setGroupGrant } from "../../api/admin";
import { StatePanel } from "../StatePanel";
import { TablePaginationBar } from "../TablePager";
import { usePagedList } from "../../utils/usePagedList";
import { ApiRequestError } from "../../types/auth";
import type { AdminDatabaseTarget, DatabaseGrant, Group } from "../../types/admin";

interface GrantsPanelProps {
    grants: DatabaseGrant[];
    groups: Group[];
    targets: AdminDatabaseTarget[];
    onRefresh: () => Promise<void>;
}

export function GrantsPanel({ grants, groups, targets, onRefresh }: GrantsPanelProps) {
    const [groupId, setGroupId] = useState("");
    const [targetId, setTargetId] = useState("");
    const [permission, setPermission] = useState<"VIEW" | "EDIT">("VIEW");

    const [error, setError] = useState("");
    const [notice, setNotice] = useState("");
    const [isSubmitting, setIsSubmitting] = useState(false);

    // Memakai nilai pilihan user, atau fallback ke item pertama biar select gak pernah kosong pas data berubah.
    const effectiveGroupId = groups.some((g) => g.id === Number(groupId))
        ? Number(groupId)
        : (groups[0]?.id ?? 0);
    const effectiveTargetId = targets.some((t) => t.id === Number(targetId))
        ? Number(targetId)
        : (targets[0]?.id ?? 0);

    const groupMap = new Map(groups.map((g) => [g.id, g]));
    const targetMap = new Map(targets.map((t) => [t.id, t]));
    const pager = usePagedList(grants, (grant) => {
        const gName = groupMap.get(grant.group_id)?.name ?? "";
        const tName = targetMap.get(grant.database_id)?.name ?? "";
        return `${gName} ${tName} ${grant.permission}`;
    });

    async function handleSubmit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        if (!effectiveGroupId || !effectiveTargetId) return;

        setError("");
        setNotice("");
        setIsSubmitting(true);

        try {
            await setGroupGrant(effectiveGroupId, effectiveTargetId, permission);
            setNotice("Grant akses database berhasil disimpan.");
            await onRefresh();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal menyimpan grant.");
        } finally {
            setIsSubmitting(false);
        }
    }

    async function handleTogglePermission(grant: DatabaseGrant) {
        const nextPermission = grant.permission === "EDIT" ? "VIEW" : "EDIT";
        setError("");
        setNotice("");
        setIsSubmitting(true);

        try {
            await setGroupGrant(grant.group_id, grant.database_id, nextPermission);
            setNotice(`Izin diubah menjadi ${nextPermission}.`);
            await onRefresh();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal mengubah izin.");
        } finally {
            setIsSubmitting(false);
        }
    }

    async function handleRemove(grant: DatabaseGrant) {
        const groupName = groupMap.get(grant.group_id)?.name ?? `Group #${grant.group_id}`;
        const targetName = targetMap.get(grant.database_id)?.name ?? `Database #${grant.database_id}`;
        if (!window.confirm(`Cabut izin "${groupName}" pada database "${targetName}"?`)) {
            return;
        }

        setError("");
        setNotice("");
        setIsSubmitting(true);

        try {
            await removeGroupGrant(grant.group_id, grant.database_id);
            setNotice("Grant berhasil dicabut.");
            await onRefresh();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal mencabut grant.");
        } finally {
            setIsSubmitting(false);
        }
    }

    return (
        <div className="stack">
            <section className="card">
                <h2>Tetapkan Grant Database</h2>

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

                {groups.length === 0 || targets.length === 0 ? (
                    <p className="muted">
                        Pastikan sudah ada minimal satu group dan satu database target terlebih
                        dahulu.
                    </p>
                ) : (
                    <form onSubmit={handleSubmit}>
                        <div className="form-grid">
                            <div className="field">
                                <label htmlFor="grant-group">Group</label>
                                <select
                                    id="grant-group"
                                    value={effectiveGroupId}
                                    onChange={(e) => setGroupId(e.target.value)}
                                >
                                    {groups.map((g) => (
                                        <option key={g.id} value={g.id}>
                                            {g.name}
                                        </option>
                                    ))}
                                </select>
                            </div>

                            <div className="field">
                                <label htmlFor="grant-target">Database Target</label>
                                <select
                                    id="grant-target"
                                    value={effectiveTargetId}
                                    onChange={(e) => setTargetId(e.target.value)}
                                >
                                    {targets.map((t) => (
                                        <option key={t.id} value={t.id}>
                                            {t.name} ({t.driver}){!t.is_active ? " (Nonaktif)" : ""}
                                        </option>
                                    ))}
                                </select>
                            </div>

                            <div className="field">
                                <label htmlFor="grant-permission">Permission</label>
                                <select
                                    id="grant-permission"
                                    value={permission}
                                    onChange={(e) =>
                                        setPermission(e.target.value === "EDIT" ? "EDIT" : "VIEW")
                                    }
                                >
                                    <option value="VIEW">VIEW</option>
                                    <option value="EDIT">EDIT</option>
                                </select>
                            </div>
                        </div>

                        <button type="submit" className="primary" disabled={isSubmitting}>
                            {isSubmitting ? "Menyimpan..." : "Simpan Grant"}
                        </button>
                    </form>
                )}
            </section>

            <section className="card">
                <div className="page-header">
                    <h2>Daftar Grant ({grants.length})</h2>
                    {pager.shouldShowSearch && (
                        <input
                            type="search"
                            aria-label="Cari grant"
                            placeholder="Cari group / database..."
                            value={pager.query}
                            onChange={(e) => pager.setQuery(e.target.value)}
                            style={{ maxInlineSize: "240px" }}
                        />
                    )}
                </div>
                {grants.length === 0 ? (
                    <p className="muted">Belum ada grant yang ditetapkan.</p>
                ) : pager.filteredTotal === 0 ? (
                    <StatePanel
                        variant="empty"
                        title="Grant tidak ditemukan"
                        message={`Tidak ada grant yang cocok dengan "${pager.query}".`}
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
                                        <th>Group</th>
                                        <th>Database</th>
                                        <th>Permission</th>
                                        <th>Aksi</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {pager.pageItems.map((grant) => {
                                        const group = groupMap.get(grant.group_id);
                                        const target = targetMap.get(grant.database_id);
                                        return (
                                            <tr key={`${grant.group_id}-${grant.database_id}`}>
                                                <td>{group ? group.name : `Group #${grant.group_id}`}</td>
                                                <td>
                                                    {target
                                                        ? target.name
                                                        : `Database #${grant.database_id}`}
                                                </td>
                                                <td>
                                                    <span
                                                        className={`badge ${grant.permission === "EDIT" ? "success" : "info"}`}
                                                    >
                                                        {grant.permission}
                                                    </span>
                                                </td>
                                                <td>
                                                    <div className="action-row">
                                                        <button
                                                            type="button"
                                                            className="btn-sm"
                                                            disabled={isSubmitting}
                                                            onClick={() => handleTogglePermission(grant)}
                                                        >
                                                            Ubah ke{" "}
                                                            {grant.permission === "EDIT"
                                                                ? "VIEW"
                                                                : "EDIT"}
                                                        </button>
                                                        <button
                                                            type="button"
                                                            className="btn-sm danger"
                                                            disabled={isSubmitting}
                                                            onClick={() => handleRemove(grant)}
                                                        >
                                                            Cabut
                                                        </button>
                                                    </div>
                                                </td>
                                            </tr>
                                        );
                                    })}
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