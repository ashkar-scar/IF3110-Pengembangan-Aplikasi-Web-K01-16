import { createContext, useContext } from "react";
import type { User } from "../types/auth";

export interface AuthContextValue {
    user: User | null;
    isLoading: boolean;
    login: (username: string, password: string) => Promise<void>;
    logout: () => Promise<void>;
}

export const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth() {
    const ctx = useContext(AuthContext);
    if (!ctx) {
        throw new Error("useAuth harus dipakai di dalam AuthProvider");
    }
    return ctx;
}
