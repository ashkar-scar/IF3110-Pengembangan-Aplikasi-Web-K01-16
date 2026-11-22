import { useState, type FormEvent } from "react";
import { createAdminUser, updateAdminUser } from "../../api/admin";
import { StatePanel } from "../StatePanel";
import { ApiRequestError } from "../../types/auth";
import type { User } from "../../types/admin";

interface UsersPanelProps {
    users: User[];
    onRefresh: () => Promise<void>;
}

export function UsersPanel({ users, onRefresh }: UsersPanelProps) {
    const [editingId, setEditingId] = useState<number | null>(null);
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [isPlatformAdmin, setIsPlatformAdmin] = useState(false);
    const [isActive, setIsActive] = useState(true);

    const [error, setError] = useState("");
    const [notice, setNotice] = useState("");
    const [searchQuery, setSearchQuery] = useState("");
    const [isSubmitting, setIsSubmitting] = useState(false);

    function resetForm() {
        setEditingId(null);
        setUsername("");
        setPassword("");
        setIsPlatformAdmin(false);
        setIsActive(true);
    }

    function startEdit(user: User) {
        setEditingId(user.id);
        setUsername(user.username);
        // Mengosongkan password saat edit biar backend tetep pakai password lama.
        setPassword("");
        setIsPlatformAdmin(user.is_platform_admin);
        setIsActive(user.is_active);
        setError("");
        setNotice("");
        window.scrollTo({ top: 0, behavior: "smooth" });
    }

    async function handleSubmit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        setError("");
        setNotice("");
        setIsSubmitting(true);

        const payload = {
            username: username.trim(),
            password,
            is_platform_admin: isPlatformAdmin,
            is_active: isActive,
        };

        try {
            if (editingId === null) {
                await createAdminUser(payload);
                setNotice(`User "${payload.username}" berhasil dibuat.`);
            } else {
                await updateAdminUser(editingId, payload);
                setNotice(`User "${payload.username}" berhasil diperbarui.`);
            }
            resetForm();
            await onRefresh();
        } catch (err) {
            if (err instanceof ApiRequestError) {
                setError(err.message);
            } else {
                setError("Gagal menyimpan data user.");
            }
        } finally {
            setIsSubmitting(false);
        }
    }

    const filteredUsers = users.filter((user) =>
        user.username.toLowerCase().includes(searchQuery.trim().toLowerCase()),
    );

    return (
        <div className="stack">
            <section className="card">
                <h2>{editingId === null ? "Tambah User" : `Edit User #${editingId}`}</h2>

                {error && (
                    <p className="alert error" role="alert">
                        {error}
                    </p>
                )}
                {notice && (
                    <p className="alert success" role="status">
                        {notice}
                    </p>
                )}

                <form onSubmit={handleSubmit}>
                    <div className="form-grid">
                        <div className="field">
                            <label htmlFor="user-username">Username</label>
                            <input
                                id="user-username"
                                type="text"
                                maxLength={100}
                                value={username}
                                onChange={(e) => setUsername(e.target.value)}
                                required
                            />
                        </div>

                        <div className="field">
                            <label htmlFor="user-password">
                                {editingId === null
                                    ? "Password (10-72 karakter)"
                                    : "Password Baru (kosongkan jika tetap)"}
                            </label>
                            <input
                                id="user-password"
                                type="password"
                                minLength={editingId === null ? 10 : undefined}
                                maxLength={72}
                                value={password}
                                onChange={(e) => setPassword(e.target.value)}
                                required={editingId === null}
                            />
                        </div>
                    </div>

                    <div className="checkbox-row">
                        <label>
                            <input
                                type="checkbox"
                                checked={isPlatformAdmin}
                                onChange={(e) => setIsPlatformAdmin(e.target.checked)}
                            />
                            Admin platform
                        </label>

                        <label>
                            <input
                                type="checkbox"
                                checked={isActive}
                                onChange={(e) => setIsActive(e.target.checked)}
                            />
                            Aktif
                        </label>
                    </div>

                    <div className="action-row">
                        <button type="submit" className="primary" disabled={isSubmitting}>
                            {isSubmitting
                                ? "Menyimpan..."
                                : editingId === null
                                    ? "Simpan User"
                                    : "Update User"}
                        </button>
                        {editingId !== null && (
                            <button type="button" onClick={resetForm} disabled={isSubmitting}>
                                Batal
                            </button>
                        )}
                    </div>
                </form>
            </section>

            <section className="card">
                <div className="page-header">
                    <h2>Daftar User ({users.length})</h2>
                    {users.length > 0 && (
                        <input
                            type="search"
                            aria-label="Cari username"
                            placeholder="Cari username..."
                            value={searchQuery}
                            onChange={(e) => setSearchQuery(e.target.value)}
                            style={{ maxInlineSize: "240px" }}
                        />
                    )}
                </div>

                {users.length === 0 ? (
                    <StatePanel
                        variant="empty"
                        title="Belum ada user"
                        message="Tambahkan user pertama melalui form di atas."
                    />
                ) : filteredUsers.length === 0 ? (
                    <StatePanel
                        variant="empty"
                        title="User tidak ditemukan"
                        message={`Tidak ada username yang cocok dengan "${searchQuery}".`}
                        action={
                            <button type="button" onClick={() => setSearchQuery("")}>
                                Reset Pencarian
                            </button>
                        }
                    />
                ) : (
                    <div className="table-scroll">
                        <table className="data-table">
                            <thead>
                                <tr>
                                    <th>Username</th>
                                    <th>Role</th>
                                    <th>Status</th>
                                    <th>Aksi</th>
                                </tr>
                            </thead>
                            <tbody>
                                {filteredUsers.map((user) => (
                                    <tr key={user.id}>
                                        <td>{user.username}</td>
                                        <td>
                                            <span
                                                className={`badge ${user.is_platform_admin ? "info" : ""}`}
                                            >
                                                {user.is_platform_admin ? "Admin" : "User"}
                                            </span>
                                        </td>
                                        <td>
                                            <span
                                                className={`badge ${user.is_active ? "success" : "danger"}`}
                                            >
                                                {user.is_active ? "Aktif" : "Nonaktif"}
                                            </span>
                                        </td>
                                        <td>
                                            <button
                                                type="button"
                                                className="btn-sm"
                                                onClick={() => startEdit(user)}
                                            >
                                                Edit
                                            </button>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </section>
        </div>
    );
}