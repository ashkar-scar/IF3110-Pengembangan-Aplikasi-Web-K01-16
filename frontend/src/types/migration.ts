export type ExecutionMode = "NORMAL" | "BATCH";
export type MigrationActionType = "UP" | "ROLLBACK" | "FORWARD" | "CANCEL_BATCH";
export type ReviewStatus =
    | "PENDING_REVIEW"
    | "APPROVED"
    | "REJECTED"
    | "ENQUEUED"
    | "COMPLETED"
    | "FAILED";
export type ApprovalDecision = "PENDING" | "APPROVED" | "REJECTED";
export type JobStatus =
    | "PENDING"
    | "RUNNING"
    | "SUCCESS"
    | "FAILED"
    | "PARTIAL"
    | "PARTIALLY_REVERTED"
    | "REVERTED"
    | "UNKNOWN";

export interface MigrationDraft {
    id: number;
    target_id: number;
    description: string;
    sql_up: string;
    sql_down: string;
    is_irreversible: boolean;
    irreversible_reason: string;
    execution_mode: ExecutionMode;
    batch_column: string;
    batch_size: number;
    revision: number;
    base_version: number;
    fingerprint: string;
    is_locked: boolean;
    created_at: string;
    updated_at: string;
}

export interface SaveDraftPayload {
    description: string;
    sql_up: string;
    sql_down: string;
    is_irreversible: boolean;
    irreversible_reason: string;
    execution_mode: ExecutionMode;
    batch_column: string;
    batch_size: number;
}

export interface PlanNode {
    title: string;
    estimate: string;
    condition?: string;
    children?: PlanNode[];
}

export interface ExplainResponse {
    driver: string;
    nodes?: PlanNode[];
    raw?: unknown;
}

export interface MigrationApproval {
    group_id: number;
    group_name: string;
    leader_id: number;
    leader_name: string;
    decision: ApprovalDecision;
    note?: string;
    decided_at: string | null;
}

// Menyamakan bentuk item GET /api/databases/{id}/reviews sama kontrak M2.
export interface MigrationRequest {
    id: number;
    target_id: number;
    draft_id: number;
    draft_revision: number;
    action_type: MigrationActionType;
    description: string;
    sql_up: string;
    sql_down: string;
    is_irreversible: boolean;
    irreversible_reason?: string;
    execution_mode: ExecutionMode;
    batch_column: string;
    batch_size: number;
    status: ReviewStatus;
    requested_by_name: string;
    can_current_user_approve: boolean;
    approvals: MigrationApproval[];
    job_id: string | null;
    created_at?: string;
}

export interface BatchCheckpoint {
    batch_no: number;
    cursor_start: string;
    cursor_end: string;
    rows_affected: number;
    status: "COMMITTED" | "REVERTED" | "FAILED";
}

export interface MigrationJob {
    id: string;
    request_id: number;
    target_id: number;
    action_type: MigrationActionType;
    execution_mode: ExecutionMode;
    status: JobStatus;
    total_batches: number;
    completed_batches: number;
    rows_processed: number;
    last_cursor: string;
    safe_error: string;
    can_retry: boolean;
    can_cancel_batch: boolean;
    checkpoints?: BatchCheckpoint[];
    created_at?: string;
    finished_at?: string | null;
}

export interface HistoryAttempt {
    id: string;
    target_id: number;
    draft_id: number;
    revision: number;
    description: string;
    sql: string;
    actor_name: string;
    action_type: MigrationActionType;
    version: number | null;
    status: "RUNNING" | "SUCCESS" | "FAILED" | "UNKNOWN";
    error: string;
    reverts_execution_id?: string | null;
    started_at: string;
    finished_at: string | null;
}

export interface HistoryResponse {
    attempts: HistoryAttempt[];
    has_more: boolean;
    can_rollback_exec_id?: string | null;
    can_forward_exec_id?: string | null;
}

export interface DiffChange {
    object: string;
    description: string;
    sql: string;
    destructive: boolean;
    manual: boolean;
}

export interface SchemaDiffResponse {
    changes: DiffChange[];
    sql: string;
    manual: boolean;
    fingerprint: string;
}
