import { useMemo, useState } from "react";
import type { DatabaseSnapshot } from "../../types/database";
import {
    buildSchemaMigrationSql,
    getColumnTypesForDriver,
    type BuilderColumn,
    type GeneratedDraftSql,
    type SchemaBuilderAction,
} from "../../utils/sqlBuilder";

interface SchemaBuilderProps {
    driver: string;
    schemaSnapshot: DatabaseSnapshot | null;
    onApply: (generated: GeneratedDraftSql) => void;
}

const ACTION_OPTIONS: { value: SchemaBuilderAction; label: string }[] = [
    { value: "create_table", label: "Buat Tabel Baru (CREATE TABLE)" },
    { value: "add_column", label: "Tambah Kolom (ADD COLUMN)" },
    { value: "drop_column", label: "Hapus Kolom (DROP COLUMN)" },
    { value: "rename_table", label: "Ubah Nama Tabel (RENAME TABLE)" },
    { value: "drop_table", label: "Hapus Tabel (DROP TABLE)" },
    { value: "create_index", label: "Buat Index (CREATE INDEX)" },
];

export function SchemaBuilder({ driver, schemaSnapshot, onApply }: SchemaBuilderProps) {
    const isSqlite = driver === "sqlite";
    const columnTypes = getColumnTypesForDriver(driver);

    const [action, setAction] = useState<SchemaBuilderAction>("create_table");
    const [schema, setSchema] = useState("public");
    const [tableName, setTableName] = useState("");
    const [newTableName, setNewTableName] = useState("");
    const [includeAutoId, setIncludeAutoId] = useState(true);
    const [columns, setColumns] = useState<BuilderColumn[]>([
        { name: "", type: columnTypes[0], nullable: false, defaultValue: "" },
    ]);
    const [columnName, setColumnName] = useState("");
    const [columnType, setColumnType] = useState<string>(columnTypes[0]);
    const [nullable, setNullable] = useState(true);
    const [defaultValue, setDefaultValue] = useState("");
    const [indexName, setIndexName] = useState("");
    const [isUniqueIndex, setIsUniqueIndex] = useState(false);
    const [builderError, setBuilderError] = useState("");
    const [appliedHint, setAppliedHint] = useState("");

    const availableSchemas = useMemo(() => {
        if (schemaSnapshot?.schemas && schemaSnapshot.schemas.length > 0) {
            return schemaSnapshot.schemas;
        }
        return ["public"];
    }, [schemaSnapshot]);

    const availableTables = useMemo(() => {
        const list = schemaSnapshot?.tables ?? [];
        if (isSqlite) {
            return list.map((t) => t.name);
        }
        const targetSchema = schema.trim() || "public";
        return list.filter((t) => t.schema === targetSchema).map((t) => t.name);
    }, [isSqlite, schema, schemaSnapshot]);

    const availableColumns = useMemo(() => {
        const list = schemaSnapshot?.tables ?? [];
        const matched = list.find((t) =>
            isSqlite
                ? t.name === tableName.trim()
                : t.schema === (schema.trim() || "public") && t.name === tableName.trim(),
        );
        return matched?.columns ? matched.columns.map((c) => c.name) : [];
    }, [isSqlite, schema, schemaSnapshot, tableName]);

    function updateColumn(index: number, patch: Partial<BuilderColumn>) {
        setColumns((prev) =>
            prev.map((col, idx) => (idx === index ? { ...col, ...patch } : col)),
        );
    }

    function addColumnRow() {
        setColumns((prev) => [
            ...prev,
            { name: "", type: columnTypes[0], nullable: true, defaultValue: "" },
        ]);
    }

    function removeColumnRow(index: number) {
        setColumns((prev) => prev.filter((_, idx) => idx !== index));
    }

    function handleGenerate() {
        setBuilderError("");
        setAppliedHint("");
        try {
            const effectiveType = columnTypes.includes(columnType) ? columnType : columnTypes[0];
            const effectiveColumns = columns.map((col) => ({
                ...col,
                type: columnTypes.includes(col.type) ? col.type : columnTypes[0],
            }));

            const generated = buildSchemaMigrationSql({
                driver,
                action,
                schema,
                tableName,
                newTableName,
                includeAutoId,
                columns: effectiveColumns,
                columnName,
                columnType: effectiveType,
                nullable,
                defaultValue,
                indexName,
                isUniqueIndex,
            });
            onApply(generated);
            setAppliedHint("SQL Up & SQL Down diisikan ke form draft di bawah.");
        } catch (err) {
            setBuilderError(err instanceof Error ? err.message : "Gagal menyusun SQL.");
        }
    }

    return (
        <details className="details-box" style={{ marginBlockStart: 0, marginBlockEnd: "1rem" }}>
            <summary>
                Bantu Susun Skema via GUI ({isSqlite ? "SQLite" : "PostgreSQL"})
            </summary>

            <div className="stack" style={{ marginTop: "0.9rem" }}>
                <p className="muted" style={{ margin: 0 }}>
                    Pilih aksi perubahan skema tanpa perlu mengetik sintaks DDL manual. Hasilnya
                    tetap masuk ke form draft agar bisa dicek dan disetujui lewat alur review.
                </p>

                <div className="form-grid">
                    <div className="field">
                        <label htmlFor="sb-action">Jenis Perubahan</label>
                        <select
                            id="sb-action"
                            value={action}
                            onChange={(e) => {
                                setAction(e.target.value as SchemaBuilderAction);
                                setBuilderError("");
                                setAppliedHint("");
                            }}
                        >
                            {ACTION_OPTIONS.map((opt) => (
                                <option key={opt.value} value={opt.value}>
                                    {opt.label}
                                </option>
                            ))}
                        </select>
                    </div>

                    {!isSqlite && (
                        <div className="field">
                            <label htmlFor="sb-schema">Schema</label>
                            <input
                                id="sb-schema"
                                type="text"
                                list="sb-schema-list"
                                value={schema}
                                onChange={(e) => setSchema(e.target.value)}
                                placeholder="public"
                            />
                            <datalist id="sb-schema-list">
                                {availableSchemas.map((s) => (
                                    <option key={s} value={s} />
                                ))}
                            </datalist>
                        </div>
                    )}

                    <div className="field">
                        <label htmlFor="sb-table">
                            {action === "create_table" ? "Nama Tabel Baru" : "Nama Tabel"}
                        </label>
                        <input
                            id="sb-table"
                            type="text"
                            list={action === "create_table" ? undefined : "sb-table-list"}
                            value={tableName}
                            onChange={(e) => setTableName(e.target.value)}
                            placeholder="Misal: invoices"
                        />
                        {action !== "create_table" && (
                            <datalist id="sb-table-list">
                                {availableTables.map((t) => (
                                    <option key={t} value={t} />
                                ))}
                            </datalist>
                        )}
                    </div>

                    {action === "rename_table" && (
                        <div className="field">
                            <label htmlFor="sb-new-table">Nama Tabel Baru</label>
                            <input
                                id="sb-new-table"
                                type="text"
                                value={newTableName}
                                onChange={(e) => setNewTableName(e.target.value)}
                                placeholder="Misal: invoices_archive"
                            />
                        </div>
                    )}
                </div>

                {action === "create_table" && (
                    <div className="stack">
                        <label className="checkbox-inline">
                            <input
                                type="checkbox"
                                checked={includeAutoId}
                                onChange={(e) => setIncludeAutoId(e.target.checked)}
                            />
                            Sertakan kolom primary key <code>id</code> otomatis
                        </label>

                        {columns.map((col, idx) => (
                            <div key={idx} className="schema-col-row">
                                <input
                                    type="text"
                                    aria-label={`Nama kolom ${idx + 1}`}
                                    placeholder="Nama kolom (misal: title)"
                                    value={col.name}
                                    onChange={(e) => updateColumn(idx, { name: e.target.value })}
                                />
                                <select
                                    aria-label={`Tipe kolom ${idx + 1}`}
                                    value={columnTypes.includes(col.type) ? col.type : columnTypes[0]}
                                    onChange={(e) => updateColumn(idx, { type: e.target.value })}
                                >
                                    {columnTypes.map((typeName) => (
                                        <option key={typeName} value={typeName}>
                                            {typeName}
                                        </option>
                                    ))}
                                </select>
                                <input
                                    type="text"
                                    aria-label={`Default kolom ${idx + 1}`}
                                    placeholder="Default (opsional)"
                                    value={col.defaultValue}
                                    onChange={(e) =>
                                        updateColumn(idx, { defaultValue: e.target.value })
                                    }
                                />
                                <label className="checkbox-inline">
                                    <input
                                        type="checkbox"
                                        checked={col.nullable}
                                        onChange={(e) =>
                                            updateColumn(idx, { nullable: e.target.checked })
                                        }
                                    />
                                    Nullable
                                </label>
                                <button
                                    type="button"
                                    className="btn-sm"
                                    onClick={() => removeColumnRow(idx)}
                                    disabled={columns.length === 1 && !includeAutoId}
                                >
                                    Hapus
                                </button>
                            </div>
                        ))}

                        <div className="action-row">
                            <button type="button" className="btn-sm" onClick={addColumnRow}>
                                + Tambah Kolom
                            </button>
                        </div>
                    </div>
                )}

                {action === "add_column" && (
                    <div className="form-grid">
                        <div className="field">
                            <label htmlFor="sb-col-name">Nama Kolom Baru</label>
                            <input
                                id="sb-col-name"
                                type="text"
                                value={columnName}
                                onChange={(e) => setColumnName(e.target.value)}
                                placeholder="Misal: status"
                            />
                        </div>
                        <div className="field">
                            <label htmlFor="sb-col-type">Tipe Data</label>
                            <select
                                id="sb-col-type"
                                value={columnTypes.includes(columnType) ? columnType : columnTypes[0]}
                                onChange={(e) => setColumnType(e.target.value)}
                            >
                                {columnTypes.map((typeName) => (
                                    <option key={typeName} value={typeName}>
                                        {typeName}
                                    </option>
                                ))}
                            </select>
                        </div>
                        <div className="field">
                            <label htmlFor="sb-col-default">Default Value (opsional)</label>
                            <input
                                id="sb-col-default"
                                type="text"
                                value={defaultValue}
                                onChange={(e) => setDefaultValue(e.target.value)}
                                placeholder="Misal: active atau 0"
                            />
                        </div>
                        <div className="field" style={{ justifyContent: "flex-end" }}>
                            <label className="checkbox-inline">
                                <input
                                    type="checkbox"
                                    checked={nullable}
                                    onChange={(e) => setNullable(e.target.checked)}
                                />
                                Boleh NULL (Nullable)
                            </label>
                        </div>
                    </div>
                )}

                {action === "drop_column" && (
                    <div className="form-grid">
                        <div className="field">
                            <label htmlFor="sb-drop-col">Nama Kolom yang Dihapus</label>
                            <input
                                id="sb-drop-col"
                                type="text"
                                list="sb-col-list"
                                value={columnName}
                                onChange={(e) => setColumnName(e.target.value)}
                                placeholder="Misal: legacy_code"
                            />
                            <datalist id="sb-col-list">
                                {availableColumns.map((c) => (
                                    <option key={c} value={c} />
                                ))}
                            </datalist>
                        </div>
                    </div>
                )}

                {action === "create_index" && (
                    <div className="form-grid">
                        <div className="field">
                            <label htmlFor="sb-idx-col">Kolom yang Di-index</label>
                            <input
                                id="sb-idx-col"
                                type="text"
                                list="sb-col-list"
                                value={columnName}
                                onChange={(e) => setColumnName(e.target.value)}
                                placeholder="Misal: email"
                            />
                            <datalist id="sb-col-list">
                                {availableColumns.map((c) => (
                                    <option key={c} value={c} />
                                ))}
                            </datalist>
                        </div>
                        <div className="field">
                            <label htmlFor="sb-idx-name">Nama Index (opsional)</label>
                            <input
                                id="sb-idx-name"
                                type="text"
                                value={indexName}
                                onChange={(e) => setIndexName(e.target.value)}
                                placeholder="Otomatis: idx_tabel_kolom"
                            />
                        </div>
                        <div className="field" style={{ justifyContent: "flex-end" }}>
                            <label className="checkbox-inline">
                                <input
                                    type="checkbox"
                                    checked={isUniqueIndex}
                                    onChange={(e) => setIsUniqueIndex(e.target.checked)}
                                />
                                Unique Index
                            </label>
                        </div>
                    </div>
                )}

                {builderError && <div className="alert error">{builderError}</div>}
                {appliedHint && <div className="alert success">{appliedHint}</div>}

                <div className="action-row">
                    <button type="button" onClick={handleGenerate}>
                        Terapkan ke Form Draft
                    </button>
                </div>
            </div>
        </details>
    );
}
