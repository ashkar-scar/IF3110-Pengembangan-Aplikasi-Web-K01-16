import { type ApiErrorEnvelope, ApiRequestError } from "../types/auth";

// Menyimpan CSRF token di memori aja biar gak kesimpan di localStorage.
let currentCsrfToken = "";
let unauthorizedHandler: (() => void) | null = null;

export function setCsrfToken(token: string) {
    currentCsrfToken = token;
}

export function setUnauthorizedHandler(handler: () => void) {
    unauthorizedHandler = handler;
}

interface RequestOptions {
    method?: "GET" | "POST" | "PUT" | "DELETE";
    body?: unknown;
    skipUnauthorizedRedirect?: boolean;
}

export async function apiRequest<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const method = options.method ?? "GET";
    const headers: Record<string, string> = {};

    if (options.body !== undefined) {
        headers["Content-Type"] = "application/json";
    }

    // Mengirim X-CSRF-Token soalnya backend nolak request mutasi tanpa header ini.
    if (method !== "GET" && currentCsrfToken !== "") {
        headers["X-CSRF-Token"] = currentCsrfToken;
    }

    const response = await fetch(path, {
        method,
        headers,
        credentials: "same-origin",
        body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    });

    if (response.status === 204) {
        return undefined as T;
    }

    const text = await response.text();
    let payload: T | ApiErrorEnvelope | undefined;
    if (text) {
        try {
            payload = JSON.parse(text) as T | ApiErrorEnvelope;
        } catch {
            // Mengubah balasan HTML (misal 404 endpoint belum ada) jadi ApiRequestError biar gak dikira putus koneksi.
            throw new ApiRequestError(
                response.status,
                "INVALID_RESPONSE",
                `Server membalas format non-JSON (HTTP ${response.status}).`,
            );
        }
    }

    if (!response.ok) {
        const errBody = payload as ApiErrorEnvelope | undefined;
        const code = errBody?.error?.code ?? "REQUEST_FAILED";
        const message = errBody?.error?.message ?? "Permintaan gagal diproses.";

        if (response.status === 401 && !options.skipUnauthorizedRedirect && unauthorizedHandler) {
            unauthorizedHandler();
        }

        throw new ApiRequestError(response.status, code, message);
    }

    return payload as T;
}