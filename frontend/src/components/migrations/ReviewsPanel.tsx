import { useState } from "react";
import { approveReview, enqueueReview, rejectReview } from "../../api/migrations";
import { StatePanel } from "../StatePanel";
import { TablePaginationBar } from "../TablePager";
import { usePagedList } from "../../utils/usePagedList";
import { ApiRequestError } from "../../types/auth";
import type { ApprovalDecision, MigrationRequest, ReviewStatus } from "../../types/migration";

interface ReviewsPanelProps {
    targetId: number;
    canEdit: boolean;
    reviews: MigrationRequest[];
    onRefresh: () => Promise<void>;
    onEnqueued: () => void;
}

function statusBadgeClass(status: ReviewStatus | ApprovalDecision) {
    if (status === "APPROVED" || status === "COMPLETED") return "badge success";
    if (status === "REJECTED" || status === "FAILED") return "badge danger";
    if (status === "ENQUEUED") return "badge info";
    return "badge warning";
}

export function ReviewsPanel({
    targetId,
    canEdit,
    reviews,
    onRefresh,
    onEnqueued,
}: ReviewsPanelProps) {
    const [notes, setNotes] = useState<Record<number, string>>({});
    const [busyId, setBusyId] = useState<number | null>(null);
    const [error, setError] = useState("");
    const [notice, setNotice] = useState("");
    const pager = usePagedList(
        reviews,
        (r) => `${r.id} ${r.description} ${r.requested_by_name} ${r.status} ${r.action_type}`,
        10,
        8,
    );

    async function handleApprove(reqId: number) {
        setError("");
        setNotice("");
        setBusyId(reqId);

        try {
            await approveReview(targetId, reqId, (notes[reqId] ?? "").trim());
            setNotice(`Pengajuan #${reqId} disetujui.`);
            await onRefresh();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal menyetujui pengajuan.");
        } finally {
            setBusyId(null);
        }
    }

    async function handleReject(reqId: number) {
        const note = (notes[reqId] ?? "").trim();
        // Mewajibkan alasan pas nolak biar tim pengaju tau bagian mana yang perlu diperbaiki.
        if (!note) {
            setError(`Isi catatan penolakan untuk pengajuan #${reqId} terlebih dahulu.`);
            return;
        }

        setError("");
        setNotice("");
        setBusyId(reqId);

        try {
            await rejectReview(targetId, reqId, note);
            setNotice(`Pengajuan #${reqId} ditolak.`);
            await onRefresh();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal menolak pengajuan.");
        } finally {
            setBusyId(null);
        }
    }

    async function handleEnqueue(reqId: number) {
        setError("");
        setNotice("");
        setBusyId(reqId);

        try {
            await enqueueReview(targetId, reqId);
            await onRefresh();
            onEnqueued();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal memasukkan ke antrean.");
        } finally {
            setBusyId(null);
        }
    }

    return (
        <div className="stack">
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

            {pager.shouldShowSearch && (
                <section className="card">
                    <div className="page-header">
                        <h2>Daftar Review ({reviews.length})</h2>
                        <input
                            type="search"
                            aria-label="Cari review"
                            placeholder="Cari review / status / pengaju..."
                            value={pager.query}
                            onChange={(e) => pager.setQuery(e.target.value)}
                            style={{ maxInlineSize: "260px" }}
                        />
                    </div>
                </section>
            )}

            {reviews.length === 0 ? (
                <section className="card">
                    <p className="muted">Belum ada pengajuan migration untuk direview.</p>
                </section>
            ) : pager.filteredTotal === 0 ? (
                <section className="card">
                    <StatePanel
                        variant="empty"
                        title="Review tidak ditemukan"
                        message={`Tidak ada review yang cocok dengan "${pager.query}".`}
                        action={
                            <button type="button" onClick={() => pager.setQuery("")}>
                                Reset Pencarian
                            </button>
                        }
                    />
                </section>
            ) : (
                <>
                    {pager.pageItems.map((req) => (
                    <section key={req.id} className="card stack">
                        <div className="page-header">
                            <div>
                                <h2>
                                    Request #{req.id} (<code>{req.action_type}</code>)
                                </h2>
                                <p className="muted">
                                    {req.description}, diajukan oleh{" "}
                                    <strong>{req.requested_by_name}</strong> pada Draft #{req.draft_id}{" "}
                                    r{req.draft_revision} (<code>{req.execution_mode}</code>)
                                </p>
                            </div>
                            <span className={statusBadgeClass(req.status)}>{req.status}</span>
                        </div>

                        <details className="details-box">
                            <summary>Lihat SQL Snapshot</summary>
                            <p className="muted">SQL Up:</p>
                            <pre className="sql-preview">{req.sql_up || "(kosong)"}</pre>
                            {req.sql_down && (
                                <>
                                    <p className="muted">SQL Down:</p>
                                    <pre className="sql-preview">{req.sql_down}</pre>
                                </>
                            )}
                        </details>

                        <div className="table-scroll">
                            <table className="data-table">
                                <thead>
                                    <tr>
                                        <th>Group</th>
                                        <th>Leader</th>
                                        <th>Keputusan</th>
                                        <th>Catatan</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {(req.approvals ?? []).map((app) => (
                                        <tr key={app.group_id}>
                                            <td>{app.group_name}</td>
                                            <td>{app.leader_name}</td>
                                            <td>
                                                <span className={statusBadgeClass(app.decision)}>
                                                    {app.decision}
                                                </span>
                                            </td>
                                            <td>{app.note || <span className="muted">(kosong)</span>}</td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>

                        {req.can_current_user_approve && req.status === "PENDING_REVIEW" && (
                            <div className="inline-form">
                                <input
                                    type="text"
                                    placeholder="Catatan review (wajib jika menolak)"
                                    value={notes[req.id] ?? ""}
                                    onChange={(e) =>
                                        setNotes((prev) => ({ ...prev, [req.id]: e.target.value }))
                                    }
                                />
                                <button
                                    type="button"
                                    className="primary"
                                    disabled={busyId === req.id}
                                    onClick={() => handleApprove(req.id)}
                                >
                                    Setujui
                                </button>
                                <button
                                    type="button"
                                    className="danger"
                                    disabled={busyId === req.id}
                                    onClick={() => handleReject(req.id)}
                                >
                                    Tolak
                                </button>
                            </div>
                        )}

                        {req.status === "APPROVED" && canEdit && !req.job_id && (
                            <div className="action-row">
                                <button
                                    type="button"
                                    className="primary"
                                    disabled={busyId === req.id}
                                    onClick={() => handleEnqueue(req.id)}
                                >
                                    {busyId === req.id
                                        ? "Memasukkan Antrean..."
                                        : "Jalankan via Antrean Worker"}
                                </button>
                            </div>
                        )}
                    </section>
                ))}
                {pager.shouldShowPagination && (
                    <section className="card">
                        <TablePaginationBar
                            page={pager.page}
                            totalPages={pager.totalPages}
                            totalItems={pager.filteredTotal}
                            pageSize={pager.pageSize}
                            onPageChange={pager.setPage}
                        />
                    </section>
                )}
            </>
            )}
        </div>
    );
}
