import { useId, useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";
import schoolBg from "../assets/anime-school-background.jpg";
import { useAuth } from "../context/useAuth";
import { BrandMark } from "./BrandMark";
import { InteractiveYuzuki } from "./InteractiveYuzuki";

const WALLPAPER_STORAGE_KEY = "dbmanager_wallpaper";

export function AppLayout() {
    const { user, logout } = useAuth();
    const navigate = useNavigate();
    const [menuOpen, setMenuOpen] = useState(false);
    const [isLoggingOut, setIsLoggingOut] = useState(false);
    const [showWallpaper, setShowWallpaper] = useState(() => {
        return window.localStorage.getItem(WALLPAPER_STORAGE_KEY) !== "off";
    });
    const navId = useId();

    function closeMenu() {
        setMenuOpen(false);
    }

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
                    <NavLink className="brand" to="/databases" onClick={closeMenu}>
                        <BrandMark size={24} />
                        <span>DBManager</span>
                    </NavLink>

                    <button
                        type="button"
                        className="menu-toggle"
                        aria-expanded={menuOpen}
                        aria-controls={navId}
                        onClick={() => setMenuOpen((prev) => !prev)}
                    >
                        {menuOpen ? "Tutup Menu" : "Menu"}
                    </button>

                    <div id={navId} className={`nav-panel ${menuOpen ? "is-open" : ""}`}>
                        <nav aria-label="Primary">
                            <ul className="nav-links">
                                <li>
                                    <NavLink to="/databases" onClick={closeMenu}>
                                        Database
                                    </NavLink>
                                </li>
                                {user?.is_platform_admin && (
                                    <li>
                                        <NavLink to="/admin" onClick={closeMenu}>
                                            Administrasi
                                        </NavLink>
                                    </li>
                                )}
                            </ul>
                        </nav>

                        <div className="nav-user">
                            <button
                                type="button"
                                className="btn-sm"
                                aria-pressed={showWallpaper}
                                onClick={toggleWallpaper}
                                title="Tampilkan atau sembunyikan wallpaper latar"
                            >
                                {showWallpaper ? "Wallpaper: On" : "Wallpaper: Off"}
                            </button>
                            <span className="user-chip">{user?.username}</span>
                            <button type="button" onClick={handleLogout} disabled={isLoggingOut}>
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
