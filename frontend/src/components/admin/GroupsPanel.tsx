import { useEffect, useState, type FormEvent } from "react";
import {
    addGroupMember,
    createAdminGroup,
    deleteAdminGroup,
    fetchGroupMembers,
    removeGroupMember,
    renameAdminGroup,
    setGroupLeader,
} from "../../api/admin";
import { ApiRequestError } from "../../types/auth";
import type { Group, User } from "../../types/admin";

interface GroupsPanelProps {
    groups: Group[];
    users: User[];
    onRefresh: () => Promise<void>;
}

export function GroupsPanel({ groups, users, onRefresh }: GroupsPanelProps) {
    const [newGroupName, setNewGroupName] = useState("");
    const [editingGroupId, setEditingGroupId] = useState<number | null>(null);
    const [editingName, setEditingName] = useState("");
    const [selectedGroupId, setSelectedGroupId] = useState<number | null>(null);
    const [leaderUserId, setLeaderUserId] = useState("");

    const [members, setMembers] = useState<User[]>([]);
    const [isLoadingMembers, setIsLoadingMembers] = useState(false);
    const [memberReloadTick, setMemberReloadTick] = useState(0);
    const [selectedUserId, setSelectedUserId] = useState("");

    const [error, setError] = useState("");
    const [notice, setNotice] = useState("");
    const [isSubmitting, setIsSubmitting] = useState(false);

    // Memilih group pertama secara default kalau belum ada yang dipilih atau group lama baru dihapus.
    const selectedGroup = groups.find((g) => g.id === selectedGroupId) ?? groups[0] ?? null;
    const selectedGroupIdValue = selectedGroup?.id ?? null;

    useEffect(() => {
        let active = true;

        Promise.resolve().then(() => {
            if (!active) return;
            if (selectedGroupIdValue === null) {
                setMembers([]);
                return;
            }

            setIsLoadingMembers(true);
            void fetchGroupMembers(selectedGroupIdValue)
                .then((data) => {
                    if (!active) return;
                    setMembers(data);
                })
                .catch((err) => {
                    if (!active) return;
                    setError(
                        err instanceof ApiRequestError
                            ? err.message
                            : "Gagal memuat anggota group.",
                    );
                })
                .finally(() => {
                    if (active) setIsLoadingMembers(false);
                });
        });

        return () => {
            active = false;
        };
    }, [selectedGroupIdValue, memberReloadTick]);

    const memberIds = new Set(members.map((m) => m.id));
    const availableUsers = users.filter((u) => !memberIds.has(u.id));
    const effectiveLeaderId =
        leaderUserId !== "" ? leaderUserId : String(selectedGroup?.leader_id ?? "");

    async function handleCreateGroup(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        setError("");
        setNotice("");
        setIsSubmitting(true);

        try {
            const created = await createAdminGroup(newGroupName.trim());
            setNewGroupName("");
            setSelectedGroupId(created.id);
            setLeaderUserId("");
            setNotice(`Group "${created.name}" berhasil dibuat.`);
            await onRefresh();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal membuat group.");
        } finally {
            setIsSubmitting(false);
        }
    }

    async function handleRenameGroup(groupId: number) {
        setError("");
        setNotice("");
        setIsSubmitting(true);

        try {
            await renameAdminGroup(groupId, editingName.trim());
            setEditingGroupId(null);
            setEditingName("");
            setNotice("Nama group berhasil diperbarui.");
            await onRefresh();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal mengganti nama group.");
        } finally {
            setIsSubmitting(false);
        }
    }

    async function handleDeleteGroup(group: Group) {
        if (!window.confirm(`Hapus group "${group.name}" beserta membership & grant terkait?`)) {
            return;
        }
        setError("");
        setNotice("");
        setIsSubmitting(true);

        try {
            await deleteAdminGroup(group.id);
            setNotice(`Group "${group.name}" berhasil dihapus.`);
            await onRefresh();
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal menghapus group.");
        } finally {
            setIsSubmitting(false);
        }
    }

    async function handleSetLeader(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        if (!selectedGroup) return;

        setError("");
        setNotice("");
        setIsSubmitting(true);

        try {
            const nextLeaderId = effectiveLeaderId ? Number(effectiveLeaderId) : null;
            await setGroupLeader(selectedGroup.id, nextLeaderId);
            setLeaderUserId("");
            setNotice(`Leader group "${selectedGroup.name}" berhasil diperbarui.`);
            await onRefresh();
        } catch (err) {
            setError(
                err instanceof ApiRequestError ? err.message : "Gagal mengatur leader group.",
            );
        } finally {
            setIsSubmitting(false);
        }
    }

    async function handleAddMember(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        if (!selectedGroup || !selectedUserId) return;

        setError("");
        setNotice("");
        setIsSubmitting(true);

        try {
            await addGroupMember(selectedGroup.id, Number(selectedUserId));
            setSelectedUserId("");
            setNotice("Anggota berhasil ditambahkan.");
            setMemberReloadTick((prev) => prev + 1);
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal menambah anggota.");
        } finally {
            setIsSubmitting(false);
        }
    }

    async function handleRemoveMember(user: User) {
        if (!selectedGroup) return;
        setError("");
        setNotice("");
        setIsSubmitting(true);

        try {
            await removeGroupMember(selectedGroup.id, user.id);
            setNotice(`"${user.username}" dikeluarkan dari ${selectedGroup.name}.`);
            setMemberReloadTick((prev) => prev + 1);
        } catch (err) {
            setError(err instanceof ApiRequestError ? err.message : "Gagal mengeluarkan anggota.");
        } finally {
            setIsSubmitting(false);
        }
    }

    return (
        <div className="stack">
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

            <section className="card">
                <h2>Tambah Group</h2>
                <form className="inline-form" onSubmit={handleCreateGroup}>
                    <input
                        type="text"
                        placeholder="Nama group (misal: Backend Team)"
                        maxLength={100}
                        value={newGroupName}
                        onChange={(e) => setNewGroupName(e.target.value)}
                        required
                    />
                    <button type="submit" className="primary" disabled={isSubmitting}>
                        Tambah Group
                    </button>
                </form>
            </section>

            <div className="explorer-grid">
                <section className="card">
                    <h2>Daftar Group ({groups.length})</h2>
                    {groups.length === 0 ? (
                        <p className="muted">Belum ada group.</p>
                    ) : (
                        <div className="stack">
                            {groups.map((group) => (
                                <div
                                    key={group.id}
                                    className={`group-item ${selectedGroup?.id === group.id ? "is-active" : ""}`}
                                >
                                    {editingGroupId === group.id ? (
                                        <div className="action-row">
                                            <input
                                                type="text"
                                                maxLength={100}
                                                value={editingName}
                                                onChange={(e) => setEditingName(e.target.value)}
                                            />
                                            <button
                                                type="button"
                                                className="btn-sm primary"
                                                disabled={isSubmitting}
                                                onClick={() => handleRenameGroup(group.id)}
                                            >
                                                Simpan
                                            </button>
                                            <button
                                                type="button"
                                                className="btn-sm"
                                                onClick={() => setEditingGroupId(null)}
                                            >
                                                Batal
                                            </button>
                                        </div>
                                    ) : (
                                        <>
                                            <button
                                                type="button"
                                                className="table-item"
                                                onClick={() => {
                                                    setSelectedGroupId(group.id);
                                                    setLeaderUserId("");
                                                }}
                                            >
                                                <div>{group.name}</div>
                                                <small className="muted">
                                                    {group.leader_username
                                                        ? `Leader: ${group.leader_username}`
                                                        : "Leader belum ditetapkan"}
                                                </small>
                                            </button>
                                            <div className="action-row">
                                                <button
                                                    type="button"
                                                    className="btn-sm"
                                                    onClick={() => {
                                                        setEditingGroupId(group.id);
                                                        setEditingName(group.name);
                                                    }}
                                                >
                                                    Rename
                                                </button>
                                                <button
                                                    type="button"
                                                    className="btn-sm danger"
                                                    disabled={isSubmitting}
                                                    onClick={() => handleDeleteGroup(group)}
                                                >
                                                    Hapus
                                                </button>
                                            </div>
                                        </>
                                    )}
                                </div>
                            ))}
                        </div>
                    )}
                </section>

                <section className="card">
                    {!selectedGroup ? (
                        <p className="muted">Pilih atau buat group untuk mengatur anggotanya.</p>
                    ) : (
                        <div className="stack">
                            <h2>Anggota: {selectedGroup.name}</h2>

                            <form className="inline-form" onSubmit={handleSetLeader}>
                                <label htmlFor="group-leader-select">Leader:</label>
                                <select
                                    id="group-leader-select"
                                    value={effectiveLeaderId}
                                    onChange={(e) => setLeaderUserId(e.target.value)}
                                >
                                    <option value="">-- Belum ditentukan --</option>
                                    {users
                                        .filter((u) => u.is_active)
                                        .map((u) => (
                                            <option key={u.id} value={u.id}>
                                                {u.username}
                                            </option>
                                        ))}
                                </select>
                                <button type="submit" disabled={isSubmitting}>
                                    Simpan Leader
                                </button>
                            </form>

                            <form className="inline-form" onSubmit={handleAddMember}>
                                <select
                                    aria-label="Pilih user untuk ditambahkan"
                                    value={selectedUserId}
                                    onChange={(e) => setSelectedUserId(e.target.value)}
                                    disabled={availableUsers.length === 0}
                                    required
                                >
                                    <option value="">
                                        {availableUsers.length === 0
                                            ? "-- Semua user sudah menjadi anggota --"
                                            : "-- Pilih user untuk ditambahkan --"}
                                    </option>
                                    {availableUsers.map((u) => (
                                        <option key={u.id} value={u.id}>
                                            {u.username} {!u.is_active ? "(Nonaktif)" : ""}
                                        </option>
                                    ))}
                                </select>
                                <button
                                    type="submit"
                                    className="primary"
                                    disabled={isSubmitting || availableUsers.length === 0}
                                >
                                    Tambah
                                </button>
                            </form>

                            {isLoadingMembers ? (
                                <p className="muted">Memuat anggota group...</p>
                            ) : members.length === 0 ? (
                                <p className="muted">Group ini belum punya anggota.</p>
                            ) : (
                                <div className="table-scroll">
                                    <table className="data-table">
                                        <thead>
                                            <tr>
                                                <th>Username</th>
                                                <th>Role</th>
                                                <th>Aksi</th>
                                            </tr>
                                        </thead>
                                        <tbody>
                                            {members.map((member) => (
                                                <tr key={member.id}>
                                                    <td>{member.username}</td>
                                                    <td>
                                                        {member.is_platform_admin ? "Admin" : "User"}
                                                    </td>
                                                    <td>
                                                        <button
                                                            type="button"
                                                            className="btn-sm"
                                                            disabled={isSubmitting}
                                                            onClick={() => handleRemoveMember(member)}
                                                        >
                                                            Keluarkan
                                                        </button>
                                                    </td>
                                                </tr>
                                            ))}
                                        </tbody>
                                    </table>
                                </div>
                            )}
                        </div>
                    )}
                </section>
            </div>
        </div>
    );
}