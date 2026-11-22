import { useState, type FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router-dom";
import schoolBg from "../assets/anime-school-background.jpg";
import { BrandMark } from "../components/BrandMark";
import { StatePanel } from "../components/StatePanel";
import { useAuth } from "../context/useAuth";
import { ApiRequestError, type User } from "../types/auth";

function resolveLandingPath(candidate: string | undefined, targetUser: User): string {
    if (!candidate || candidate === "/login") {
        return "/databases";
    }
    if (candidate.startsWith("/admin") && !targetUser.is_platform_admin) {
        return "/databases";
    }
    return candidate;
}

export function LoginPage() {
    const { user, isLoading, login } = useAuth();
    const navigate = useNavigate();
    const location = useLocation();
    const rawFrom = (location.state as { from?: string } | null)?.from;

    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [error, setError] = useState("");
    const [isSubmitting, setIsSubmitting] = useState(false);

    if (isLoading) {
        return (
            <div className="app-shell">
                <div className="app-backdrop" aria-hidden="true">
                    <div
                        className="app-backdrop-img"
                        style={{ backgroundImage: `url(${schoolBg})` }}
                    />
                </div>
                <main className="app-main">
                    <section className="login-single card">
                        <StatePanel
                            variant="loading"
                            title="Memeriksa sesi"
                            message="Menunggu pengecekan cookie login..."
                        />
                    </section>
                </main>
            </div>
        );
    }

    if (user) {
        return <Navigate to={resolveLandingPath(rawFrom, user)} replace />;
    }

    async function handleSubmit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        setError("");
        setIsSubmitting(true);

        try {
            const loggedUser = await login(username.trim(), password);
            navigate(resolveLandingPath(rawFrom, loggedUser), { replace: true });
        } catch (err) {
            if (err instanceof ApiRequestError) {
                setError(err.message);
            } else {
                setError("Tidak dapat terhubung ke server.");
            }
        } finally {
            setIsSubmitting(false);
        }
    }

    return (
        <div className="app-shell">
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
            <main className="app-main login-stage">
                <section className="login-shell card">
                    <div className="login-pane">
                        <div className="login-brand">
                            <BrandMark size={24} />
                            <span>DBManager</span>
                        </div>
                        <h1>Masuk ke Workspace</h1>
                        <p className="muted">
                            Kelola skema, approval tim, dan migrasi database target dalam satu meja
                            kerja.
                        </p>

                        {error && (
                            <p className="alert error" role="alert">
                                {error}
                            </p>
                        )}

                        <form onSubmit={handleSubmit}>
                            <div className="field">
                                <label htmlFor="username">Username</label>
                                <input
                                    id="username"
                                    name="username"
                                    type="text"
                                    autoComplete="username"
                                    autoCorrect="off"
                                    autoCapitalize="none"
                                    spellCheck={false}
                                    value={username}
                                    onChange={(e) => setUsername(e.target.value)}
                                    required
                                />
                            </div>

                            <div className="field">
                                <label htmlFor="password">Password</label>
                                <input
                                    id="password"
                                    name="password"
                                    type="password"
                                    autoComplete="current-password"
                                    value={password}
                                    onChange={(e) => setPassword(e.target.value)}
                                    required
                                />
                            </div>

                            <button
                                type="submit"
                                className="primary full-width"
                                disabled={isSubmitting}
                            >
                                {isSubmitting ? "Memproses..." : "Masuk"}
                            </button>
                        </form>
                    </div>
                </section>
            </main>
        </div>
    );
}
