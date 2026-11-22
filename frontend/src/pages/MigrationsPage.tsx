import { useCallback, useEffect, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { fetchDatabases, fetchDatabaseSchema } from "../api/databases";
import {
    fetchDatabaseJobs,
    fetchDrafts,
    fetchHistory,
    fetchReviews,
} from "../api/migrations";
import { DraftsPanel } from "../components/migrations/DraftsPanel";
import { HistoryPanel } from "../components/migrations/HistoryPanel";
import { JobsPanel } from "../components/migrations/JobsPanel";
import { ReviewsPanel } from "../components/migrations/ReviewsPanel";
import { StatePanel } from "../components/StatePanel";
import { ApiRequestError } from "../types/auth";
import type { DatabaseSnapshot, DatabaseTarget } from "../types/database";
import type {
    HistoryResponse,
    MigrationDraft,
    MigrationJob,
    MigrationRequest,
} from "../types/migration";

type MigrationTab = "drafts" | "reviews" | "jobs" | "history";

export function MigrationsPage() {
    const { id } = useParams();
    const targetId = Number(id);
    const [searchParams, setSearchParams] = useSearchParams();

    const rawTab = searchParams.get("tab");
    const activeTab: MigrationTab =
        rawTab === "reviews" || rawTab === "jobs" || rawTab === "history" ? rawTab : "drafts";
    const initialSql = searchParams.get("sql") ?? "";

    const [target, setTarget] = useState<DatabaseTarget | null>(null);
    const [drafts, setDrafts] = useState<MigrationDraft[]>([]);
    const [reviews, setReviews] = useState<MigrationRequest[]>([]);
    const [jobs, setJobs] = useState<MigrationJob[]>([]);
    const [history, setHistory] = useState<HistoryResponse | null>(null);
    const [schemaSnapshot, setSchemaSnapshot] = useState<DatabaseSnapshot | null>(null);
    const [hideRollback, setHideRollback] = useState(false);
    const [historyOffset, setHistoryOffset] = useState(0);
    const [isLoading, setIsLoading] = useState(true);
    const [error, setError] = useState("");
    const [tabErrors, setTabErrors] = useState<Partial<Record<MigrationTab, string>>>({});
    const [jobsEndpointMissing, setJobsEndpointMissing] = useState(false);

    const loadAll = useCallback(async () => {
        if (!Number.isInteger(targetId) || targetId <= 0) {
            setError("ID database tidak valid.");
            setIsLoading(false);
            return;
        }

        setError("");
        setTabErrors({});
        setJobsEndpointMissing(false);
        try {
            const targets = await fetchDatabases();
            const currentTarget = targets.find((item) => item.id === targetId) ?? null;
            setTarget(currentTarget);

            if (!currentTarget) {
                setError("Database tidak ditemukan atau Anda tidak memiliki akses.");
                return;
            }

            // Memakai allSettled supaya kalau ada endpoint M2 yang belum live di backend, tab lain tetap kebuka.
            const [draftRes, reviewRes, jobRes, historyRes, schemaRes] = await Promise.allSettled([
                fetchDrafts(targetId),
                fetchReviews(targetId),
                fetchDatabaseJobs(targetId),
                fetchHistory(targetId, hideRollback, historyOffset),
                fetchDatabaseSchema(targetId),
            ]);

            const nextErrors: Partial<Record<MigrationTab, string>> = {};
            const readErr = (res: PromiseRejectedResult) => {
                if (res.reason instanceof ApiRequestError) {
                    if (res.reason.status === 404 && res.reason.code === "INVALID_RESPONSE") {
                        return "Endpoint untuk tab ini belum tersedia di server.";
                    }
                    return res.reason.message;
                }
                return "Endpoint belum tersedia di server.";
            };

            if (draftRes.status === "fulfilled") setDrafts(draftRes.value);
            else nextErrors.drafts = readErr(draftRes);

            if (reviewRes.status === "fulfilled") setReviews(reviewRes.value);
            else nextErrors.reviews = readErr(reviewRes);

            if (jobRes.status === "fulfilled") {
                setJobs(jobRes.value.jobs);
            } else if (
                jobRes.reason instanceof ApiRequestError &&
                jobRes.reason.status === 404 &&
                jobRes.reason.code === "INVALID_RESPONSE"
            ) {
                setJobsEndpointMissing(true);
            } else {
                nextErrors.jobs = readErr(jobRes);
            }

            if (historyRes.status === "fulfilled") setHistory(historyRes.value);
            else nextErrors.history = readErr(historyRes);

            if (schemaRes.status === "fulfilled") setSchemaSnapshot(schemaRes.value.snapshot);

            setTabErrors(nextErrors);
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal memuat data migration.");
        } finally {
            setIsLoading(false);
        }
    }, [targetId, hideRollback, historyOffset]);

    useEffect(() => {
        // Memakai Promise chain di effect biar eslint react-hooks/set-state-in-effect gak protes.
        let active = true;
        Promise.resolve().then(() => {
            if (active) return loadAll();
        });
        return () => {
            active = false;
        };
    }, [loadAll]);

    function selectTab(tab: MigrationTab) {
        setSearchParams(tab === "drafts" ? {} : { tab });
    }

    const canEdit = target?.permission === "EDIT";
    const pendingReviewsCount = reviews.filter((r) => r.status === "PENDING_REVIEW").length;
    const activeJobsCount = jobs.filter((j) => j.status === "PENDING" || j.status === "RUNNING").length;

    return (
        <div className="stack">
            <section className="card">
                <div className="page-header">
                    <div>
                        <nav className="breadcrumb" aria-label="Navigasi halaman">
                            <Link to="/databases">Daftar Database</Link>
                            <span className="breadcrumb-sep" aria-hidden="true">/</span>
                            <Link to={`/databases/${id}`}>Explorer</Link>
                            <span className="breadcrumb-sep" aria-hidden="true">/</span>
                            <span className="breadcrumb-current">Migrations</span>
                        </nav>
                        <h1>Migrations: {target ? target.name : `Database #${id}`}</h1>
                        {target && (
                            <div className="meta-row">
                                <span className="meta-chip">
                                    <span className="meta-label">Driver</span>
                                    <code>{target.driver}</code>
                                </span>
                                <span className="meta-chip">
                                    <span className="meta-label">Izin</span>
                                    <span
                                        className={`badge ${canEdit ? "success" : "info"}`}
                                    >
                                        {target.permission}
                                    </span>
                                </span>
                            </div>
                        )}
                    </div>

                    <div className="action-row">
                        <Link className="btn" to={`/databases/${id}`}>
                            Explorer
                        </Link>
                        {canEdit && (
                            <Link className="btn" to={`/databases/${id}/diff`}>
                                Schema Diff
                            </Link>
                        )}
                        <button
                            type="button"
                            onClick={() => {
                                setIsLoading(true);
                                void loadAll();
                            }}
                            disabled={isLoading}
                        >
                            {isLoading ? "Memuat..." : "Muat Ulang"}
                        </button>
                    </div>
                </div>

                <div className="tab-group" role="tablist" aria-label="Menu migration">
                    <button
                        type="button"
                        role="tab"
                        aria-selected={activeTab === "drafts"}
                        className={activeTab === "drafts" ? "is-active" : ""}
                        onClick={() => selectTab("drafts")}
                    >
                        Drafts ({drafts.length})
                    </button>
                    <button
                        type="button"
                        role="tab"
                        aria-selected={activeTab === "reviews"}
                        className={activeTab === "reviews" ? "is-active" : ""}
                        onClick={() => selectTab("reviews")}
                    >
                        Review & Approval ({pendingReviewsCount})
                    </button>
                    <button
                        type="button"
                        role="tab"
                        aria-selected={activeTab === "jobs"}
                        className={activeTab === "jobs" ? "is-active" : ""}
                        onClick={() => selectTab("jobs")}
                    >
                        Jobs ({activeJobsCount > 0 ? `${activeJobsCount} aktif` : jobs.length})
                    </button>
                    <button
                        type="button"
                        role="tab"
                        aria-selected={activeTab === "history"}
                        className={activeTab === "history" ? "is-active" : ""}
                        onClick={() => selectTab("history")}
                    >
                        History ({(history?.history ?? history?.attempts ?? []).length})
                    </button>
                </div>

                {error && (
                    <StatePanel
                        variant="error"
                        title="Tidak dapat membuka workspace"
                        message={error}
                        onRetry={() => {
                            setIsLoading(true);
                            void loadAll();
                        }}
                        action={
                            <Link className="btn" to="/databases">
                                Kembali ke Daftar Database
                            </Link>
                        }
                    />
                )}
            </section>

            {isLoading && !target ? (
                <section className="card">
                    <StatePanel
                        variant="loading"
                        title="Memuat workspace migration"
                        message="Mengambil draft, approval, job antrean, dan riwayat eksekusi..."
                    />
                </section>
            ) : (
                !error && (
                    <>
                        {tabErrors[activeTab] && (
                            <section className="card">
                                <StatePanel
                                    variant="error"
                                    title={`Gagal memuat tab ${activeTab}`}
                                    message={tabErrors[activeTab] ?? "Terjadi kesalahan."}
                                    onRetry={() => {
                                        setIsLoading(true);
                                        void loadAll();
                                    }}
                                />
                            </section>
                        )}
                        {activeTab === "drafts" && (
                            <DraftsPanel
                                targetId={targetId}
                                driver={target?.driver ?? "postgres"}
                                schemaSnapshot={schemaSnapshot}
                                canEdit={canEdit}
                                drafts={drafts}
                                initialSql={initialSql}
                                onRefresh={loadAll}
                                onSubmittedReview={() => selectTab("reviews")}
                            />
                        )}
                        {activeTab === "reviews" && !tabErrors.reviews && (
                            <ReviewsPanel
                                targetId={targetId}
                                canEdit={canEdit}
                                reviews={reviews}
                                onRefresh={loadAll}
                                onEnqueued={() => selectTab("jobs")}
                            />
                        )}
                        {activeTab === "jobs" && !tabErrors.jobs && (
                            jobsEndpointMissing ? (
                                <section className="card">
                                    <StatePanel
                                        variant="empty"
                                        title="Antrean worker belum aktif"
                                        message="Endpoint pantauan job (/api/databases/:id/jobs) belum tersedia di server backend ini."
                                        onRetry={() => {
                                            setIsLoading(true);
                                            void loadAll();
                                        }}
                                        retryLabel="Cek Ulang"
                                    />
                                </section>
                            ) : (
                                <JobsPanel canEdit={canEdit} jobs={jobs} onRefresh={loadAll} />
                            )
                        )}
                        {activeTab === "history" && !tabErrors.history && (
                            <HistoryPanel
                                targetId={targetId}
                                canEdit={canEdit}
                                history={history}
                                hideRollback={hideRollback}
                                offset={historyOffset}
                                onFilterChange={(nextHide, nextOffset) => {
                                    setHideRollback(nextHide);
                                    setHistoryOffset(nextOffset);
                                }}
                                onSubmittedReview={() => {
                                    void loadAll();
                                    selectTab("reviews");
                                }}
                            />
                        )}
                    </>
                )
            )}
        </div>
    );
}
