import { useEffect, useState } from "react";
import { cancelBatchJob, fetchJobDetail, retryJob } from "../../api/migrations";
import { ApiRequestError } from "../../types/auth";
import type { JobStatus, MigrationJob } from "../../types/migration";

interface JobsPanelProps {
    canEdit: boolean;
    jobs: MigrationJob[];
    offset: number;
    hasMore: boolean;
    onOffsetChange: (nextOffset: number) => void;
    onRefresh: () => Promise<void>;
}

function jobBadgeClass(status: JobStatus) {
    if (status === "SUCCESS" || status === "REVERTED") return "badge success";
    if (status === "FAILED" || status === "UNKNOWN") return "badge danger";
    if (status === "RUNNING" || status === "PENDING") return "badge info";
    return "badge warning";
}

const JOBS_PAGE_SIZE = 200;

export function JobsPanel({
    canEdit,
    jobs,
    offset,
    hasMore,
    onOffsetChange,
    onRefresh,
}: JobsPanelProps) {
    const [selectedJob, setSelectedJob] = useState<MigrationJob | null>(null);
    const [busyId, setBusyId] = useState<string | null>(null);
    const [error, setError] = useState("");
    const [notice, setNotice] = useState("");

    const hasActiveJobs = jobs.some((j) => j.status === "PENDING" || j.status === "RUNNING");
    const selectedJobId = selectedJob?.id ?? null;

    // Melakukan polling tiap 1 detik cuma pas ada job yang lagi antre atau jalan.
    useEffect(() => {
        if (!hasActiveJobs) return;

        const timer = window.setInterval(() => {
            void onRefresh();
            if (selectedJobId) {
                void fetchJobDetail(selectedJobId)
                    .then((detail) => setSelectedJob(detail))
                    .catch(() => {});
            }
        }, 1000);

        return () => window.clearInterval(timer);
    }, [hasActiveJobs, selectedJobId, onRefresh]);

    async function handleOpenDetail(jobId: string) {
        setError("");
        try {
            const detail = await fetchJobDetail(jobId);
            setSelectedJob(detail);
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal memuat detail job.");
        }
    }

    async function handleRetry(jobId: string) {
        setError("");
        setNotice("");
        setBusyId(jobId);

        try {
            await retryJob(jobId);
            setNotice(`Job ${jobId} dimasukkan kembali ke antrean.`);
            await onRefresh();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal melakukan retry job.");
        } finally {
            setBusyId(null);
        }
    }

    async function handleCancelBatch(job: MigrationJob) {
        // Memperjelas sifat final Cancel Batch supaya tidak tertukar dengan Rollback yang bisa di-Forward.
        const confirmText =
            job.status === "SUCCESS"
                ? `Ajukan pembatalan batch (SQL down) untuk job ${job.id}?\n\nCatatan: Cancel Batch bersifat final dan tidak bisa di-Forward ulang. Jika masih ingin opsi Forward, gunakan Ajukan Rollback di tab Riwayat.`
                : `Ajukan pembatalan batch terbalik (SQL down) untuk job ${job.id}?\n\nCatatan: Cancel Batch bersifat final dan tidak bisa di-Forward ulang.`;
        if (!window.confirm(confirmText)) {
            return;
        }
        setError("");
        setNotice("");
        setBusyId(job.id);

        try {
            const res = await cancelBatchJob(job.id);
            setNotice(
                res.request_id
                    ? `Pengajuan cancel batch #${res.request_id} dibuat (perlu approval di tab Review & Approval).`
                    : `Permintaan cancel batch untuk job ${job.id} diproses.`,
            );
            await onRefresh();
        } catch (err) {
            setError(
                err instanceof ApiRequestError ? err.message : "Gagal membatalkan batch job.",
            );
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

            {jobs.length === 0 ? (
                <section className="card">
                    <p className="muted">Belum ada job eksekusi migration pada target ini.</p>
                </section>
            ) : (
                jobs.map((job) => {
                    const progressPercent =
                        job.total_batches > 0
                            ? Math.min(
                                  100,
                                  Math.round((job.completed_batches / job.total_batches) * 100),
                              )
                            : 0;

                    return (
                        <section key={job.id} className="card stack">
                            <div className="page-header">
                                <div>
                                    <h2>
                                        Job <code>{job.id}</code> (<code>{job.action_type}</code>)
                                    </h2>
                                    <p className="muted">
                                        Request #{job.request_id}, mode{" "}
                                        <code>{job.execution_mode}</code>, {job.rows_processed}{" "}
                                        baris diproses
                                        {job.last_cursor ? `, cursor ${job.last_cursor}` : ""}
                                    </p>
                                </div>
                                <span className={jobBadgeClass(job.status)}>{job.status}</span>
                            </div>

                            {job.execution_mode === "BATCH" && (
                                <div>
                                    <div className="pagination-bar">
                                        <span className="muted">
                                            Progress Batch: {job.completed_batches} /{" "}
                                            {job.total_batches} ({progressPercent}%)
                                        </span>
                                    </div>
                                    <div className="progress-track">
                                        <div
                                            className="progress-bar"
                                            style={{ inlineSize: `${progressPercent}%` }}
                                        />
                                    </div>
                                </div>
                            )}

                            {job.safe_error && (
                                <p className="alert error" role="alert">
                                    {job.safe_error}
                                </p>
                            )}

                            <div className="action-row">
                                <button
                                    type="button"
                                    className="btn-sm"
                                    onClick={() => handleOpenDetail(job.id)}
                                >
                                    Detail Checkpoint
                                </button>
                                {canEdit && job.can_retry && (
                                    <button
                                        type="button"
                                        className="btn-sm primary"
                                        disabled={busyId === job.id}
                                        onClick={() => handleRetry(job.id)}
                                    >
                                        {busyId === job.id
                                            ? "Memproses..."
                                            : "Retry dari Checkpoint"}
                                    </button>
                                )}
                                {canEdit && job.can_cancel_batch && (
                                    <button
                                        type="button"
                                        className="btn-sm danger"
                                        title="Membatalkan batch secara final (tidak bisa di-Forward ulang)"
                                        disabled={busyId === job.id}
                                        onClick={() => handleCancelBatch(job)}
                                    >
                                        Cancel Batch
                                    </button>
                                )}
                            </div>
                        </section>
                    );
                })
            )}

            {jobs.length > 0 && (offset > 0 || hasMore) && (
                <section className="card">
                    <div className="pagination-bar">
                        <span className="muted">
                            Menampilkan {jobs.length} job mulai offset {offset}
                        </span>
                        <div className="action-row">
                            <button
                                type="button"
                                disabled={offset === 0}
                                onClick={() => onOffsetChange(Math.max(offset - JOBS_PAGE_SIZE, 0))}
                            >
                                Sebelumnya
                            </button>
                            <button
                                type="button"
                                disabled={!hasMore}
                                onClick={() => onOffsetChange(offset + JOBS_PAGE_SIZE)}
                            >
                                Berikutnya
                            </button>
                        </div>
                    </div>
                </section>
            )}

            {selectedJob && (
                <section className="card stack">
                    <div className="page-header">
                        <h2>
                            Checkpoint Job <code>{selectedJob.id}</code>
                        </h2>
                        <button type="button" className="btn-sm" onClick={() => setSelectedJob(null)}>
                            Tutup
                        </button>
                    </div>

                    {!selectedJob.checkpoints || selectedJob.checkpoints.length === 0 ? (
                        <p className="muted">Belum ada checkpoint batch pada job ini.</p>
                    ) : (
                        <div className="table-scroll">
                            <table className="data-table">
                                <thead>
                                    <tr>
                                        <th>Batch</th>
                                        <th>Cursor</th>
                                        <th>Rows Affected</th>
                                        <th>Status</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {selectedJob.checkpoints.map((cp) => (
                                        <tr key={cp.batch_no}>
                                            <td>#{cp.batch_no}</td>
                                            <td>
                                                <code>
                                                    {cp.cursor_start} sampai {cp.cursor_end}
                                                </code>
                                            </td>
                                            <td>{cp.rows_affected}</td>
                                            <td>{cp.status}</td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )}
                </section>
            )}
        </div>
    );
}
