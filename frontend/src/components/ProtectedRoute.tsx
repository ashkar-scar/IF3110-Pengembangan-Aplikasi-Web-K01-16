import { Link, Navigate, Outlet, useLocation } from "react-router-dom";
import { useAuth } from "../context/useAuth";
import { StatePanel } from "./StatePanel";

export function ProtectedRoute({ requireAdmin = false }: { requireAdmin?: boolean }) {
    const { user, isLoading } = useAuth();
    const location = useLocation();

    if (isLoading) {
        return (
            <main className="app-main">
                <section className="card">
                    <StatePanel
                        variant="loading"
                        title="Memeriksa sesi login"
                        message="Sebentar, status akun Anda sedang diverifikasi..."
                    />
                </section>
            </main>
        );
    }

    if (!user) {
        return <Navigate to="/login" replace state={{ from: location.pathname }} />;
    }

    if (requireAdmin && !user.is_platform_admin) {
        return (
            <section className="card">
                <h1>Akses Ditolak</h1>
                <StatePanel
                    variant="error"
                    title="Khusus Admin Platform"
                    message="Akun Anda tidak memiliki izin untuk membuka halaman administrasi."
                    action={
                        <Link className="btn" to="/databases">
                            Kembali ke Daftar Database
                        </Link>
                    }
                />
            </section>
        );
    }

    return <Outlet />;
}