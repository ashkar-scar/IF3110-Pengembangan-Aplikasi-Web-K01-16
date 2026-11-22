import type {
    AdminDatabaseTarget,
    DatabaseGrant,
    Group,
    SaveTargetPayload,
    SaveUserPayload,
    User,
} from "../types/admin";
import type { DatabasePermission } from "../types/database";
import { apiRequest } from "./client";

// Mengelompokkan endpoint CRUD user di admin_users.go & admin_edit.go.
export function fetchAdminUsers() {
    return apiRequest<User[]>("/api/admin/users");
}

export function createAdminUser(payload: SaveUserPayload) {
    return apiRequest<User>("/api/admin/users", {
        method: "POST",
        body: payload,
    });
}

export function updateAdminUser(id: number, payload: SaveUserPayload) {
    return apiRequest<User>(`/api/admin/users/${id}`, {
        method: "PUT",
        body: payload,
    });
}

// Mengelompokkan endpoint group dan anggotanya di admin_groups.go & admin_edit.go.
export function fetchAdminGroups() {
    return apiRequest<Group[]>("/api/admin/groups");
}

export function createAdminGroup(name: string) {
    return apiRequest<Group>("/api/admin/groups", {
        method: "POST",
        body: { name },
    });
}

export function renameAdminGroup(id: number, name: string) {
    return apiRequest<void>(`/api/admin/groups/${id}`, {
        method: "PUT",
        body: { name },
    });
}

export function deleteAdminGroup(id: number) {
    return apiRequest<void>(`/api/admin/groups/${id}`, {
        method: "DELETE",
    });
}

export function setGroupLeader(groupId: number, leaderId: number | null) {
    return apiRequest<Group>(`/api/admin/groups/${groupId}/leader`, {
        method: "PUT",
        body: { leader_id: leaderId },
    });
}

export function fetchGroupMembers(groupId: number) {
    return apiRequest<User[]>(`/api/admin/groups/${groupId}/members`);
}

export function addGroupMember(groupId: number, userId: number) {
    return apiRequest<void>(`/api/admin/groups/${groupId}/members`, {
        method: "POST",
        body: { user_id: userId },
    });
}

export function removeGroupMember(groupId: number, userId: number) {
    return apiRequest<void>(`/api/admin/groups/${groupId}/members/${userId}`, {
        method: "DELETE",
    });
}

// Mengelompokkan endpoint grant permission group ke target database.
export function fetchAdminGrants() {
    return apiRequest<DatabaseGrant[]>("/api/admin/grants");
}

export function setGroupGrant(
    groupId: number,
    targetId: number,
    permission: Exclude<DatabasePermission, "NONE">,
) {
    return apiRequest<void>(`/api/admin/groups/${groupId}/grants/${targetId}`, {
        method: "PUT",
        body: { permission },
    });
}

export function removeGroupGrant(groupId: number, targetId: number) {
    return apiRequest<void>(`/api/admin/groups/${groupId}/grants/${targetId}`, {
        method: "DELETE",
    });
}

// Mengelompokkan endpoint registry database target dan uji koneksinya.
export function fetchAdminTargets() {
    return apiRequest<AdminDatabaseTarget[]>("/api/admin/databases");
}

export function createAdminTarget(payload: SaveTargetPayload) {
    return apiRequest<AdminDatabaseTarget>("/api/admin/databases", {
        method: "POST",
        body: payload,
    });
}

export function updateAdminTarget(id: number, payload: SaveTargetPayload) {
    return apiRequest<AdminDatabaseTarget>(`/api/admin/databases/${id}`, {
        method: "PUT",
        body: payload,
    });
}

export function disableAdminTarget(id: number) {
    return apiRequest<void>(`/api/admin/databases/${id}`, {
        method: "DELETE",
    });
}

export function testAdminTarget(id: number) {
    return apiRequest<{ connected: boolean }>(`/api/admin/databases/${id}/test`, {
        method: "POST",
    });
}