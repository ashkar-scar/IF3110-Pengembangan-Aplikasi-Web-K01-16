import http from "k6/http";
import exec from "k6/execution";
import { check, sleep } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

const BASE_URL = __ENV.BASE_URL || "http://127.0.0.1:8080";
const K6_USER = __ENV.K6_USER || "admin";
const K6_PASSWORD = __ENV.K6_PASSWORD || "";
const SECONDARY_USER = __ENV.SECONDARY_USER || "";
const SECONDARY_PASSWORD = __ENV.SECONDARY_PASSWORD || "";
const SAME_TARGET_ID = String(__ENV.SAME_TARGET_ID || "1");
const DIFF_TARGET_IDS_RAW = __ENV.DIFF_TARGET_IDS || "";
const API_TARGET_IDS_RAW = __ENV.API_TARGET_IDS || "1,2,3";

const STAGES = [
    { key: "1_normal", label: "Normal (Baseline)", endMs: 20000, durationSec: 20 },
    { key: "2_stress", label: "Stress Menengah", endMs: 45000, durationSec: 25 },
    { key: "3_peak", label: "Peak Stress", endMs: 70000, durationSec: 25 },
    { key: "4_cooldown", label: "Cooldown (Ramp Down)", endMs: 78000, durationSec: 8 },
    { key: "5_recovery", label: "Recovery (Steady Normal)", endMs: 103000, durationSec: 25 },
];

const apiReqDuration = new Trend("api_req_duration", true);
const apiReadDuration = new Trend("api_read_duration", true);
const apiEnqueueDuration = new Trend("api_enqueue_duration", true);
const apiReqCount = new Counter("api_req_count");
const apiErrorRate = new Rate("api_error_rate");

const jobEnqueueAccepted = new Rate("job_enqueue_accepted");
const jobExecSuccess = new Rate("job_exec_success");
const jobOccConflict = new Rate("job_occ_conflict");
const jobSystemError = new Rate("job_system_error");
const jobQueueWaitMs = new Trend("job_queue_wait_ms", true);
const jobExecDurationMs = new Trend("job_exec_duration_ms", true);
const jobTotalDurationMs = new Trend("job_total_duration_ms", true);
const jobsCompleted = new Counter("jobs_completed");

const stageThresholds = {};
for (const st of STAGES) {
    stageThresholds[`api_req_duration{stage:${st.key}}`] = [];
    stageThresholds[`api_read_duration{stage:${st.key}}`] = [];
    stageThresholds[`api_enqueue_duration{stage:${st.key}}`] = [];
    stageThresholds[`api_req_count{stage:${st.key}}`] = [];
    stageThresholds[`api_error_rate{stage:${st.key}}`] = [];
    stageThresholds[`job_enqueue_accepted{stage:${st.key}}`] = [];
    stageThresholds[`job_exec_success{stage:${st.key}}`] = [];
    stageThresholds[`job_occ_conflict{stage:${st.key}}`] = [];
    stageThresholds[`job_system_error{stage:${st.key}}`] = [];
    stageThresholds[`job_queue_wait_ms{stage:${st.key}}`] = [];
    stageThresholds[`job_exec_duration_ms{stage:${st.key}}`] = [];
    stageThresholds[`job_total_duration_ms{stage:${st.key}}`] = [];
    stageThresholds[`jobs_completed{stage:${st.key}}`] = [];
    for (const scope of ["same_db", "diff_db"]) {
        stageThresholds[`job_queue_wait_ms{stage:${st.key},scope:${scope}}`] = [];
        stageThresholds[`job_exec_duration_ms{stage:${st.key},scope:${scope}}`] = [];
        stageThresholds[`job_exec_success{stage:${st.key},scope:${scope}}`] = [];
        stageThresholds[`jobs_completed{stage:${st.key},scope:${scope}}`] = [];
    }
}

export const options = {
    setupTimeout: "120s",
    scenarios: {
        // Menguji akses baca API dan pengiriman job migration secara paralel dari beban normal hingga peak lalu pulih kembali.
        api_access: {
            executor: "ramping-vus",
            exec: "runApiAccess",
            startVUs: 4,
            stages: [
                { duration: "20s", target: 4 },
                { duration: "5s", target: 12 },
                { duration: "20s", target: 12 },
                { duration: "5s", target: 28 },
                { duration: "20s", target: 28 },
                { duration: "8s", target: 4 },
                { duration: "25s", target: 4 },
            ],
            gracefulRampDown: "5s",
        },
        migration_jobs: {
            executor: "ramping-vus",
            exec: "runMigrationJob",
            startVUs: 2,
            stages: [
                { duration: "20s", target: 2 },
                { duration: "5s", target: 5 },
                { duration: "20s", target: 5 },
                { duration: "5s", target: 8 },
                { duration: "20s", target: 8 },
                { duration: "8s", target: 2 },
                { duration: "25s", target: 2 },
            ],
            gracefulRampDown: "5s",
            gracefulStop: "20s",
        },
    },
    thresholds: {
        ...stageThresholds,
        api_read_duration: ["p(95)<2500"],
        api_req_duration: ["p(95)<2500"],
        "api_read_duration{stage:1_normal}": ["p(95)<500"],
        "api_read_duration{stage:5_recovery}": ["p(95)<750"],
        "http_req_failed{kind:api_read}": ["rate<0.01"],
        "http_req_failed{kind:api_enqueue}": ["rate<0.01"],
        api_error_rate: ["rate<0.01"],
        job_system_error: ["rate<0.01"],
        "job_queue_wait_ms{scope:same_db}": [],
        "job_queue_wait_ms{scope:diff_db}": [],
        "job_exec_duration_ms{scope:same_db}": [],
        "job_exec_duration_ms{scope:diff_db}": [],
        "job_exec_success{scope:same_db}": [],
        "job_exec_success{scope:diff_db}": [],
        "jobs_completed{scope:same_db}": [],
        "jobs_completed{scope:diff_db}": [],
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
            tags: { kind: "prep" },
        },
    );
    check(res, { [`login ${username} 200`]: (r) => r.status === 200 });
    return {
        username,
        cookie: res.cookies.yuzuki_akmal?.[0]?.value ?? "",
        csrf: res.json()?.csrf_token ?? "",
    };
}

function mutateHeaders(session) {
    return {
        "Content-Type": "application/json",
        Origin: BASE_URL,
        Cookie: `yuzuki_akmal=${session.cookie}`,
        "X-CSRF-Token": session.csrf,
    };
}

function readHeaders(session) {
    return {
        Cookie: `yuzuki_akmal=${session.cookie}`,
    };
}

function createAndApproveMigration(editor, approvers, targetId, label) {
    const editorHeaders = mutateHeaders(editor);
    const draftRes = http.post(
        `${BASE_URL}/api/databases/${targetId}/drafts`,
        JSON.stringify({
            description: `k6 stress ${label}`,
            sql_up: "SELECT 1",
            sql_down: "",
            is_irreversible: true,
            irreversible_reason: "Stress test query aman tanpa mutasi data",
            execution_mode: "NORMAL",
            batch_column: "",
            batch_size: 1000,
        }),
        { headers: editorHeaders, tags: { kind: "prep" } },
    );
    if (draftRes.status !== 200 && draftRes.status !== 201) {
        return null;
    }

    const draftId = draftRes.json()?.id;
    const reviewRes = http.post(
        `${BASE_URL}/api/databases/${targetId}/drafts/${draftId}/submit-review`,
        null,
        { headers: editorHeaders, tags: { kind: "prep" } },
    );
    if (reviewRes.status !== 200 && reviewRes.status !== 201) {
        return null;
    }

    const reqId = reviewRes.json()?.id;
    for (const leader of approvers) {
        http.post(
            `${BASE_URL}/api/databases/${targetId}/reviews/${reqId}/approve`,
            JSON.stringify({ note: "k6 pre-approved migration" }),
            { headers: mutateHeaders(leader), tags: { kind: "prep" } },
        );
    }
    return reqId;
}

function parseIdList(raw) {
    return raw
        .split(",")
        .map((s) => s.trim())
        .filter((s) => s !== "");
}

function currentStageKey(startMs) {
    const elapsed = Date.now() - startMs;
    for (const st of STAGES) {
        if (elapsed < st.endMs) {
            return st.key;
        }
    }
    return "5_recovery";
}

export function setup() {
    if (!K6_PASSWORD) {
        throw new Error("Isi env K6_PASSWORD sebelum menjalankan skrip stress k6.");
    }

    const editor = signIn(K6_USER, K6_PASSWORD);
    const approvers = [editor];
    if (SECONDARY_USER && SECONDARY_PASSWORD && SECONDARY_USER !== K6_USER) {
        approvers.push(signIn(SECONDARY_USER, SECONDARY_PASSWORD));
    }

    const listRes = http.get(`${BASE_URL}/api/databases`, {
        headers: readHeaders(editor),
        tags: { kind: "prep" },
    });
    const allTargets = listRes.status === 200 ? listRes.json() : [];
    const targetById = {};
    for (const t of allTargets) {
        targetById[String(t.id)] = t;
    }

    let diffTargets = parseIdList(DIFF_TARGET_IDS_RAW);
    if (diffTargets.length === 0) {
        diffTargets = allTargets
            .filter(
                (t) =>
                    t.driver === "postgres" &&
                    t.permission === "EDIT" &&
                    String(t.id) !== SAME_TARGET_ID,
            )
            .map((t) => String(t.id));
    }
    if (diffTargets.length === 0) {
        diffTargets = [SAME_TARGET_ID];
    }

    let apiTargets = parseIdList(API_TARGET_IDS_RAW).filter((id) => targetById[id]);
    if (apiTargets.length === 0) {
        apiTargets = allTargets.map((t) => String(t.id));
    }

    // Mengambil nama schema dan tabel nyata tiap target sekali saat setup agar request rows valid.
    const tableMap = {};
    for (const id of apiTargets) {
        const snapRes = http.get(`${BASE_URL}/api/databases/${id}/schema`, {
            headers: readHeaders(editor),
            tags: { kind: "prep" },
        });
        if (snapRes.status === 200) {
            const tables = snapRes.json()?.snapshot?.tables ?? [];
            if (tables.length > 0) {
                tableMap[id] = {
                    schema: tables[0].schema || (targetById[id]?.driver === "sqlite" ? "main" : "public"),
                    table: tables[0].name,
                };
            }
        }
    }

    // Menyiapkan request berstatus APPROVED awal sebelum fase beban dimulai.
    const initialApproved = {};
    const warmupTargets = Array.from(new Set([SAME_TARGET_ID, ...diffTargets]));
    for (const id of warmupTargets) {
        initialApproved[id] = createAndApproveMigration(editor, approvers, id, `warmup-${id}`);
    }

    return {
        editor,
        approvers,
        sameTargetId: SAME_TARGET_ID,
        diffTargets,
        apiTargets,
        tableMap,
        initialApproved,
        setupFinishedMs: Date.now(),
    };
}

function recordApiRead(res, stage) {
    const tags = { stage };
    const ok = res.status >= 200 && res.status < 400;
    apiReqDuration.add(res.timings.duration, tags);
    apiReadDuration.add(res.timings.duration, tags);
    apiReqCount.add(1, tags);
    apiErrorRate.add(!ok, tags);
    return ok;
}

export function runApiAccess(data) {
    const stage = currentStageKey(data.setupFinishedMs);
    const iterIdx = exec.scenario.iterationInTest;
    const targetId = data.apiTargets[iterIdx % data.apiTargets.length];
    const headers = readHeaders(data.editor);

    const listRes = http.get(`${BASE_URL}/api/databases`, {
        headers,
        tags: { kind: "api_read", stage },
    });
    check(listRes, { "api list databases ok": (r) => r.status === 200 });
    recordApiRead(listRes, stage);

    const schemaRes = http.get(`${BASE_URL}/api/databases/${targetId}/schema`, {
        headers,
        tags: { kind: "api_read", stage },
    });
    check(schemaRes, { "api schema snapshot ok": (r) => r.status === 200 });
    recordApiRead(schemaRes, stage);

    const meta = data.tableMap[targetId];
    if (meta) {
        const rowsRes = http.get(
            `${BASE_URL}/api/databases/${targetId}/rows?schema=${encodeURIComponent(meta.schema)}&table=${encodeURIComponent(meta.table)}&limit=50&offset=0`,
            {
                headers,
                tags: { kind: "api_read", stage },
            },
        );
        check(rowsRes, { "api rows ok": (r) => r.status === 200 });
        recordApiRead(rowsRes, stage);
    }

    const jobsRes = http.get(`${BASE_URL}/api/databases/${targetId}/jobs`, {
        headers,
        tags: { kind: "api_read", stage },
    });
    check(jobsRes, { "api list jobs ok": (r) => r.status === 200 });
    recordApiRead(jobsRes, stage);

    sleep(0.5);
}

export function runMigrationJob(data) {
    const stage = currentStageKey(data.setupFinishedMs);
    const seq = exec.scenario.iterationInTest;
    const isSameDb = seq % 2 === 0;
    const scope = isSameDb ? "same_db" : "diff_db";

    const targetId = isSameDb
        ? data.sameTargetId
        : data.diffTargets[Math.floor(seq / 2) % data.diffTargets.length];

    let reqId = null;
    if (seq === 0 || (scope === "diff_db" && seq <= data.diffTargets.length * 2)) {
        const candidate = data.initialApproved[targetId];
        if (candidate && seq <= data.diffTargets.length * 2) {
            reqId = candidate;
        }
    }
    if (!reqId) {
        reqId = createAndApproveMigration(
            data.editor,
            data.approvers,
            targetId,
            `${scope}-seq${seq}`,
        );
    }
    if (!reqId) {
        jobSystemError.add(1, { stage });
        sleep(0.5);
        return;
    }

    const stageTags = { stage };
    const bothTags = { stage, scope };

    const enqueueRes = http.post(
        `${BASE_URL}/api/databases/${targetId}/reviews/${reqId}/enqueue`,
        null,
        {
            headers: mutateHeaders(data.editor),
            tags: { kind: "api_enqueue", stage },
        },
    );
    const accepted = enqueueRes.status === 202;
    apiReqDuration.add(enqueueRes.timings.duration, stageTags);
    apiEnqueueDuration.add(enqueueRes.timings.duration, stageTags);
    apiReqCount.add(1, stageTags);
    apiErrorRate.add(!accepted, stageTags);
    jobEnqueueAccepted.add(accepted, bothTags);
    check(enqueueRes, { "enqueue accepted 202": () => accepted });

    const jobId = enqueueRes.json()?.job_id;
    if (!accepted || !jobId) {
        jobExecSuccess.add(0, bothTags);
        jobSystemError.add(1, bothTags);
        sleep(0.5);
        return;
    }

    // Memantau status job hingga status terminal; HTTP 202 di atas belum dihitung sebagai migration berhasil.
    let finalJob = null;
    const headers = readHeaders(data.editor);
    for (let poll = 0; poll < 80; poll++) {
        const pollRes = http.get(`${BASE_URL}/api/jobs/${jobId}`, {
            headers,
            tags: { kind: "job_poll", stage },
        });
        if (pollRes.status === 200) {
            const job = pollRes.json();
            if (
                job?.status === "SUCCESS" ||
                job?.status === "FAILED" ||
                job?.status === "PARTIAL" ||
                job?.status === "REVERTED" ||
                job?.status === "PARTIALLY_REVERTED"
            ) {
                finalJob = job;
                break;
            }
        }
        sleep(0.25);
    }

    if (!finalJob) {
        jobExecSuccess.add(0, bothTags);
        jobOccConflict.add(0, bothTags);
        jobSystemError.add(1, bothTags);
        return;
    }

    const succeeded = finalJob.status === "SUCCESS";
    const isOccConflict =
        finalJob.status === "FAILED" &&
        String(finalJob.safe_error || "").includes("versi database telah berubah");

    jobExecSuccess.add(succeeded, bothTags);
    jobOccConflict.add(isOccConflict, bothTags);
    jobSystemError.add(!succeeded && !isOccConflict, bothTags);
    jobsCompleted.add(1, bothTags);

    const createdMs = Date.parse(finalJob.created_at || "");
    const startedMs = Date.parse(finalJob.started_at || "");
    const finishedMs = Date.parse(finalJob.finished_at || "");

    if (!Number.isNaN(createdMs) && !Number.isNaN(startedMs)) {
        jobQueueWaitMs.add(Math.max(0, startedMs - createdMs), bothTags);
    }
    if (!Number.isNaN(startedMs) && !Number.isNaN(finishedMs)) {
        jobExecDurationMs.add(Math.max(0, finishedMs - startedMs), bothTags);
    }
    if (!Number.isNaN(createdMs) && !Number.isNaN(finishedMs)) {
        jobTotalDurationMs.add(Math.max(0, finishedMs - createdMs), bothTags);
    }

    sleep(0.2);
}

function fmt(val, digits = 2) {
    if (val === undefined || val === null || Number.isNaN(val)) return "-";
    return Number(val).toFixed(digits);
}

function metricValues(data, name) {
    return data.metrics?.[name]?.values ?? {};
}

export function handleSummary(data) {
    const lines = [
        "",
        "=== RINGKASAN STRESS TEST K6 PER TINGKAT BEBAN ===",
        "Stage           | API p50  | API p95  | API rps | API Err | QWait p50 | QWait p95 | JobDur p50 | JobDur p95 | Job/s | Job Succ | OCC Fail | Sys Err",
        "----------------|----------|----------|---------|---------|-----------|-----------|------------|------------|-------|----------|----------|--------",
    ];

    for (const st of STAGES) {
        const apiD = metricValues(data, `api_req_duration{stage:${st.key}}`);
        const apiC = metricValues(data, `api_req_count{stage:${st.key}}`);
        const apiE = metricValues(data, `api_error_rate{stage:${st.key}}`);
        const qW = metricValues(data, `job_queue_wait_ms{stage:${st.key}}`);
        const jD = metricValues(data, `job_exec_duration_ms{stage:${st.key}}`);
        const jC = metricValues(data, `jobs_completed{stage:${st.key}}`);
        const jS = metricValues(data, `job_exec_success{stage:${st.key}}`);
        const jO = metricValues(data, `job_occ_conflict{stage:${st.key}}`);
        const jErr = metricValues(data, `job_system_error{stage:${st.key}}`);

        const apiRps = (apiC.count ?? 0) / st.durationSec;
        const jobRps = (jC.count ?? 0) / st.durationSec;

        lines.push(
            `${st.key.padEnd(15)} | ` +
                `${(fmt(apiD.med) + "ms").padStart(8)} | ` +
                `${(fmt(apiD["p(95)"]) + "ms").padStart(8)} | ` +
                `${fmt(apiRps).padStart(7)} | ` +
                `${(fmt((apiE.rate ?? 0) * 100) + "%").padStart(7)} | ` +
                `${(fmt(qW.med) + "ms").padStart(9)} | ` +
                `${(fmt(qW["p(95)"]) + "ms").padStart(9)} | ` +
                `${(fmt(jD.med) + "ms").padStart(10)} | ` +
                `${(fmt(jD["p(95)"]) + "ms").padStart(10)} | ` +
                `${fmt(jobRps).padStart(5)} | ` +
                `${(fmt((jS.rate ?? 0) * 100) + "%").padStart(8)} | ` +
                `${(fmt((jO.rate ?? 0) * 100) + "%").padStart(8)} | ` +
                `${(fmt((jErr.rate ?? 0) * 100) + "%").padStart(7)}`,
        );
    }

    lines.push("");
    lines.push("=== DETAIL SAME_DB VS DIFF_DB PER TINGKAT BEBAN ===");
    for (const st of STAGES) {
        for (const scope of ["same_db", "diff_db"]) {
            const qW = metricValues(data, `job_queue_wait_ms{stage:${st.key},scope:${scope}}`);
            const jD = metricValues(data, `job_exec_duration_ms{stage:${st.key},scope:${scope}}`);
            const jC = metricValues(data, `jobs_completed{stage:${st.key},scope:${scope}}`);
            const jS = metricValues(data, `job_exec_success{stage:${st.key},scope:${scope}}`);
            lines.push(
                `${st.key.padEnd(11)} ${scope.padEnd(8)} -> jobs=${jC.count ?? 0}, ` +
                    `succ=${fmt((jS.rate ?? 0) * 100)}%, ` +
                    `qWait(p50/p95)=${fmt(qW.med)}/${fmt(qW["p(95)"])}ms, ` +
                    `exec(p50/p95)=${fmt(jD.med)}/${fmt(jD["p(95)"])}ms`,
            );
        }
    }

    const totalHttp = metricValues(data, "http_reqs");
    const apiRead = metricValues(data, "api_read_duration");
    const apiAll = metricValues(data, "api_req_duration");
    const apiErr = metricValues(data, "api_error_rate");
    const sysErr = metricValues(data, "job_system_error");
    const enqRate = metricValues(data, "job_enqueue_accepted");
    const execRate = metricValues(data, "job_exec_success");
    lines.push("");
    lines.push("=== RINGKASAN GLOBAL & THRESHOLD ===");
    lines.push(
        `http_reqs=${totalHttp.count ?? 0} (${fmt(totalHttp.rate)} req/s) | ` +
            `api_read p50=${fmt(apiRead.med)}ms p95=${fmt(apiRead["p(95)"])}ms | ` +
            `api_all p50=${fmt(apiAll.med)}ms p95=${fmt(apiAll["p(95)"])}ms | ` +
            `api_err=${fmt((apiErr.rate ?? 0) * 100)}% | ` +
            `enqueue_202=${fmt((enqRate.rate ?? 0) * 100)}% | ` +
            `job_success=${fmt((execRate.rate ?? 0) * 100)}% | ` +
            `sys_err=${fmt((sysErr.rate ?? 0) * 100)}%`,
    );

    for (const [name, metric] of Object.entries(data.metrics || {})) {
        if (!metric.thresholds) continue;
        for (const [expr, result] of Object.entries(metric.thresholds)) {
            if (!expr) continue;
            lines.push(`THRESHOLD ${result.ok ? "PASS" : "FAIL"}: ${name} [${expr}]`);
        }
    }
    lines.push("");

    const output = { stdout: `${lines.join("\n")}\n` };
    if (__ENV.SUMMARY_FILE) {
        output[__ENV.SUMMARY_FILE] = JSON.stringify(data, null, 2);
    }
    return output;
}
