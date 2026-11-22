import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = __ENV.BASE_URL || "http://127.0.0.1:8080";
const TARGET_ID = __ENV.TARGET_ID || "1";
const EDITOR_USER = __ENV.EDITOR_USER || "admin";
const EDITOR_PASSWORD = __ENV.EDITOR_PASSWORD || "";
const LEADER_USER = __ENV.LEADER_USER || EDITOR_USER;
const LEADER_PASSWORD = __ENV.LEADER_PASSWORD || EDITOR_PASSWORD;

export const options = {
    vus: 5,
    iterations: 10,
    thresholds: {
        http_req_failed: ["rate<0.05"],
        http_req_duration: ["p(95)<1000"],
    },
};

function signIn(username, password) {
    const res = http.post(
        `${BASE_URL}/api/auth/login`,
        JSON.stringify({ username, password }),
        {
            headers: {
                "Content-Type": "application/json",
                Origin: BASE_URL,
            },
        },
    );
    check(res, { [`login ${username} 200`]: (r) => r.status === 200 });
    return {
        cookie: res.cookies.yuzuki_akmal?.[0]?.value ?? "",
        csrf: res.json()?.csrf_token ?? "",
    };
}

export function setup() {
    if (!EDITOR_PASSWORD) {
        throw new Error("Isi env EDITOR_PASSWORD (dan LEADER_PASSWORD jika terpisah).");
    }

    return {
        editor: signIn(EDITOR_USER, EDITOR_PASSWORD),
        leader: signIn(LEADER_USER, LEADER_PASSWORD),
    };
}

export default function (data) {
    const editorMutateHeaders = {
        "Content-Type": "application/json",
        Origin: BASE_URL,
        Cookie: `yuzuki_akmal=${data.editor.cookie}`,
        "X-CSRF-Token": data.editor.csrf,
    };

    const draftRes = http.post(
        `${BASE_URL}/api/databases/${TARGET_ID}/drafts`,
        JSON.stringify({
            description: `k6 load draft ${__VU}-${__ITER}`,
            sql_up: "SELECT 1",
            sql_down: "",
            is_irreversible: true,
            irreversible_reason: "Benchmark k6 read-safe",
            execution_mode: "NORMAL",
            batch_column: "",
            batch_size: 1000,
        }),
        { headers: editorMutateHeaders },
    );
    check(draftRes, { "create draft ok": (r) => r.status === 200 || r.status === 201 });
    if (draftRes.status >= 400) return;

    const draftId = draftRes.json()?.id;
    const reviewRes = http.post(
        `${BASE_URL}/api/databases/${TARGET_ID}/drafts/${draftId}/submit-review`,
        null,
        { headers: editorMutateHeaders },
    );
    check(reviewRes, { "submit review ok": (r) => r.status === 200 || r.status === 201 });
    if (reviewRes.status >= 400) return;

    const reqId = reviewRes.json()?.id;
    const leaderHeaders = {
        "Content-Type": "application/json",
        Origin: BASE_URL,
        Cookie: `yuzuki_akmal=${data.leader.cookie}`,
        "X-CSRF-Token": data.leader.csrf,
    };
    const approveRes = http.post(
        `${BASE_URL}/api/databases/${TARGET_ID}/reviews/${reqId}/approve`,
        JSON.stringify({ note: "k6 auto-approve" }),
        { headers: leaderHeaders },
    );
    check(approveRes, { "approve ok": (r) => r.status === 200 });

    const enqueueRes = http.post(
        `${BASE_URL}/api/databases/${TARGET_ID}/reviews/${reqId}/enqueue`,
        null,
        { headers: editorMutateHeaders },
    );
    check(enqueueRes, { "enqueue 202": (r) => r.status === 202 });

    const jobId = enqueueRes.json()?.job_id;
    if (!jobId) return;

    // Memantau job sampai selesai untuk mengukur throughput antrean + worker.
    for (let i = 0; i < 15; i++) {
        const jobRes = http.get(`${BASE_URL}/api/jobs/${jobId}`, {
            headers: { Cookie: `yuzuki_akmal=${data.editor.cookie}` },
        });
        const status = jobRes.json()?.status;
        if (status === "SUCCESS" || status === "FAILED" || status === "PARTIAL") {
            break;
        }
        sleep(0.5);
    }
}
