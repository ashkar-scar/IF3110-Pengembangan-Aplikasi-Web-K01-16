export type SchemaBuilderAction =
    | "create_table"
    | "add_column"
    | "drop_column"
    | "rename_table"
    | "drop_table"
    | "create_index";

export interface BuilderColumn {
    name: string;
    type: string;
    nullable: boolean;
    defaultValue: string;
}

export interface SchemaBuilderInput {
    driver: string;
    action: SchemaBuilderAction;
    schema: string;
    tableName: string;
    newTableName: string;
    includeAutoId: boolean;
    columns: BuilderColumn[];
    columnName: string;
    columnType: string;
    nullable: boolean;
    defaultValue: string;
    indexName: string;
    isUniqueIndex: boolean;
}

export interface GeneratedDraftSql {
    description: string;
    sqlUp: string;
    sqlDown: string;
    isIrreversible: boolean;
    irreversibleReason: string;
}

const FORBIDDEN_IDENTIFIERS = new Set(["schema_migrations", "ruangdb_internal"]);

export const POSTGRES_COLUMN_TYPES = [
    "TEXT",
    "INTEGER",
    "BIGINT",
    "BOOLEAN",
    "NUMERIC(12,2)",
    "TIMESTAMPTZ",
    "UUID",
    "JSONB",
] as const;

export const SQLITE_COLUMN_TYPES = [
    "TEXT",
    "INTEGER",
    "REAL",
    "NUMERIC",
    "BLOB",
] as const;

export function getColumnTypesForDriver(driver: string): readonly string[] {
    return driver === "sqlite" ? SQLITE_COLUMN_TYPES : POSTGRES_COLUMN_TYPES;
}

function validateIdentifier(raw: string, label: string): string {
    const trimmed = raw.trim();
    if (!trimmed) {
        throw new Error(`${label} wajib diisi.`);
    }
    if (/[.;\r\n]/.test(trimmed)) {
        throw new Error(`${label} tidak boleh mengandung karakter titik, titik koma, atau baris baru.`);
    }
    if (FORBIDDEN_IDENTIFIERS.has(trimmed.toLowerCase())) {
        throw new Error(`${label} tidak boleh memakai nama internal (${trimmed}).`);
    }
    return trimmed;
}

function quoteIdent(ident: string): string {
    return `"${ident.replaceAll('"', '""')}"`;
}

function qualifyTable(driver: string, schema: string, tableName: string): { sql: string; label: string } {
    const cleanTable = validateIdentifier(tableName, "Nama tabel");
    if (driver === "sqlite") {
        return {
            sql: quoteIdent(cleanTable),
            label: cleanTable,
        };
    }

    const cleanSchema = validateIdentifier(schema || "public", "Nama schema");
    return {
        sql: `${quoteIdent(cleanSchema)}.${quoteIdent(cleanTable)}`,
        label: `${cleanSchema}.${cleanTable}`,
    };
}

function formatDefaultLiteral(raw: string): string {
    const trimmed = raw.trim();
    if (!trimmed) {
        return "";
    }
    if (/^'.*'$/.test(trimmed)) {
        return trimmed;
    }
    if (/^-?\d+(\.\d+)?$/.test(trimmed)) {
        return trimmed;
    }
    if (/^(TRUE|FALSE|NULL|CURRENT_TIMESTAMP|NOW\(\))$/i.test(trimmed)) {
        return trimmed.toUpperCase();
    }
    return `'${trimmed.replaceAll("'", "''")}'`;
}

export function buildSchemaMigrationSql(input: SchemaBuilderInput): GeneratedDraftSql {
    const isSqlite = input.driver === "sqlite";
    const schemaName = isSqlite ? "main" : (input.schema.trim() || "public");
    const target = qualifyTable(input.driver, schemaName, input.tableName);

    switch (input.action) {
        case "create_table": {
            const definitions: string[] = [];
            const seenColumns = new Set<string>();

            if (input.includeAutoId) {
                seenColumns.add("id");
                definitions.push(
                    isSqlite
                        ? '    "id" INTEGER PRIMARY KEY AUTOINCREMENT'
                        : '    "id" BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY',
                );
            }

            for (let i = 0; i < input.columns.length; i += 1) {
                const col = input.columns[i];
                if (!col.name.trim() && input.columns.length === 1 && input.includeAutoId) {
                    continue;
                }
                const cleanCol = validateIdentifier(col.name, `Nama kolom #${i + 1}`);
                const lower = cleanCol.toLowerCase();
                if (seenColumns.has(lower)) {
                    throw new Error(`Nama kolom "${cleanCol}" duplikat.`);
                }
                seenColumns.add(lower);

                const parts = [`    ${quoteIdent(cleanCol)} ${col.type.trim() || "TEXT"}`];
                if (!col.nullable) {
                    parts.push("NOT NULL");
                }
                if (col.defaultValue.trim()) {
                    parts.push(`DEFAULT ${formatDefaultLiteral(col.defaultValue)}`);
                }
                definitions.push(parts.join(" "));
            }

            if (definitions.length === 0) {
                throw new Error("Tambahkan minimal satu kolom atau aktifkan kolom id otomatis.");
            }

            return {
                description: `Buat tabel ${target.label}`,
                sqlUp: `CREATE TABLE IF NOT EXISTS ${target.sql} (\n${definitions.join(",\n")}\n);`,
                sqlDown: `DROP TABLE IF EXISTS ${target.sql};`,
                isIrreversible: false,
                irreversibleReason: "",
            };
        }

        case "add_column": {
            const cleanCol = validateIdentifier(input.columnName, "Nama kolom");
            if (isSqlite && !input.nullable && !input.defaultValue.trim()) {
                throw new Error("Di SQLite, kolom NOT NULL wajib punya nilai default atau centang Nullable.");
            }

            const parts = [
                `ALTER TABLE ${target.sql} ADD COLUMN ${quoteIdent(cleanCol)} ${input.columnType.trim() || "TEXT"}`,
            ];
            if (!input.nullable) {
                parts.push("NOT NULL");
            }
            if (input.defaultValue.trim()) {
                parts.push(`DEFAULT ${formatDefaultLiteral(input.defaultValue)}`);
            }

            const sqlDown = isSqlite
                ? `ALTER TABLE ${target.sql} DROP COLUMN ${quoteIdent(cleanCol)};`
                : `ALTER TABLE ${target.sql} DROP COLUMN IF EXISTS ${quoteIdent(cleanCol)};`;

            return {
                description: `Tambah kolom ${cleanCol} ke ${target.label}`,
                sqlUp: `${parts.join(" ")};`,
                sqlDown,
                isIrreversible: false,
                irreversibleReason: "",
            };
        }

        case "drop_column": {
            const cleanCol = validateIdentifier(input.columnName, "Nama kolom");
            const sqlUp = isSqlite
                ? `ALTER TABLE ${target.sql} DROP COLUMN ${quoteIdent(cleanCol)};`
                : `ALTER TABLE ${target.sql} DROP COLUMN IF EXISTS ${quoteIdent(cleanCol)};`;

            return {
                description: `Hapus kolom ${cleanCol} dari ${target.label}`,
                sqlUp,
                sqlDown: "",
                isIrreversible: true,
                irreversibleReason: "Penghapusan kolom bersifat permanen.",
            };
        }

        case "rename_table": {
            const cleanNew = validateIdentifier(input.newTableName, "Nama tabel baru");
            if (cleanNew.toLowerCase() === input.tableName.trim().toLowerCase()) {
                throw new Error("Nama tabel baru harus berbeda dengan nama tabel asal.");
            }
            const renamedTarget = qualifyTable(input.driver, schemaName, cleanNew);

            return {
                description: `Ubah nama tabel ${target.label} menjadi ${cleanNew}`,
                sqlUp: `ALTER TABLE ${target.sql} RENAME TO ${quoteIdent(cleanNew)};`,
                sqlDown: `ALTER TABLE ${renamedTarget.sql} RENAME TO ${quoteIdent(input.tableName.trim())};`,
                isIrreversible: false,
                irreversibleReason: "",
            };
        }

        case "drop_table": {
            return {
                description: `Hapus tabel ${target.label}`,
                sqlUp: `DROP TABLE IF EXISTS ${target.sql};`,
                sqlDown: "",
                isIrreversible: true,
                irreversibleReason: "Penghapusan tabel bersifat permanen.",
            };
        }

        case "create_index": {
            const cleanCol = validateIdentifier(input.columnName, "Nama kolom");
            const rawIdx = input.indexName.trim() || `idx_${input.tableName.trim()}_${cleanCol}`;
            const cleanIdx = validateIdentifier(rawIdx, "Nama index");
            const uniqueClause = input.isUniqueIndex ? "UNIQUE " : "";
            const dropTarget = isSqlite
                ? quoteIdent(cleanIdx)
                : `${quoteIdent(validateIdentifier(schemaName, "Nama schema"))}.${quoteIdent(cleanIdx)}`;

            return {
                description: `Buat ${input.isUniqueIndex ? "unique " : ""}index ${cleanIdx} pada ${target.label}(${cleanCol})`,
                sqlUp: `CREATE ${uniqueClause}INDEX IF NOT EXISTS ${quoteIdent(cleanIdx)} ON ${target.sql} (${quoteIdent(cleanCol)});`,
                sqlDown: `DROP INDEX IF EXISTS ${dropTarget};`,
                isIrreversible: false,
                irreversibleReason: "",
            };
        }
    }
}
