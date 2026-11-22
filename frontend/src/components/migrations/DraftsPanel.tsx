import { useState, type FormEvent } from "react";
import { createDraft, explainSql, submitDraftReview, updateDraft } from "../../api/migrations";
import { ApiRequestError } from "../../types/auth";
import type { DatabaseSnapshot } from "../../types/database";
import type {
    ExecutionMode,
    ExplainResponse,
    MigrationDraft,
    PlanNode,
} from "../../types/migration";
import type { GeneratedDraftSql } from "../../utils/sqlBuilder";
import { SchemaBuilder } from "./SchemaBuilder";

interface DraftsPanelProps {
    targetId: number;
    driver: string;
    schemaSnapshot: DatabaseSnapshot | null;
    canEdit: boolean;
    drafts: MigrationDraft[];
    initialSql?: string;
    onRefresh: () => Promise<void>;
    onSubmittedReview: () => void;
}

function PlanTree({ nodes }: { nodes: PlanNode[] }) {
    return (
        <ul className="plan-tree">
            {nodes.map((node, index) => (
                <li key={`${node.title}-${index}`}>
                    <div>
                        <strong>{node.title}</strong>{" "}
                        <span className="muted">({node.estimate})</span>
                    </div>
                    {node.condition && <div className="muted">{node.condition}</div>}
                    {node.children && node.children.length > 0 && <PlanTree nodes={node.children} />}
                </li>
            ))}
        </ul>
    );
}

export function DraftsPanel({
    targetId,
    driver,
    schemaSnapshot,
    canEdit,
    drafts,
    initialSql = "",
    onRefresh,
    onSubmittedReview,
}: DraftsPanelProps) {
    const [selectedDraftId, setSelectedDraftId] = useState<number | null>(null);
    const [isLockedView, setIsLockedView] = useState(false);

    const [description, setDescription] = useState("");
    const [sqlUp, setSqlUp] = useState(initialSql);
    const [sqlDown, setSqlDown] = useState("");
    const [isIrreversible, setIsIrreversible] = useState(false);
    const [irreversibleReason, setIrreversibleReason] = useState("");
    const [executionMode, setExecutionMode] = useState<ExecutionMode>("NORMAL");
    const [batchColumn, setBatchColumn] = useState("id");
    const [batchSize, setBatchSize] = useState("1000");

    const [explainResult, setExplainResult] = useState<ExplainResponse | null>(null);
    const [isExplaining, setIsExplaining] = useState(false);
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [error, setError] = useState("");
    const [notice, setNotice] = useState("");

    function resetEditor() {
        setSelectedDraftId(null);
        setIsLockedView(false);
        setDescription("");
        setSqlUp("");
        setSqlDown("");
        setIsIrreversible(false);
        setIrreversibleReason("");
        setExecutionMode("NORMAL");
        setBatchColumn("id");
        setBatchSize("1000");
        setExplainResult(null);
    }

    function openDraft(draft: MigrationDraft) {
        setSelectedDraftId(draft.id);
        // Mengunci form kalau draft udah pernah diajukan biar revisinya gak ketimpa.
        setIsLockedView(draft.is_locked);
        setDescription(draft.description);
        setSqlUp(draft.sql_up);
        setSqlDown(draft.sql_down);
        setIsIrreversible(draft.is_irreversible);
        setIrreversibleReason(draft.irreversible_reason);
        setExecutionMode(draft.execution_mode);
        setBatchColumn(draft.batch_column || "id");
        setBatchSize(String(draft.batch_size || 1000));
        setExplainResult(null);
        setError("");
        setNotice("");
    }

    function handleApplyBuilderSql(generated: GeneratedDraftSql) {
        setDescription(generated.description);
        setSqlUp(generated.sqlUp);
        setSqlDown(generated.sqlDown);
        setIsIrreversible(generated.isIrreversible);
        setIrreversibleReason(generated.irreversibleReason);
        setExplainResult(null);
        setError("");
        setNotice("");
    }

    async function handleSaveDraft(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        if (!canEdit || isLockedView) return;

        setError("");
        setNotice("");
        setIsSubmitting(true);

        const payload = {
            description: description.trim(),
            sql_up: sqlUp,
            sql_down: isIrreversible ? "" : sqlDown,
            is_irreversible: isIrreversible,
            irreversible_reason: isIrreversible ? irreversibleReason.trim() : "",
            execution_mode: executionMode,
            batch_column: executionMode === "BATCH" ? batchColumn.trim() : "",
            batch_size: executionMode === "BATCH" ? Number(batchSize) || 1000 : 1000,
        };

        try {
            if (selectedDraftId === null) {
                const created = await createDraft(targetId, payload);
                setSelectedDraftId(created.id);
                setNotice(`Draft #${created.id} berhasil disimpan.`);
            } else {
                await updateDraft(targetId, selectedDraftId, payload);
                setNotice(`Draft #${selectedDraftId} berhasil diperbarui.`);
            }
            await onRefresh();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal menyimpan draft.");
        } finally {
            setIsSubmitting(false);
        }
    }

    async function handleExplain() {
        if (!sqlUp.trim()) return;
        setError("");
        setNotice("");
        setIsExplaining(true);

        try {
            const result = await explainSql(targetId, sqlUp);
            setExplainResult(result);
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal menjalankan EXPLAIN.");
        } finally {
            setIsExplaining(false);
        }
    }

    async function handleSubmitReview(draftId: number) {
        if (!window.confirm("Ajukan draft ini ke review? Setelah diajukan, SQL revisi ini terkunci.")) {
            return;
        }
        setError("");
        setNotice("");
        setIsSubmitting(true);

        try {
            await submitDraftReview(targetId, draftId);
            if (selectedDraftId === draftId) {
                setIsLockedView(true);
            }
            await onRefresh();
            onSubmittedReview();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal mengajukan review.");
        } finally {
            setIsSubmitting(false);
        }
    }

    const readOnly = !canEdit || isLockedView;

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

            {canEdit && (
                <section className="card">
                    <div className="page-header">
                        <div>
                            <h2>
                                {selectedDraftId === null
                                    ? "Buat Draft Migration"
                                    : isLockedView
                                      ? `Detail Draft #${selectedDraftId} (Terkunci)`
                                      : `Edit Draft #${selectedDraftId}`}
                            </h2>
                            {isLockedView && (
                                <p className="muted">
                                    Revisi ini sudah dikunci karena pernah diajukan/dieksekusi. Buat
                                    draft baru untuk perubahan SQL.
                                </p>
                            )}
                        </div>

                        {selectedDraftId !== null && (
                            <button type="button" onClick={resetEditor}>
                                + Draft Baru
                            </button>
                        )}
                    </div>

                    {!isLockedView && (
                        <SchemaBuilder
                            driver={driver}
                            schemaSnapshot={schemaSnapshot}
                            onApply={handleApplyBuilderSql}
                        />
                    )}

                    <form onSubmit={handleSaveDraft}>
                        <div className="form-grid">
                            <div className="field">
                                <label htmlFor="draft-desc">Deskripsi Migration</label>
                                <input
                                    id="draft-desc"
                                    type="text"
                                    placeholder="Misal: Tambah kolom tier ke tabel customers"
                                    value={description}
                                    onChange={(e) => setDescription(e.target.value)}
                                    disabled={readOnly}
                                    required
                                />
                            </div>

                            <div className="field">
                                <label htmlFor="draft-mode">Mode Eksekusi</label>
                                <select
                                    id="draft-mode"
                                    value={executionMode}
                                    onChange={(e) =>
                                        setExecutionMode(
                                            e.target.value === "BATCH" ? "BATCH" : "NORMAL",
                                        )
                                    }
                                    disabled={readOnly}
                                >
                                    <option value="NORMAL">NORMAL (1 transaksi)</option>
                                    <option value="BATCH">BATCH (bertahap pakai cursor)</option>
                                </select>
                            </div>
                        </div>

                        {executionMode === "BATCH" && (
                            <>
                                <div className="form-grid">
                                    <div className="field">
                                        <label htmlFor="batch-col">Kolom Cursor Batch</label>
                                        <input
                                            id="batch-col"
                                            type="text"
                                            placeholder="id"
                                            value={batchColumn}
                                            onChange={(e) => setBatchColumn(e.target.value)}
                                            disabled={readOnly}
                                            required
                                        />
                                    </div>

                                    <div className="field">
                                        <label htmlFor="batch-size">Ukuran Batch (1 sampai 100000)</label>
                                        <input
                                            id="batch-size"
                                            type="number"
                                            min={1}
                                            max={100000}
                                            value={batchSize}
                                            onChange={(e) => setBatchSize(e.target.value)}
                                            disabled={readOnly}
                                            required
                                        />
                                    </div>
                                </div>
                                <p className="muted">
                                    Gunakan parameter <code>$1</code> (cursor terakhir) dan{" "}
                                    <code>$2</code> (batas batch) pada query batch agar lolos
                                    validasi <code>sqlguard</code>.
                                </p>
                            </>
                        )}

                        <div className="field">
                            <label htmlFor="draft-sql-up">SQL Up (DML/DDL)</label>
                            <textarea
                                id="draft-sql-up"
                                className="code-input"
                                rows={6}
                                value={sqlUp}
                                onChange={(e) => setSqlUp(e.target.value)}
                                disabled={readOnly}
                                required
                            />
                        </div>

                        <div className="checkbox-row">
                            <label>
                                <input
                                    type="checkbox"
                                    checked={isIrreversible}
                                    onChange={(e) => setIsIrreversible(e.target.checked)}
                                    disabled={readOnly}
                                />
                                Irreversible (tidak bisa di-rollback)
                            </label>
                        </div>

                        {isIrreversible ? (
                            <div className="field">
                                <label htmlFor="irreversible-reason">Alasan Irreversible</label>
                                <input
                                    id="irreversible-reason"
                                    type="text"
                                    placeholder="Misal: Drop kolom permanen setelah arsip"
                                    value={irreversibleReason}
                                    onChange={(e) => setIrreversibleReason(e.target.value)}
                                    disabled={readOnly}
                                    required
                                />
                            </div>
                        ) : (
                            <div className="field">
                                <label htmlFor="draft-sql-down">
                                    SQL Down (untuk Rollback / Cancel Batch)
                                </label>
                                <textarea
                                    id="draft-sql-down"
                                    className="code-input"
                                    rows={4}
                                    value={sqlDown}
                                    onChange={(e) => setSqlDown(e.target.value)}
                                    disabled={readOnly}
                                />
                            </div>
                        )}

                        <div className="action-row">
                            {!readOnly && (
                                <button type="submit" className="primary" disabled={isSubmitting}>
                                    {isSubmitting
                                        ? "Menyimpan..."
                                        : selectedDraftId === null
                                          ? "Simpan Draft"
                                          : "Update Draft"}
                                </button>
                            )}
                            <button
                                type="button"
                                disabled={isExplaining || !sqlUp.trim()}
                                onClick={handleExplain}
                            >
                                {isExplaining ? "Menganalisis..." : "Cek Execution Plan"}
                            </button>
                            {selectedDraftId !== null && !isLockedView && (
                                <button
                                    type="button"
                                    disabled={isSubmitting}
                                    onClick={() => handleSubmitReview(selectedDraftId)}
                                >
                                    Ajukan Review
                                </button>
                            )}
                        </div>
                    </form>

                    {explainResult && (
                        <div className="details-box">
                            <h3>Execution Plan ({explainResult.driver})</h3>
                            {explainResult.nodes && explainResult.nodes.length > 0 ? (
                                <PlanTree nodes={explainResult.nodes} />
                            ) : (
                                <pre className="sql-preview">
                                    {JSON.stringify(explainResult.raw ?? explainResult, null, 2)}
                                </pre>
                            )}
                        </div>
                    )}
                </section>
            )}

            <section className="card">
                <h2>Daftar Draft ({drafts.length})</h2>
                {drafts.length === 0 ? (
                    <p className="muted">Belum ada draft migration pada target ini.</p>
                ) : (
                    <div className="table-scroll">
                        <table className="data-table">
                            <thead>
                                <tr>
                                    <th>ID</th>
                                    <th>Deskripsi</th>
                                    <th>Mode</th>
                                    <th>Status</th>
                                    <th>Aksi</th>
                                </tr>
                            </thead>
                            <tbody>
                                {drafts.map((draft) => (
                                    <tr key={draft.id}>
                                        <td>
                                            #{draft.id} r{draft.revision}
                                        </td>
                                        <td>
                                            <div>{draft.description}</div>
                                            {draft.is_irreversible && (
                                                <span className="badge danger">Irreversible</span>
                                            )}
                                        </td>
                                        <td>
                                            <code>{draft.execution_mode}</code>
                                        </td>
                                        <td>
                                            <span
                                                className={`badge ${draft.is_locked ? "warning" : "info"}`}
                                            >
                                                {draft.is_locked ? "Terkunci" : "Draft"}
                                            </span>
                                        </td>
                                        <td>
                                            <div className="action-row">
                                                <button
                                                    type="button"
                                                    className="btn-sm"
                                                    onClick={() => openDraft(draft)}
                                                >
                                                    {draft.is_locked || !canEdit ? "Lihat" : "Edit"}
                                                </button>
                                                {canEdit && !draft.is_locked && (
                                                    <button
                                                        type="button"
                                                        className="btn-sm primary"
                                                        disabled={isSubmitting}
                                                        onClick={() => handleSubmitReview(draft.id)}
                                                    >
                                                        Ajukan Review
                                                    </button>
                                                )}
                                            </div>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </section>
        </div>
    );
}
