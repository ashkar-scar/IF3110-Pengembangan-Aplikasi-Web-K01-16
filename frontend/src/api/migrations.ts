import type {
    ExplainResponse,
    HistoryResponse,
    JobListResponse,
    MigrationDraft,
    MigrationJob,
    MigrationRequest,
    SaveDraftPayload,
    SchemaDiffResponse,
} from "../types/migration";
import { apiRequest } from "./client";

// Mengelompokkan endpoint draft, explain, dan pengajuan review UP.
export function fetchDrafts(targetId: number) {
    return apiRequest<MigrationDraft[]>(`/api/databases/${targetId}/drafts`);
}

export function createDraft(targetId: number, payload: SaveDraftPayload) {
    return apiRequest<MigrationDraft>(`/api/databases/${targetId}/drafts`, {
        method: "POST",
        body: payload,
    });
}

export function updateDraft(targetId: number, draftId: number, payload: SaveDraftPayload) {
    return apiRequest<MigrationDraft>(`/api/databases/${targetId}/drafts/${draftId}`, {
        method: "PUT",
        body: payload,
    });
}

export function explainSql(targetId: number, sql: string) {
    return apiRequest<ExplainResponse>(`/api/databases/${targetId}/explain`, {
        method: "POST",
        body: { sql },
    });
}

export function submitDraftReview(targetId: number, draftId: number) {
    return apiRequest<MigrationRequest>(
        `/api/databases/${targetId}/drafts/${draftId}/submit-review`,
        { method: "POST" },
    );
}

// Mengelompokkan endpoint approval multi-group dan enqueue ke worker.
export function fetchReviews(targetId: number) {
    return apiRequest<MigrationRequest[]>(`/api/databases/${targetId}/reviews`);
}

export function approveReview(targetId: number, reqId: number, note = "") {
    return apiRequest<MigrationRequest>(`/api/databases/${targetId}/reviews/${reqId}/approve`, {
        method: "POST",
        body: { note },
    });
}

export function rejectReview(targetId: number, reqId: number, note: string) {
    return apiRequest<MigrationRequest>(`/api/databases/${targetId}/reviews/${reqId}/reject`, {
        method: "POST",
        body: { note },
    });
}

export function enqueueReview(targetId: number, reqId: number) {
    return apiRequest<{ job_id: string; status: string }>(
        `/api/databases/${targetId}/reviews/${reqId}/enqueue`,
        // Mengirim body kosong supaya apiRequest menyertakan Content-Type application/json sesuai kontrak backend.
        { method: "POST", body: {} },
    );
}

// Mengelompokkan endpoint pantauan job async, retry, dan cancel batch.
export async function fetchDatabaseJobs(targetId: number, offset = 0): Promise<JobListResponse> {
    const query = offset > 0 ? `?offset=${offset}` : "";
    const data = await apiRequest<JobListResponse | MigrationJob[]>(
        `/api/databases/${targetId}/jobs${query}`,
    );
    if (Array.isArray(data)) {
        return { jobs: data, has_more: false };
    }
    return {
        jobs: data?.jobs ?? [],
        has_more: Boolean(data?.has_more),
    };
}

export function fetchJobDetail(jobId: string) {
    return apiRequest<MigrationJob>(`/api/jobs/${jobId}`);
}

export function retryJob(jobId: string) {
    return apiRequest<{ job_id: string; status: string }>(`/api/jobs/${jobId}/retry`, {
        method: "POST",
        body: {},
    });
}

export function cancelBatchJob(jobId: string) {
    return apiRequest<{ request_id?: number; job_id?: string }>(`/api/jobs/${jobId}/cancel-batch`, {
        method: "POST",
        body: {},
    });
}

// Mengelompokkan endpoint riwayat eksekusi dan rollback/forward linear.
export function fetchHistory(targetId: number, hideRollback: boolean, offset = 0) {
    const params = new URLSearchParams({
        hide_rollback: String(hideRollback),
        offset: String(offset),
    });
    return apiRequest<HistoryResponse>(`/api/databases/${targetId}/history?${params.toString()}`);
}

export function submitRollback(targetId: number, execId: string) {
    return apiRequest<MigrationRequest>(
        `/api/databases/${targetId}/history/${execId}/submit-rollback`,
        { method: "POST" },
    );
}

export function submitForward(targetId: number, execId: string) {
    return apiRequest<MigrationRequest>(
        `/api/databases/${targetId}/history/${execId}/submit-forward`,
        { method: "POST" },
    );
}

export function runSchemaDiff(
    targetId: number,
    payload: {
        source_id: number;
        current_schema: string;
        desired_schema: string;
        allow_destructive: boolean;
    },
) {
    return apiRequest<SchemaDiffResponse>(`/api/databases/${targetId}/diff`, {
        method: "POST",
        body: payload,
    });
}
