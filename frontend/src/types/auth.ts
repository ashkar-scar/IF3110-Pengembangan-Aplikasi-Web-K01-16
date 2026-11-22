// Menyamakan bentuk JSON User sama struct Go di internal/core/user.go.
export interface User {
    id: number;
    username: string;
    is_platform_admin: boolean;
    is_active: boolean;
    created_at: string;
}

export interface AuthSession {
    user: User;
    csrf_token: string;
}

export interface ApiErrorEnvelope {
    error: {
        code: string;
        message: string;
    };
}

export class ApiRequestError extends Error {
    status: number;
    code: string;

    constructor(status: number, code: string, message: string) {
        super(message);
        this.name = "ApiRequestError";
        this.status = status;
        this.code = code;
    }
}