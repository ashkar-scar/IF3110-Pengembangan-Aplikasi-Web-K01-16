import type { ReactNode } from "react";
import { InteractiveYuzuki } from "./InteractiveYuzuki";

interface StatePanelProps {
    variant: "loading" | "empty" | "error";
    title: string;
    message: string;
    onRetry?: () => void;
    retryLabel?: string;
    action?: ReactNode;
}

// Menyatukan tampilan loading, kosong, dan error supaya semua halaman punya feedback + aksi pemulihan.
export function StatePanel({
    variant,
    title,
    message,
    onRetry,
    retryLabel = "Coba Lagi",
    action,
}: StatePanelProps) {
    return (
        <div
            className={`state-box ${variant === "error" ? "is-error" : ""} ${variant === "empty" ? "is-empty" : ""}`}
            role={variant === "error" ? "alert" : "status"}
        >
            {variant === "empty" && <InteractiveYuzuki variant="inline" />}
            {variant === "error" && <InteractiveYuzuki variant="inline" defaultMood="sad" />}
            <div className="state-copy">
                <p className="state-title">{title}</p>
                <p className="muted">{message}</p>
                {(onRetry || action) && (
                    <div className="action-row">
                        {onRetry && (
                            <button type="button" onClick={onRetry}>
                                {retryLabel}
                            </button>
                        )}
                        {action}
                    </div>
                )}
            </div>
        </div>
    );
}
