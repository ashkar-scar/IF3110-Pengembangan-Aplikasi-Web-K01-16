import { useState } from "react";

export interface PagedResult<T> {
    query: string;
    setQuery: (next: string) => void;
    filteredTotal: number;
    pageItems: T[];
    page: number;
    totalPages: number;
    setPage: (next: number) => void;
    pageSize: number;
    shouldShowSearch: boolean;
    shouldShowPagination: boolean;
}

// Menyediakan filter dan pagination klien yang aktif otomatis saat daftar data mulai banyak.
export function usePagedList<T>(
    items: T[],
    getSearchText: (item: T) => string,
    pageSize = 15,
    searchThreshold = 10,
): PagedResult<T> {
    const [query, setQueryState] = useState("");
    const [page, setPage] = useState(0);

    const normalized = query.trim().toLowerCase();
    const filtered = normalized
        ? items.filter((item) => getSearchText(item).toLowerCase().includes(normalized))
        : items;

    const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
    const safePage = Math.min(page, totalPages - 1);
    const start = safePage * pageSize;

    return {
        query,
        setQuery: (next: string) => {
            setQueryState(next);
            setPage(0);
        },
        filteredTotal: filtered.length,
        pageItems: filtered.slice(start, start + pageSize),
        page: safePage,
        totalPages,
        setPage: (next: number) => {
            setPage(Math.max(0, Math.min(next, totalPages - 1)));
        },
        pageSize,
        shouldShowSearch: items.length > searchThreshold || query !== "",
        shouldShowPagination: filtered.length > pageSize,
    };
}
