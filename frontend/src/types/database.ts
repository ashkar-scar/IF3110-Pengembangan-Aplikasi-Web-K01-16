export type DatabasePermission = "NONE" | "VIEW" | "EDIT";

// Menyamakan struktur target database sama response /api/databases di explorer.go.
export interface DatabaseTarget {
    id: number;
    name: string;
    driver: string;
    permission: DatabasePermission;
}

export interface TableColumn {
    name: string;
    type: string;
    nullable: boolean;
    default: string | null;
    identity?: string;
    generated?: string;
}

export interface TableConstraint {
    name: string;
    kind: string;
    definition: string;
    columns: string[] | null;
}

export interface TableIndex {
    name: string;
    definition: string;
    columns: string[] | null;
    unique: boolean;
    constraint: boolean;
    method: string;
    predicate?: string;
}

export interface DatabaseTable {
    schema: string;
    name: string;
    definition?: string;
    columns: TableColumn[] | null;
    constraints: TableConstraint[] | null;
    indexes: TableIndex[] | null;
}

export interface DatabaseSnapshot {
    driver: string;
    schemas: string[];
    tables: DatabaseTable[] | null;
}

export interface DatabaseSchemaResponse {
    snapshot: DatabaseSnapshot;
    fingerprint: string;
    version: string;
}

export interface TableRowsPage {
    columns: string[];
    rows: unknown[][];
    limit: number;
    offset: number;
    has_more: boolean;
}