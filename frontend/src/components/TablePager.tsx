interface TablePaginationBarProps {
    page: number;
    totalPages: number;
    totalItems: number;
    pageSize: number;
    onPageChange: (nextPage: number) => void;
}

export function TablePaginationBar({
    page,
    totalPages,
    totalItems,
    pageSize,
    onPageChange,
}: TablePaginationBarProps) {
    if (totalItems <= pageSize) return null;
    const start = page * pageSize + 1;
    const end = Math.min((page + 1) * pageSize, totalItems);

    return (
        <div className="pagination-bar">
            <span className="muted">
                Menampilkan {start}–{end} dari {totalItems} (halaman {page + 1}/{totalPages})
            </span>
            <div className="action-row">
                <button
                    type="button"
                    className="btn-sm"
                    disabled={page === 0}
                    onClick={() => onPageChange(page - 1)}
                >
                    Sebelumnya
                </button>
                <button
                    type="button"
                    className="btn-sm"
                    disabled={page + 1 >= totalPages}
                    onClick={() => onPageChange(page + 1)}
                >
                    Berikutnya
                </button>
            </div>
        </div>
    );
}
