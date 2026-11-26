"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { api, setCsrfToken, setUnauthorizedHandler } from "@/lib/api";
import type { AuthResponse, LeaderGroup, User } from "@/lib/types";

interface AuthState {
  user: User | null;
  leaderGroups: LeaderGroup[];
  loading: boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
}

const AuthContext = createContext<AuthState | null>(null);

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth harus dipakai di dalam AuthProvider");
  return ctx;
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [leaderGroups, setLeaderGroups] = useState<LeaderGroup[]>([]);
  const [loading, setLoading] = useState(true);

  const apply = useCallback((r: AuthResponse | null) => {
    setUser(r?.user ?? null);
    setLeaderGroups(r?.leader_groups ?? []);
    setCsrfToken(r?.csrf_token ?? "");
  }, []);

  const refresh = useCallback(async () => {
    try {
      apply(await api<AuthResponse>("/api/auth/me"));
    } catch {
      apply(null);
    } finally {
      setLoading(false);
    }
  }, [apply]);

  useEffect(() => {
    setUnauthorizedHandler(() => apply(null)); // sesi berakhir -> guard mengarahkan ke /login
    // eslint-disable-next-line react-hooks/set-state-in-effect -- fetch saat mount; state baru diset setelah await
    void refresh();
    return () => setUnauthorizedHandler(null);
  }, [apply, refresh]);

  const login = useCallback(
    async (username: string, password: string) => {
      apply(await api<AuthResponse>("/api/auth/login", { method: "POST", body: { username, password } }));
    },
    [apply],
  );

  const logout = useCallback(async () => {
    try {
      await api("/api/auth/logout", { method: "POST" });
    } finally {
      apply(null);
    }
  }, [apply]);

  const value = useMemo(
    () => ({ user, leaderGroups, loading, login, logout, refresh }),
    [user, leaderGroups, loading, login, logout, refresh],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
