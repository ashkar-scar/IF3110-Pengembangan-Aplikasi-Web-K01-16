import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import {
    fetchAdminGrants,
    fetchAdminGroups,
    fetchAdminTargets,
    fetchAdminUsers,
} from "../api/admin";
import { UsersPanel } from "../components/admin/UsersPanel";
import { ApiRequestError } from "../types/auth";
import type { AdminDatabaseTarget, DatabaseGrant, Group, User } from "../types/admin";
import { GrantsPanel } from "../components/admin/GrantsPanel";
import { GroupsPanel } from "../components/admin/GroupsPanel";
import { TargetsPanel } from "../components/admin/TargetsPanel";
import { StatePanel } from "../components/StatePanel";

type AdminTab = "users" | "groups" | "grants" | "targets";

const TABS: { key: AdminTab; label: string }[] = [
    { key: "users", label: "Users" },
    { key: "groups", label: "Groups" },
    { key: "grants", label: "Grants" },
    { key: "targets", label: "Targets" },
];

export function AdminPage() {
    const [searchParams, setSearchParams] = useSearchParams();
    const rawTab = searchParams.get("tab");
    const activeTab: AdminTab =
        rawTab === "groups" || rawTab === "grants" || rawTab === "targets" ? rawTab : "users";

    const [users, setUsers] = useState<User[]>([]);
    const [groups, setGroups] = useState<Group[]>([]);
    const [grants, setGrants] = useState<DatabaseGrant[]>([]);
    const [targets, setTargets] = useState<AdminDatabaseTarget[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [error, setError] = useState("");

    // Mengambil 4 data master sekaligus soalnya tab Groups & Grants saling butuh ID dari Users dan Targets.
    const loadAll = useCallback(async () => {
        setError("");
        try {
            const [usersData, groupsData, grantsData, targetsData] = await Promise.all([
                fetchAdminUsers(),
                fetchAdminGroups(),
                fetchAdminGrants(),
                fetchAdminTargets(),
            ]);
            setUsers(usersData);
            setGroups(groupsData);
            setGrants(grantsData);
            setTargets(targetsData);
        } catch (err) {
            if (err instanceof ApiRequestError) {
                setError(err.message);
            } else {
                setError("Gagal memuat data administrasi.");
            }
        } finally {
            setIsLoading(false);
        }
    }, []);

    useEffect(() => {
        let active = true;
        Promise.resolve().then(() => {
            if (active) return loadAll();
        });
        return () => {
            active = false;
        };
    }, [loadAll]);

    function selectTab(tab: AdminTab) {
        setSearchParams(tab === "users" ? {} : { tab });
    }

    return (
        <div className="stack">
            <section className="card">
                <div className="page-header">
                    <div>
                        <h1>Administrasi Platform</h1>
                        <p className="muted">
                            Kelola user, grup akses, grant database, dan koneksi target.
                        </p>
                    </div>

                    <div className="tab-group" role="tablist" aria-label="Menu administrasi">
                        {TABS.map((tab) => (
                            <button
                                key={tab.key}
                                type="button"
                                role="tab"
                                aria-selected={activeTab === tab.key}
                                className={activeTab === tab.key ? "is-active" : ""}
                                onClick={() => selectTab(tab.key)}
                            >
                                {tab.label}
                            </button>
                        ))}
                    </div>
                </div>

                {error && (
                    <StatePanel
                        variant="error"
                        title="Gagal memuat data administrasi"
                        message={error}
                        onRetry={() => {
                            setIsLoading(true);
                            void loadAll();
                        }}
                    />
                )}
            </section>

            {isLoading ? (
                <section className="card">
                    <StatePanel
                        variant="loading"
                        title="Memuat data administrasi"
                        message="Mengambil daftar users, groups, grants, dan database targets..."
                    />
                </section>
            ) : (
                <>
                    {activeTab === "users" && <UsersPanel users={users} onRefresh={loadAll} />}
                    {activeTab === "groups" && (
                        <GroupsPanel groups={groups} users={users} onRefresh={loadAll} />
                    )}
                    {activeTab === "grants" && (
                        <GrantsPanel
                            grants={grants}
                            groups={groups}
                            targets={targets}
                            onRefresh={loadAll}
                        />
                    )}
                    {activeTab === "targets" && (
                        <TargetsPanel targets={targets} onRefresh={loadAll} />
                    )}
                </>
            )}
        </div>
    );
}