import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = __ENV.BASE_URL || "http://127.0.0.1:8080";
const USERNAME = __ENV.K6_USER || "admin";
const PASSWORD = __ENV.K6_PASSWORD || "";
const TARGET_ID = __ENV.TARGET_ID || "1";
const TABLE_NAME = __ENV.TABLE_NAME || "customers";

export const options = {
    stages: [
        { duration: "15s", target: 10 },
        { duration: "30s", target: 25 },
        { duration: "15s", target: 0 },
    ],
    thresholds: {
        http_req_failed: ["rate<0.01"],
        http_req_duration: ["p(95)<500"],
    },
};

export function setup() {
    if (!PASSWORD) {
        throw new Error("Isi env K6_PASSWORD sebelum menjalankan skrip k6.");
    }

    const res = http.post(
        `${BASE_URL}/api/auth/login`,
        JSON.stringify({ username: USERNAME, password: PASSWORD }),
        {
            headers: {
                "Content-Type": "application/json",
                Origin: BASE_URL,
            },
        },
    );

    check(res, { "login status 200": (r) => r.status === 200 });
    const sessionCookie = res.cookies.yuzuki_akmal?.[0]?.value ?? "";
    const body = res.json();

    return {
        cookie: sessionCookie,
        csrfToken: body?.csrf_token ?? "",
    };
}

export default function (data) {
    // Menyisipkan cookie hasil setup supaya tiap VU gak terus-menerus login dan menuhin tabel sessions.
    const headers = {
        Cookie: `yuzuki_akmal=${data.cookie}`,
    };

    const dbRes = http.get(`${BASE_URL}/api/databases`, { headers });
    check(dbRes, { "list databases 200": (r) => r.status === 200 });

    const schemaRes = http.get(`${BASE_URL}/api/databases/${TARGET_ID}/schema`, { headers });
    check(schemaRes, { "schema snapshot 200": (r) => r.status === 200 });

    const rowsRes = http.get(
        `${BASE_URL}/api/databases/${TARGET_ID}/rows?schema=public&table=${TABLE_NAME}&limit=50&offset=0`,
        { headers },
    );
    check(rowsRes, { "rows page 200": (r) => r.status === 200 });

    sleep(0.5);
}
