import { useEffect, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { fetchDatabases, fetchDatabaseSchema, fetchTableRows } from "../api/databases";
import { StatePanel } from "../components/StatePanel";
import { ApiRequestError } from "../types/auth";
import type {
    DatabaseSchemaResponse,
    DatabaseTarget,
    TableRowsPage,
} from "../types/database";

const PAGE_LIMIT = 50;

interface RowsCache {
    tableKey: string;
    fullKey: string;
    page: TableRowsPage;
}

function formatConstraintKind(kind: string) {
    switch (kind) {
        case "p":
            return "PRIMARY KEY";
        case "f":
            return "FOREIGN KEY";
        case "u":
            return "UNIQUE";
        case "c":
            return "CHECK";
        default:
            return kind;
    }
}

function formatCellValue(value: unknown) {
    if (value === null || value === undefined) {
        return <span className="null-pill">NULL</span>;
    }
    if (typeof value === "object") {
        return JSON.stringify(value);
    }
    return String(value);
}

export function ExplorerPage() {
    const { id } = useParams();
    const targetId = Number(id);
    const [searchParams, setSearchParams] = useSearchParams();

    const [target, setTarget] = useState<DatabaseTarget | null>(null);
    const [schemaData, setSchemaData] = useState<DatabaseSchemaResponse | null>(null);
    const [schemaError, setSchemaError] = useState("");
    const [isLoadingSchema, setIsLoadingSchema] = useState(true);
    const [reloadTick, setReloadTick] = useState(0);

    const [tableFilter, setTableFilter] = useState("");
    const [rowsCache, setRowsCache] = useState<RowsCache | null>(null);
    const [rowsError, setRowsError] = useState("");
    const [isLoadingRows, setIsLoadingRows] = useState(false);

    // Menyimpan schema, tabel, tab, dan offset di URL biar gak hilang pas halaman di-refresh.
    const requestedSchema = searchParams.get("schema") ?? "";
    const requestedTable = searchParams.get("table") ?? "";
    const activeTab = searchParams.get("tab") === "rows" ? "rows" : "structure";
    const parsedOffset = Number(searchParams.get("offset") ?? "0");
    const offset = Number.isInteger(parsedOffset) && parsedOffset >= 0 ? parsedOffset : 0;

    // Memuat info target dan snapshot skema sekaligus pas halaman dibuka atau di-reload.
    useEffect(() => {
        let active = true;

        Promise.resolve().then(() => {
            if (!active) return;
            if (!Number.isInteger(targetId) || targetId <= 0) {
                setSchemaError("ID database tidak valid.");
                setIsLoadingSchema(false);
                return;
            }

            setIsLoadingSchema(true);
            setSchemaError("");

            void Promise.all([fetchDatabases(), fetchDatabaseSchema(targetId)])
                .then(([targets, schemaResponse]) => {
                    if (!active) return;
                    setTarget(targets.find((item) => item.id === targetId) ?? null);
                    setSchemaData(schemaResponse);
                })
                .catch((err) => {
                    if (!active) return;
                    if (err instanceof ApiRequestError) {
                        setSchemaError(err.message);
                    } else {
                        setSchemaError("Gagal memuat skema database.");
                    }
                })
                .finally(() => {
                    if (active) setIsLoadingSchema(false);
                });
        });

        return () => {
            active = false;
        };
    }, [targetId, reloadTick]);

    const schemas = schemaData?.snapshot.schemas ?? [];
    const activeSchema = schemas.includes(requestedSchema)
        ? requestedSchema
        : schemas.includes("public")
            ? "public"
            : (schemas[0] ?? "");

    const tablesInSchema = (schemaData?.snapshot.tables ?? []).filter(
        (item) => item.schema === activeSchema,
    );
    const filteredTables = tablesInSchema.filter((item) =>
        item.name.toLowerCase().includes(tableFilter.trim().toLowerCase()),
    );

    const selectedTable =
        tablesInSchema.find((item) => item.name === requestedTable) ?? tablesInSchema[0] ?? null;
    const selectedTableName = selectedTable?.name ?? "";

    const currentTableKey = selectedTable ? `${targetId}:${activeSchema}:${selectedTable.name}` : "";
    const desiredRowsKey = currentTableKey ? `${currentTableKey}:${offset}:${reloadTick}` : "";
    const visibleRowsPage = rowsCache?.tableKey === currentTableKey ? rowsCache.page : null;

    // Meng-cache baris tabel aktif biar bolak-balik tab Struktur dan Data Baris gak fetch ulang.
    useEffect(() => {
        if (activeTab !== "rows" || !selectedTableName || !activeSchema || !Number.isInteger(targetId)) {
            return;
        }
        if (rowsCache?.fullKey === desiredRowsKey) {
            return;
        }

        let active = true;

        Promise.resolve().then(() => {
            if (!active) return;
            setIsLoadingRows(true);
            setRowsError("");

            void fetchTableRows(targetId, activeSchema, selectedTableName, PAGE_LIMIT, offset)
                .then((page) => {
                    if (!active) return;
                    setRowsCache({
                        tableKey: currentTableKey,
                        fullKey: desiredRowsKey,
                        page,
                    });
                })
                .catch((err) => {
                    if (!active) return;
                    if (err instanceof ApiRequestError) {
                        setRowsError(err.message);
                    } else {
                        setRowsError("Gagal memuat isi tabel.");
                    }
                })
                .finally(() => {
                    if (active) setIsLoadingRows(false);
                });
        });

        return () => {
            active = false;
        };
    }, [
        targetId,
        activeSchema,
        selectedTableName,
        activeTab,
        offset,
        reloadTick,
        currentTableKey,
        desiredRowsKey,
        rowsCache?.fullKey,
    ]);

    function updateExplorerParams(next: {
        schema?: string;
        table?: string;
        tab?: "structure" | "rows";
        offset?: number;
    }) {
        const params = new URLSearchParams();
        const nextSchema = next.schema ?? activeSchema;
        const nextTable = next.table ?? selectedTable?.name ?? "";
        const nextTab = next.tab ?? activeTab;
        const nextOffset = next.offset ?? 0;

        if (nextSchema) params.set("schema", nextSchema);
        if (nextTable) params.set("table", nextTable);
        if (nextTab === "rows") params.set("tab", "rows");
        if (nextTab === "rows" && nextOffset > 0) params.set("offset", String(nextOffset));
        setSearchParams(params);
    }

    function handleSchemaChange(nextSchema: string) {
        const firstTable =
            (schemaData?.snapshot.tables ?? []).find((item) => item.schema === nextSchema)?.name ??
            "";
        updateExplorerParams({ schema: nextSchema, table: firstTable, offset: 0 });
    }

    const columns = selectedTable?.columns ?? [];
    const constraints = selectedTable?.constraints ?? [];
    const indexes = selectedTable?.indexes ?? [];

    return (
        <div className="stack">
            <section className="card">
                <div className="page-header">
                    <div>
                        <nav className="breadcrumb" aria-label="Navigasi halaman">
                            <Link to="/databases">Daftar Database</Link>
                            <span className="breadcrumb-sep" aria-hidden="true">/</span>
                            <span className="breadcrumb-current">Explorer</span>
                        </nav>
                        <h1>{target ? target.name : `Database #${id}`}</h1>
                        {schemaData && (
                            <div className="meta-row">
                                <span className="meta-chip">
                                    <span className="meta-label">Driver</span>
                                    <code>{schemaData.snapshot.driver}</code>
                                </span>
                                {target && (
                                    <span className="meta-chip">
                                        <span className="meta-label">Izin</span>
                                        <span
                                            className={`badge ${target.permission === "EDIT" ? "success" : "info"}`}
                                        >
                                            {target.permission}
                                        </span>
                                    </span>
                                )}
                                <span className="meta-chip">
                                    <span className="meta-label">Versi Skema</span>
                                    <strong>v{schemaData.version}</strong>
                                </span>
                            </div>
                        )}
                    </div>

                    <div className="action-row">
                        <Link className="btn" to={`/databases/${id}/migrations`}>
                            Migrations
                        </Link>
                        {target?.permission === "EDIT" && (
                            <Link className="btn" to={`/databases/${id}/diff`}>
                                Schema Diff
                            </Link>
                        )}
                        <button
                            type="button"
                            onClick={() => setReloadTick((prev) => prev + 1)}
                            disabled={isLoadingSchema}
                        >
                            {isLoadingSchema ? "Memuat..." : "Muat Ulang"}
                        </button>
                    </div>
                </div>

                {schemaError && (
                    <StatePanel
                        variant="error"
                        title="Gagal memuat skema"
                        message={schemaError}
                        onRetry={() => setReloadTick((prev) => prev + 1)}
                        action={
                            <Link className="btn" to="/databases">
                                Kembali ke Daftar Database
                            </Link>
                        }
                    />
                )}
            </section>

            {isLoadingSchema && !schemaData ? (
                <section className="card">
                    <StatePanel
                        variant="loading"
                        title="Membaca skema database"
                        message="Mengambil daftar schema, tabel, kolom, dan index..."
                    />
                </section>
            ) : (
                schemaData && (
                    <div className="explorer-grid">
                        <aside className="card explorer-sidebar">
                            <div className="field">
                                <label htmlFor="schema-select">Schema</label>
                                <select
                                    id="schema-select"
                                    value={activeSchema}
                                    onChange={(e) => handleSchemaChange(e.target.value)}
                                >
                                    {schemas.map((schemaName) => (
                                        <option key={schemaName} value={schemaName}>
                                            {schemaName}
                                        </option>
                                    ))}
                                </select>
                            </div>

                            <div className="field">
                                <label htmlFor="table-filter">Tabel ({tablesInSchema.length})</label>
                                <input
                                    id="table-filter"
                                    type="search"
                                    placeholder="Cari tabel..."
                                    value={tableFilter}
                                    onChange={(e) => setTableFilter(e.target.value)}
                                />
                            </div>

                            {tablesInSchema.length === 0 ? (
                                <p className="muted">Schema ini belum memiliki tabel.</p>
                            ) : filteredTables.length === 0 ? (
                                <div className="stack">
                                    <p className="muted">
                                        Tidak ada tabel yang cocok dengan "{tableFilter}".
                                    </p>
                                    <button
                                        type="button"
                                        className="btn-sm"
                                        onClick={() => setTableFilter("")}
                                    >
                                        Reset Pencarian
                                    </button>
                                </div>
                            ) : (
                                <ul className="table-list">
                                    {filteredTables.map((table) => (
                                        <li key={table.name}>
                                            <button
                                                type="button"
                                                className={`table-item ${selectedTable?.name === table.name ? "is-active" : ""}`}
                                                onClick={() =>
                                                    updateExplorerParams({
                                                        table: table.name,
                                                        offset: 0,
                                                    })
                                                }
                                            >
                                                {table.name}
                                            </button>
                                        </li>
                                    ))}
                                </ul>
                            )}
                        </aside>

                        <section className="card explorer-content">
                            {!selectedTable ? (
                                <p className="muted">Pilih tabel untuk melihat detailnya.</p>
                            ) : (
                                <>
                                    <div className="page-header">
                                        <div>
                                            <h2>
                                                {activeSchema}.{selectedTable.name}
                                            </h2>
                                            <p className="muted">
                                                {columns.length} kolom, {constraints.length}{" "}
                                                constraint, {indexes.length} index
                                            </p>
                                        </div>

                                        <div className="tab-group" role="tablist" aria-label="Tampilan tabel">
                                            <button
                                                type="button"
                                                role="tab"
                                                aria-selected={activeTab === "structure"}
                                                className={activeTab === "structure" ? "is-active" : ""}
                                                onClick={() =>
                                                    updateExplorerParams({ tab: "structure", offset: 0 })
                                                }
                                            >
                                                Struktur
                                            </button>
                                            <button
                                                type="button"
                                                role="tab"
                                                aria-selected={activeTab === "rows"}
                                                className={activeTab === "rows" ? "is-active" : ""}
                                                onClick={() =>
                                                    updateExplorerParams({ tab: "rows", offset: 0 })
                                                }
                                            >
                                                Data Baris
                                            </button>
                                        </div>
                                    </div>

                                    {activeTab === "structure" ? (
                                        <div className="stack">
                                            <div>
                                                <h3>Kolom</h3>
                                                <div className="table-scroll">
                                                    <table className="data-table">
                                                        <thead>
                                                            <tr>
                                                                <th>Nama</th>
                                                                <th>Tipe</th>
                                                                <th>Nullable</th>
                                                                <th>Default</th>
                                                            </tr>
                                                        </thead>
                                                        <tbody>
                                                            {columns.map((col) => (
                                                                <tr key={col.name}>
                                                                    <td>
                                                                        <code>{col.name}</code>
                                                                    </td>
                                                                    <td>
                                                                        <code>{col.type}</code>
                                                                    </td>
                                                                    <td>{col.nullable ? "Ya" : "Tidak"}</td>
                                                                    <td>
                                                                        {col.default === null ? (
                                                                            <span className="muted">(tidak ada)</span>
                                                                        ) : (
                                                                            <code>{col.default}</code>
                                                                        )}
                                                                    </td>
                                                                </tr>
                                                            ))}
                                                        </tbody>
                                                    </table>
                                                </div>
                                            </div>

                                            {constraints.length > 0 && (
                                                <div>
                                                    <h3>Constraint</h3>
                                                    <div className="table-scroll">
                                                        <table className="data-table">
                                                            <thead>
                                                                <tr>
                                                                    <th>Nama</th>
                                                                    <th>Jenis</th>
                                                                    <th>Kolom</th>
                                                                    <th>Definisi</th>
                                                                </tr>
                                                            </thead>
                                                            <tbody>
                                                                {constraints.map((c) => (
                                                                    <tr key={c.name}>
                                                                        <td>{c.name}</td>
                                                                        <td>{formatConstraintKind(c.kind)}</td>
                                                                        <td>
                                                                            <code>
                                                                                {(c.columns ?? []).join(", ")}
                                                                            </code>
                                                                        </td>
                                                                        <td>
                                                                            <code>{c.definition}</code>
                                                                        </td>
                                                                    </tr>
                                                                ))}
                                                            </tbody>
                                                        </table>
                                                    </div>
                                                </div>
                                            )}

                                            {indexes.length > 0 && (
                                                <div>
                                                    <h3>Index</h3>
                                                    <div className="table-scroll">
                                                        <table className="data-table">
                                                            <thead>
                                                                <tr>
                                                                    <th>Nama</th>
                                                                    <th>Method</th>
                                                                    <th>Kolom</th>
                                                                    <th>Unik</th>
                                                                </tr>
                                                            </thead>
                                                            <tbody>
                                                                {indexes.map((idx) => (
                                                                    <tr key={idx.name}>
                                                                        <td>{idx.name}</td>
                                                                        <td>
                                                                            <code>{idx.method}</code>
                                                                        </td>
                                                                        <td>
                                                                            <code>
                                                                                {(idx.columns ?? []).join(", ")}
                                                                            </code>
                                                                        </td>
                                                                        <td>{idx.unique ? "Ya" : "Tidak"}</td>
                                                                    </tr>
                                                                ))}
                                                            </tbody>
                                                        </table>
                                                    </div>
                                                </div>
                                            )}
                                        </div>
                                    ) : (
                                        <div className="stack">
                                            {rowsError ? (
                                                <StatePanel
                                                    variant="error"
                                                    title="Gagal memuat isi tabel"
                                                    message={rowsError}
                                                    onRetry={() => setReloadTick((prev) => prev + 1)}
                                                />
                                            ) : isLoadingRows && !visibleRowsPage ? (
                                                <StatePanel
                                                    variant="loading"
                                                    title="Memuat isi tabel"
                                                    message={`Mengambil baris data dari ${activeSchema}.${selectedTable.name}...`}
                                                />
                                            ) : visibleRowsPage && visibleRowsPage.rows.length === 0 ? (
                                                <StatePanel
                                                    variant="empty"
                                                    title="Tabel masih kosong"
                                                    message="Belum ada baris data di dalam tabel ini."
                                                />
                                            ) : (
                                                visibleRowsPage && (
                                                    <>
                                                        <div
                                                            className={`table-scroll ${isLoadingRows ? "is-loading" : ""}`}
                                                        >
                                                            <table className="data-table">
                                                                <thead>
                                                                    <tr>
                                                                        {visibleRowsPage.columns.map((colName) => (
                                                                            <th key={colName}>{colName}</th>
                                                                        ))}
                                                                    </tr>
                                                                </thead>
                                                                <tbody>
                                                                    {visibleRowsPage.rows.map((row, rowIdx) => (
                                                                        <tr key={rowIdx}>
                                                                            {row.map((cell, cellIdx) => (
                                                                                <td key={cellIdx}>
                                                                                    {formatCellValue(cell)}
                                                                                </td>
                                                                            ))}
                                                                        </tr>
                                                                    ))}
                                                                </tbody>
                                                            </table>
                                                        </div>

                                                        <div className="pagination-bar">
                                                            <span className="muted">
                                                                {isLoadingRows
                                                                    ? "Memuat halaman..."
                                                                    : `Menampilkan baris ${visibleRowsPage.offset + 1} sampai ${visibleRowsPage.offset + visibleRowsPage.rows.length}`}
                                                            </span>
                                                            <div className="action-row">
                                                                <button
                                                                    type="button"
                                                                    disabled={
                                                                        isLoadingRows || visibleRowsPage.offset === 0
                                                                    }
                                                                    onClick={() =>
                                                                        updateExplorerParams({
                                                                            tab: "rows",
                                                                            offset: Math.max(
                                                                                visibleRowsPage.offset -
                                                                                    PAGE_LIMIT,
                                                                                0,
                                                                            ),
                                                                        })
                                                                    }
                                                                >
                                                                    Sebelumnya
                                                                </button>
                                                                <button
                                                                    type="button"
                                                                    disabled={
                                                                        isLoadingRows || !visibleRowsPage.has_more
                                                                    }
                                                                    onClick={() =>
                                                                        updateExplorerParams({
                                                                            tab: "rows",
                                                                            offset:
                                                                                visibleRowsPage.offset +
                                                                                PAGE_LIMIT,
                                                                        })
                                                                    }
                                                                >
                                                                    Berikutnya
                                                                </button>
                                                            </div>
                                                        </div>
                                                    </>
                                                )
                                            )}
                                        </div>
                                    )}
                                </>
                            )}
                        </section>
                    </div>
                )
            )}
        </div>
    );
}