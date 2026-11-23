import { lazy, Suspense } from "react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { AppLayout } from "./components/AppLayout";
import { ProtectedRoute } from "./components/ProtectedRoute";
import { StatePanel } from "./components/StatePanel";
import { AuthProvider } from "./context/AuthContext";
import { LoginPage } from "./pages/LoginPage";

const DatabasesPage = lazy(() =>
    import("./pages/DatabasesPage").then((m) => ({ default: m.DatabasesPage })),
);
const ExplorerPage = lazy(() =>
    import("./pages/ExplorerPage").then((m) => ({ default: m.ExplorerPage })),
);
const MigrationsPage = lazy(() =>
    import("./pages/MigrationsPage").then((m) => ({ default: m.MigrationsPage })),
);
const DiffPage = lazy(() => import("./pages/DiffPage").then((m) => ({ default: m.DiffPage })));
const HelpPage = lazy(() => import("./pages/HelpPage").then((m) => ({ default: m.HelpPage })));
const AdminPage = lazy(() => import("./pages/AdminPage").then((m) => ({ default: m.AdminPage })));

function RouteFallback() {
    return (
        <section className="card">
            <StatePanel
                variant="loading"
                title="Memuat halaman"
                message="Menyiapkan komponen antarmuka..."
            />
        </section>
    );
}

export default function App() {
    return (
        <AuthProvider>
            <BrowserRouter>
                <Suspense fallback={<RouteFallback />}>
                    <Routes>
                        <Route path="/login" element={<LoginPage />} />

                        <Route element={<ProtectedRoute />}>
                            <Route element={<AppLayout />}>
                                <Route path="/databases" element={<DatabasesPage />} />
                                <Route path="/databases/:id" element={<ExplorerPage />} />
                                <Route path="/databases/:id/migrations" element={<MigrationsPage />} />
                                <Route path="/databases/:id/diff" element={<DiffPage />} />
                                <Route path="/help" element={<HelpPage />} />

                                <Route element={<ProtectedRoute requireAdmin />}>
                                    <Route path="/admin" element={<AdminPage />} />
                                </Route>
                            </Route>
                        </Route>

                        <Route path="*" element={<Navigate to="/databases" replace />} />
                    </Routes>
                </Suspense>
            </BrowserRouter>
        </AuthProvider>
    );
}
