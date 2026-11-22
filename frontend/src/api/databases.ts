import type { DatabaseSchemaResponse, DatabaseTarget, TableRowsPage } from "../types/database";
import { apiRequest } from "./client";

export function fetchDatabases() {
    return apiRequest<DatabaseTarget[]>("/api/databases");
}

// Mengambil snapshot utuh sekali aja biar pindah-pindah schema & tabel gak buka koneksi target terus.
export function fetchDatabaseSchema(targetId: number) {
    return apiRequest<DatabaseSchemaResponse>(`/api/databases/${targetId}/schema`);
}

export function fetchTableRows(
    targetId: number,
    schema: string,
    table: string,
    limit = 50,
    offset = 0,
) {
    const params = new URLSearchParams({
        schema,
        table,
        limit: String(limit),
        offset: String(offset),
    });
    return apiRequest<TableRowsPage>(`/api/databases/${targetId}/rows?${params.toString()}`);
}