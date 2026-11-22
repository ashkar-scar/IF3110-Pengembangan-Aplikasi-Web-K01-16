import type { AuthSession } from "../types/auth";
import { apiRequest } from "./client";

export function fetchCurrentUser() {
    return apiRequest<AuthSession>("/api/auth/current-user", {
        skipUnauthorizedRedirect: true,
    });
}

export function loginRequest(username: string, password: string) {
    return apiRequest<AuthSession>("/api/auth/login", {
        method: "POST",
        body: { username, password },
        skipUnauthorizedRedirect: true,
    });
}

export function logoutRequest() {
    return apiRequest<void>("/api/auth/logout", {
        method: "POST",
    });
}