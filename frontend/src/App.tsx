import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { AppLayout } from "./components/AppLayout";
import { ProtectedRoute } from "./components/ProtectedRoute";
import { AuthProvider } from "./context/AuthContext";
import { LoginPage } from "./pages/LoginPage";
import { DatabasesPage } from "./pages/DatabasesPage";
import { ExplorerPage } from "./pages/ExplorerPage";
import { AdminPage } from "./pages/AdminPage";
import { DiffPage } from "./pages/DiffPage";
import { MigrationsPage } from "./pages/MigrationsPage";

export default function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<LoginPage />} />

          <Route element={<ProtectedRoute />}>
            <Route element={<AppLayout />}>
              <Route
                path="/databases"
                element={<DatabasesPage />} />
              <Route path="/databases/:id" element={<ExplorerPage />} />
              <Route path="/databases/:id/migrations" element={<MigrationsPage />} />
              <Route path="/databases/:id/diff" element={<DiffPage />} />

              <Route element={<ProtectedRoute requireAdmin />}>
                <Route path="/admin" element={<AdminPage />} />
              </Route>
            </Route>
          </Route>

          <Route path="*" element={<Navigate to="/databases" replace />} />
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  );
}