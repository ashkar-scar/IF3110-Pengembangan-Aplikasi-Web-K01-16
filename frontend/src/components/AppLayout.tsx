import { useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";
import schoolBg from "../assets/anime-school-background.jpg";
import { useAuth } from "../context/useAuth";
import { BrandMark } from "./BrandMark";
import { InteractiveYuzuki } from "./InteractiveYuzuki";

const WALLPAPER_STORAGE_KEY = "dbmanager_wallpaper";

export function AppLayout() {
    const { user, logout } = useAuth();
    const navigate = useNavigate();
    const [isLoggingOut, setIsLoggingOut] = useState(false);
    const [showWallpaper, setShowWallpaper] = useState(() => {
        return window.localStorage.getItem(WALLPAPER_STORAGE_KEY) !== "off";
    });

    function toggleWallpaper() {
        setShowWallpaper((prev) => {
            const next = !prev;
            window.localStorage.setItem(WALLPAPER_STORAGE_KEY, next ? "on" : "off");
            return next;
        });
    }

    async function handleLogout() {
        setIsLoggingOut(true);
        try {
            await logout();
            navigate("/login", { replace: true });
        } finally {
            setIsLoggingOut(false);
        }
    }

    return (
        <div className="app-shell">
            {showWallpaper && (
                <>
                    <div className="app-backdrop" aria-hidden="true">
                        <div
                            className="app-backdrop-img"
                            style={{ backgroundImage: `url(${schoolBg})` }}
                        />
                        <div className="studio-motes">
                            <span />
                            <span />
                            <span />
                            <span />
                            <span />
                            <span />
                        </div>
                    </div>

                    <InteractiveYuzuki variant="companion" />
                </>
            )}

            <a className="skip-link" href="#main-content">
                Lewati ke konten utama
            </a>
            <header className="topbar">
                <div className="topbar-inner">
                    <NavLink className="brand" to="/databases">
                        <BrandMark size={24} />
                        <span>DBManager</span>
                    </NavLink>

                    <div className="nav-panel">
                        <nav className="topbar-nav" aria-label="Primary">
                            <ul className="nav-links">
                                <li>
                                    <NavLink to="/databases">
                                        Database
                                    </NavLink>
                                </li>
                                {user?.is_platform_admin && (
                                    <li>
                                        <NavLink to="/admin">
                                            Administrasi
                                        </NavLink>
                                    </li>
                                )}
                                <li>
                                    <NavLink to="/help">
                                        Bantuan
                                    </NavLink>
                                </li>
                            </ul>
                        </nav>

                        <div className="nav-user">
                            <button
                                type="button"
                                className="btn-sm wallpaper-toggle"
                                aria-pressed={showWallpaper}
                                aria-label={
                                    showWallpaper
                                        ? "Matikan wallpaper latar"
                                        : "Aktifkan wallpaper latar"
                                }
                                onClick={toggleWallpaper}
                                title="Tampilkan atau sembunyikan wallpaper latar"
                            >
                                <svg
                                    width="14"
                                    height="14"
                                    viewBox="0 0 24 24"
                                    fill="none"
                                    stroke="currentColor"
                                    strokeWidth="2"
                                    strokeLinecap="round"
                                    strokeLinejoin="round"
                                    aria-hidden="true"
                                >
                                    <rect x="3" y="3" width="18" height="18" rx="3" />
                                    <circle cx="8.5" cy="8.5" r="1.5" />
                                    <path d="m21 15-5-5L5 21" />
                                </svg>
                                <span className="wallpaper-prefix">Wallpaper:</span>
                                <span>{showWallpaper ? "On" : "Off"}</span>
                            </button>
                            <span className="user-chip" title={user?.username}>
                                {user?.username}
                            </span>
                            <button
                                type="button"
                                className="nav-logout"
                                onClick={handleLogout}
                                disabled={isLoggingOut}
                            >
                                {isLoggingOut ? "Keluar..." : "Keluar"}
                            </button>
                        </div>
                    </div>
                </div>
            </header>

            <main id="main-content" className="app-main">
                <Outlet />
            </main>
        </div>
    );
}
