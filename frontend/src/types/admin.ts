import type { User } from "./auth";
import type { DatabasePermission } from "./database";

export type { User };

export interface Group {
    id: number;
    name: string;
    leader_id?: number | null;
    leader_username?: string | null;
    created_at: string;
}

export interface DatabaseGrant {
    group_id: number;
    database_id: number;
    permission: Exclude<DatabasePermission, "NONE">;
}

// Menyamakan field registry target di core/database_target.go (password gak pernah dikirim balik).
export interface AdminDatabaseTarget {
    id: number;
    name: string;
    driver: string;
    host: string;
    port: number;
    database: string;
    username: string;
    sslmode: string;
    is_active: boolean;
}

export interface SaveUserPayload {
    username: string;
    password: string;
    is_platform_admin: boolean;
    is_active: boolean;
}

export interface SaveTargetPayload {
    name: string;
    driver: string;
    host: string;
    port: number;
    database: string;
    username: string;
    password: string;
    sslmode: string;
    is_active: boolean;
    create_database?: boolean;
    creator_username?: string;
    creator_password?: string;
}