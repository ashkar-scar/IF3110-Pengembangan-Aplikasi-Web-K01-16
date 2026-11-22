import { useEffect, useRef, useState, type PointerEvent } from "react";
import yuzukiAngry from "../assets/yuzuki-chibi-angry.png";
import yuzukiClick from "../assets/yuzuki-chibi-click.png";
import yuzukiDrag from "../assets/yuzuki-chibi-drag.png";
import yuzukiHover from "../assets/yuzuki-chibi-hover.png";
import yuzukiIdle from "../assets/yuzuki-chibi.png";
import yuzukiPout from "../assets/yuzuki-chibi-pout.png";
import yuzukiSad from "../assets/yuzuki-chibi-sad.png";
import yuzukiSurprised from "../assets/yuzuki-chibi-surprised.png";
import yuzukiWink from "../assets/yuzuki-chibi-wink.png";

type YuzukiMood =
    | "idle"
    | "hover"
    | "happy"
    | "wink"
    | "surprised"
    | "pout"
    | "angry"
    | "sad"
    | "drag";
type ClickReaction = "happy" | "wink" | "surprised" | "pout" | "angry" | "sad";

const CLICK_SEQUENCE: { mood: ClickReaction; line: string }[] = [
    { mood: "happy", line: "Semangat kelola skema!" },
    { mood: "wink", line: "Query aman, siap jalan!" },
    { mood: "surprised", line: "Eh? Ada migrasi baru?!" },
    { mood: "pout", line: "Hmph, jangan dicolek terus!" },
    { mood: "angry", line: "Kesal nih, lagi fokus kerja!" },
    { mood: "sad", line: "Aduh, capek diganggu terus..." },
];

const SPRITES: { mood: YuzukiMood; src: string }[] = [
    { mood: "idle", src: yuzukiIdle },
    { mood: "hover", src: yuzukiHover },
    { mood: "happy", src: yuzukiClick },
    { mood: "wink", src: yuzukiWink },
    { mood: "surprised", src: yuzukiSurprised },
    { mood: "pout", src: yuzukiPout },
    { mood: "angry", src: yuzukiAngry },
    { mood: "sad", src: yuzukiSad },
    { mood: "drag", src: yuzukiDrag },
];

const POSITION_STORAGE_KEY = "dbmanager_yuzuki_pos";

interface DragState {
    pointerId: number;
    startX: number;
    startY: number;
    originX: number;
    originY: number;
    width: number;
    height: number;
    dragged: boolean;
}

function clampPosition(x: number, y: number, width: number, height: number) {
    const maxX = Math.max(8, window.innerWidth - width - 8);
    const maxY = Math.max(8, window.innerHeight - height - 8);
    return {
        x: Math.min(Math.max(8, x), maxX),
        y: Math.min(Math.max(8, y), maxY),
    };
}

function loadSavedPosition(): { x: number; y: number } | null {
    try {
        const raw = window.sessionStorage.getItem(POSITION_STORAGE_KEY);
        if (!raw) return null;
        const parsed = JSON.parse(raw) as { x?: unknown; y?: unknown };
        if (typeof parsed.x === "number" && typeof parsed.y === "number") {
            return clampPosition(parsed.x, parsed.y, 120, 180);
        }
    } catch {
        return null;
    }
    return null;
}

interface InteractiveYuzukiProps {
    variant: "companion" | "inline";
    defaultMood?: YuzukiMood;
}

export function InteractiveYuzuki({ variant, defaultMood = "idle" }: InteractiveYuzukiProps) {
    const draggable = variant === "companion";
    const [isHovered, setIsHovered] = useState(false);
    const [isDragging, setIsDragging] = useState(false);
    const [reaction, setReaction] = useState<{ mood: ClickReaction; line: string } | null>(null);
    const [animTick, setAnimTick] = useState(0);
    const [position, setPosition] = useState<{ x: number; y: number } | null>(() =>
        draggable ? loadSavedPosition() : null,
    );

    const triggerRef = useRef<HTMLButtonElement | null>(null);
    const dragRef = useRef<DragState | null>(null);
    const justDraggedRef = useRef(false);
    const dragCountRef = useRef(0);
    const clickCountRef = useRef(0);
    const timerRef = useRef<number | null>(null);

    useEffect(() => {
        return () => {
            if (timerRef.current !== null) {
                window.clearTimeout(timerRef.current);
            }
        };
    }, []);

    useEffect(() => {
        if (!draggable) return;
        function handleResize() {
            setPosition((prev) => {
                if (!prev || !triggerRef.current) return prev;
                const rect = triggerRef.current.getBoundingClientRect();
                return clampPosition(prev.x, prev.y, rect.width, rect.height);
            });
        }
        window.addEventListener("resize", handleResize);
        return () => window.removeEventListener("resize", handleResize);
    }, [draggable]);

    function showReaction(mood: ClickReaction, line: string, durationMs = 1400) {
        if (timerRef.current !== null) {
            window.clearTimeout(timerRef.current);
        }
        setReaction({ mood, line });
        setAnimTick((prev) => prev + 1);
        timerRef.current = window.setTimeout(() => {
            setReaction(null);
            timerRef.current = null;
        }, durationMs);
    }

    function handlePointerDown(event: PointerEvent<HTMLButtonElement>) {
        if (!draggable || event.button !== 0 || !triggerRef.current) return;
        const rect = triggerRef.current.getBoundingClientRect();
        dragRef.current = {
            pointerId: event.pointerId,
            startX: event.clientX,
            startY: event.clientY,
            originX: rect.left,
            originY: rect.top,
            width: rect.width,
            height: rect.height,
            dragged: false,
        };
        event.currentTarget.setPointerCapture(event.pointerId);
    }

    function handlePointerMove(event: PointerEvent<HTMLButtonElement>) {
        const drag = dragRef.current;
        if (!drag || drag.pointerId !== event.pointerId) return;

        const dx = event.clientX - drag.startX;
        const dy = event.clientY - drag.startY;
        if (!drag.dragged && Math.hypot(dx, dy) > 5) {
            drag.dragged = true;
            setIsDragging(true);
            if (timerRef.current !== null) {
                window.clearTimeout(timerRef.current);
                timerRef.current = null;
            }
            setReaction(null);
        }

        if (drag.dragged) {
            setPosition(
                clampPosition(drag.originX + dx, drag.originY + dy, drag.width, drag.height),
            );
        }
    }

    function finishDrag(event: PointerEvent<HTMLButtonElement>) {
        const drag = dragRef.current;
        if (!drag || drag.pointerId !== event.pointerId) return;

        if (event.currentTarget.hasPointerCapture(event.pointerId)) {
            event.currentTarget.releasePointerCapture(event.pointerId);
        }

        if (drag.dragged) {
            justDraggedRef.current = true;
            setIsDragging(false);
            const finalPos = clampPosition(
                drag.originX + (event.clientX - drag.startX),
                drag.originY + (event.clientY - drag.startY),
                drag.width,
                drag.height,
            );
            setPosition(finalPos);
            window.sessionStorage.setItem(POSITION_STORAGE_KEY, JSON.stringify(finalPos));
            dragCountRef.current += 1;
            if (dragCountRef.current >= 3) {
                showReaction("pout", "Pusing diseret terus!", 1200);
            } else {
                showReaction("surprised", "Posisi baru oke!", 1100);
            }
        }
        dragRef.current = null;
    }

    function handlePoke() {
        // Mencegah klik ikut kepicu setelah selesai nyeret Yuzuki.
        if (justDraggedRef.current) {
            justDraggedRef.current = false;
            return;
        }
        const next = CLICK_SEQUENCE[clickCountRef.current % CLICK_SEQUENCE.length];
        clickCountRef.current += 1;
        showReaction(next.mood, next.line);
    }

    function handleDoubleClick() {
        if (!draggable) return;
        setPosition(null);
        window.sessionStorage.removeItem(POSITION_STORAGE_KEY);
        showReaction("wink", "Balik ke pojok lagi~", 1200);
    }

    const activeMood: YuzukiMood = isDragging
        ? "drag"
        : reaction
          ? reaction.mood
          : isHovered
            ? "hover"
            : defaultMood;

    const bubbleText = isDragging
        ? "Waaah, pelan-pelan!"
        : reaction
          ? reaction.line
          : isHovered
            ? draggable
                ? "Klik atau seret aku!"
                : "Klik aku dong!"
            : "";

    const companionStyle =
        draggable && position
            ? {
                  insetInlineStart: `${position.x}px`,
                  insetBlockStart: `${position.y}px`,
                  insetInlineEnd: "auto",
                  insetBlockEnd: "auto",
              }
            : undefined;

    const bubbleBelow = Boolean(draggable && position && position.y < 52);

    return (
        <div
            className={`yuzuki-slot is-${variant} ${bubbleBelow ? "is-bubble-below" : ""}`}
            style={companionStyle}
        >
            {bubbleText && (
                <div key={animTick} className="yuzuki-bubble" role="status">
                    {bubbleText}
                </div>
            )}

            <button
                ref={triggerRef}
                type="button"
                className={`yuzuki-trigger is-${activeMood}`}
                aria-label={
                    draggable
                        ? "Sapa atau seret maskot Yuzuki (klik dua kali untuk reset posisi)"
                        : "Sapa maskot Yuzuki"
                }
                onMouseEnter={() => setIsHovered(true)}
                onMouseLeave={() => setIsHovered(false)}
                onFocus={() => setIsHovered(true)}
                onBlur={() => setIsHovered(false)}
                onPointerDown={handlePointerDown}
                onPointerMove={handlePointerMove}
                onPointerUp={finishDrag}
                onPointerCancel={finishDrag}
                onClick={handlePoke}
                onDoubleClick={handleDoubleClick}
            >
                {/* Menumpuk semua sprite biar transisi ganti ekspresi instan tanpa kedip load. */}
                <span className="yuzuki-stage" key={animTick}>
                    {SPRITES.map((sprite) => (
                        <img
                            key={sprite.mood}
                            src={sprite.src}
                            alt=""
                            aria-hidden="true"
                            draggable={false}
                            className={`yuzuki-sprite ${activeMood === sprite.mood ? "is-active" : ""}`}
                        />
                    ))}
                </span>
            </button>
        </div>
    );
}
