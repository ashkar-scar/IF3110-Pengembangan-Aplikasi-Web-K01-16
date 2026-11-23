import { useState } from "react";
import { submitForward, submitRollback } from "../../api/migrations";
import { ApiRequestError } from "../../types/auth";
import type { HistoryAttempt, HistoryResponse } from "../../types/migration";

const HISTORY_PAGE_SIZE = 200;

interface HistoryPanelProps {
    targetId: number;
    canEdit: boolean;
    history: HistoryResponse | null;
    hideRollback: boolean;
    offset: number;
    onFilterChange: (hideRollback: boolean, offset: number) => void;
    onSubmittedReview: () => void;
}

function attemptBadgeClass(status: HistoryAttempt["status"]) {
    if (status === "SUCCESS") return "badge success";
    if (status === "FAILED" || status === "UNKNOWN") return "badge danger";
    return "badge info";
}

function attemptVersionLabel(attempt: HistoryAttempt) {
    if (attempt.version !== null) return `v${attempt.version}`;
    if (attempt.batch_no != null) return `Batch #${attempt.batch_no}`;
    if (attempt.action_type === "CANCEL_BATCH") return "Pembatalan Batch";
    return "(tanpa versi)";
}

export function HistoryPanel({
    targetId,
    canEdit,
    history,
    hideRollback,
    offset,
    onFilterChange,
    onSubmittedReview,
}: HistoryPanelProps) {
    const [hideBatchLogs, setHideBatchLogs] = useState(true);
    const [busyExecId, setBusyExecId] = useState<string | null>(null);
    const [error, setError] = useState("");

    async function handleAction(kind: "rollback" | "forward", execId: string) {
        const label = kind === "rollback" ? "ROLLBACK" : "FORWARD";
        if (!window.confirm(`Ajukan ${label} untuk eksekusi ${execId} ke alur approval?`)) {
            return;
        }
        setError("");
        setBusyExecId(execId);

        try {
            if (kind === "rollback") {
                await submitRollback(targetId, execId);
            } else {
                await submitForward(targetId, execId);
            }
            onSubmittedReview();
        } catch (err) {
            setError(
                err instanceof ApiRequestError
                    ? err.message
                    : `Gagal mengajukan ${label.toLowerCase()}.`,
            );
        } finally {
            setBusyExecId(null);
        }
    }

    const attempts = history?.history ?? history?.attempts ?? [];
    const batchLogCount = attempts.filter((attempt) => attempt.batch_no != null).length;
    const visibleAttempts = hideBatchLogs
        ? attempts.filter((attempt) => attempt.batch_no == null)
        : attempts;

    return (
        <div className="stack">
            <section className="card">
                <div className="page-header">
                    <div>
                        <h2>Riwayat Eksekusi (Append-Only)</h2>
                        <p className="muted">
                            Rollback dan Forward hanya bisa diajukan sesuai urutan versi linear.
                        </p>
                    </div>

                    <div className="action-row">
                        <label className="checkbox-inline">
                            <input
                                type="checkbox"
                                checked={hideBatchLogs}
                                onChange={(e) => setHideBatchLogs(e.target.checked)}
                            />
                            Sembunyikan log per-batch
                            {batchLogCount > 0 ? ` (${batchLogCount})` : ""}
                        </label>
                        <label className="checkbox-inline">
                            <input
                                type="checkbox"
                                checked={hideRollback}
                                onChange={(e) => onFilterChange(e.target.checked, 0)}
                            />
                            Sembunyikan yang sudah di-rollback
                        </label>
                    </div>
                </div>

                {error && (
                    <p className="alert error" role="alert">
                        {error}
                    </p>
                )}

                {attempts.length === 0 ? (
                    <p className="muted">Belum ada riwayat eksekusi pada target ini.</p>
                ) : (
                    <>
                        {visibleAttempts.length === 0 ? (
                            <p className="muted">
                                Semua {attempts.length} entri pada halaman ini merupakan log
                                per-batch; matikan &quot;Sembunyikan log per-batch&quot; untuk
                                melihat detailnya.
                            </p>
                        ) : (
                            <div className="table-scroll">
                                <table className="data-table">
                                    <thead>
                                        <tr>
                                            <th>Versi / ID</th>
                                            <th>Aksi</th>
                                            <th>Deskripsi & SQL</th>
                                            <th>Eksekutor</th>
                                            <th>Status</th>
                                            <th>Aksi Linear</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {visibleAttempts.map((attempt) => {
                                            const canRollback =
                                                canEdit &&
                                                history?.can_rollback_exec_id === attempt.id;
                                            const canForward =
                                                canEdit &&
                                                history?.can_forward_exec_id === attempt.id;

                                            return (
                                                <tr key={attempt.id}>
                                                    <td>
                                                        <div>{attemptVersionLabel(attempt)}</div>
                                                        <code className="muted">{attempt.id}</code>
                                                    </td>
                                                    <td>
                                                        <code>{attempt.action_type}</code>
                                                    </td>
                                                    <td>
                                                        <div>{attempt.description}</div>
                                                        {attempt.error && (
                                                            <p className="alert error">
                                                                {attempt.error}
                                                            </p>
                                                        )}
                                                        <details className="details-box">
                                                            <summary>Lihat SQL</summary>
                                                            <pre className="sql-preview">
                                                                {attempt.sql}
                                                            </pre>
                                                        </details>
                                                    </td>
                                                    <td>{attempt.actor_name}</td>
                                                    <td>
                                                        <span
                                                            className={attemptBadgeClass(
                                                                attempt.status,
                                                            )}
                                                        >
                                                            {attempt.status}
                                                        </span>
                                                    </td>
                                                    <td>
                                                        <div className="action-row">
                                                            {canRollback && (
                                                                <button
                                                                    type="button"
                                                                    className="btn-sm"
                                                                    disabled={
                                                                        busyExecId === attempt.id
                                                                    }
                                                                    onClick={() =>
                                                                        handleAction(
                                                                            "rollback",
                                                                            attempt.id,
                                                                        )
                                                                    }
                                                                >
                                                                    Ajukan Rollback
                                                                </button>
                                                            )}
                                                            {canForward && (
                                                                <button
                                                                    type="button"
                                                                    className="btn-sm primary"
                                                                    disabled={
                                                                        busyExecId === attempt.id
                                                                    }
                                                                    onClick={() =>
                                                                        handleAction(
                                                                            "forward",
                                                                            attempt.id,
                                                                        )
                                                                    }
                                                                >
                                                                    Ajukan Forward
                                                                </button>
                                                            )}
                                                        </div>
                                                    </td>
                                                </tr>
                                            );
                                        })}
                                    </tbody>
                                </table>
                            </div>
                        )}

                        <div className="pagination-bar">
                            <span className="muted">
                                Menampilkan {visibleAttempts.length}
                                {visibleAttempts.length !== attempts.length
                                    ? ` dari ${attempts.length}`
                                    : ""}{" "}
                                entri mulai offset {offset}
                            </span>
                            <div className="action-row">
                                <button
                                    type="button"
                                    disabled={offset === 0}
                                    onClick={() =>
                                        onFilterChange(
                                            hideRollback,
                                            Math.max(offset - HISTORY_PAGE_SIZE, 0),
                                        )
                                    }
                                >
                                    Sebelumnya
                                </button>
                                <button
                                    type="button"
                                    disabled={!history?.has_more}
                                    onClick={() =>
                                        onFilterChange(hideRollback, offset + attempts.length)
                                    }
                                >
                                    Berikutnya
                                </button>
                            </div>
                        </div>
                    </>
                )}
            </section>
        </div>
    );
}
