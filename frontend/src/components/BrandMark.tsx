export function BrandMark({ size = 24 }: { size?: number }) {
    return (
        <svg
            width={size}
            height={size}
            viewBox="0 0 32 32"
            fill="none"
            aria-hidden="true"
            className="brand-mark"
        >
            <path
                d="M6 9.5L16 4l10 5.5v13L16 28 6 22.5v-7L16 10l5.2 3v6.2L16 22.2l-4.8-2.8"
                stroke="currentColor"
                strokeWidth="2.8"
                strokeLinecap="round"
                strokeLinejoin="round"
            />
        </svg>
    );
}
