interface TablePaginationBarProps {
    page: number;
    totalPages: number;
    totalItems: number;
    pageSize: number;
    onPageChange: (nextPage: number) => void;
    serverOffset?: number;
    hasMoreServer?: boolean;
    onServerPrev?: () => void;
    onServerNext?: () => void;
}

export function TablePaginationBar({
    page,
    totalPages,
    totalItems,
    pageSize,
    onPageChange,
    serverOffset = 0,
    hasMoreServer = false,
    onServerPrev,
    onServerNext,
}: TablePaginationBarProps) {
    const hasPrev = page > 0 || serverOffset > 0;
    const hasNext = page + 1 < totalPages || hasMoreServer;
    if (!hasPrev && !hasNext) return null;

    const start = serverOffset + page * pageSize + 1;
    const end = serverOffset + Math.min((page + 1) * pageSize, totalItems);
    const totalLabel = hasMoreServer ? `${serverOffset + totalItems}+` : `${serverOffset + totalItems}`;

    function handlePrev() {
        if (page > 0) {
            onPageChange(page - 1);
        } else if (serverOffset > 0 && onServerPrev) {
            onServerPrev();
        }
    }

    function handleNext() {
        if (page + 1 < totalPages) {
            onPageChange(page + 1);
        } else if (hasMoreServer && onServerNext) {
            onServerNext();
        }
    }

    return (
        <div className="pagination-bar">
            <span className="muted">
                Menampilkan {totalItems === 0 ? 0 : `${start}–${end}`} dari {totalLabel} (halaman{" "}
                {page + 1}/{totalPages})
            </span>
            <div className="action-row">
                <button type="button" className="btn-sm" disabled={!hasPrev} onClick={handlePrev}>
                    Sebelumnya
                </button>
                <button type="button" className="btn-sm" disabled={!hasNext} onClick={handleNext}>
                    Berikutnya
                </button>
            </div>
        </div>
    );
}
