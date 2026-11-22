import { useCallback, useEffect, useState, type ReactNode } from "react";
import { fetchCurrentUser, loginRequest, logoutRequest } from "../api/auth";
import { setCsrfToken, setUnauthorizedHandler } from "../api/client";
import type { User } from "../types/auth";
import { AuthContext } from "./useAuth";

export function AuthProvider({ children }: { children: ReactNode }) {
    const [user, setUser] = useState<User | null>(null);
    const [isLoading, setIsLoading] = useState(true);

    const clearSession = useCallback(() => {
        setCsrfToken("");
        setUser(null);
    }, []);

    // Memuat sesi dari cookie pas awal buka, flag active biar gak update state pas unmount.
    useEffect(() => {
        setUnauthorizedHandler(clearSession);

        let active = true;
        fetchCurrentUser()
            .then((session) => {
                if (!active) return;
                setCsrfToken(session.csrf_token);
                setUser(session.user);
            })
            .catch(() => {
                if (!active) return;
                clearSession();
            })
            .finally(() => {
                if (active) setIsLoading(false);
            });

        return () => {
            active = false;
        };
    }, [clearSession]);

    const login = useCallback(async (username: string, password: string) => {
        const session = await loginRequest(username, password);
        setCsrfToken(session.csrf_token);
        setUser(session.user);
    }, []);

    const logout = useCallback(async () => {
        try {
            await logoutRequest();
        } finally {
            clearSession();
        }
    }, [clearSession]);

    return (
        <AuthContext.Provider value={{ user, isLoading, login, logout }}>
            {children}
        </AuthContext.Provider>
    );
}