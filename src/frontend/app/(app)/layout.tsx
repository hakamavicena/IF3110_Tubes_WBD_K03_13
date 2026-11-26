"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import AppShell from "@/components/AppShell";
import { useAuth } from "@/components/AuthProvider";
import { Spinner } from "@/components/ui";

// Guard: seluruh halaman di grup (app) wajib login.
export default function AppLayout({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth();
  const router = useRouter();
  useEffect(() => {
    if (!loading && !user) router.replace("/login");
  }, [loading, user, router]);

  if (loading || !user) return <Spinner label="Memeriksa sesi…" />;
  return <AppShell>{children}</AppShell>;
}
