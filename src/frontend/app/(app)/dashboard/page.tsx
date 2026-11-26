"use client";

import Link from "next/link";
import { useAuth } from "@/components/AuthProvider";
import { Card } from "@/components/ui";

export default function DashboardPage() {
  const { user, leaderGroups } = useAuth();
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold text-slate-900">Halo, {user?.username}</h1>
      <Card title="Ringkasan akun">
        <dl className="grid gap-3 text-sm sm:grid-cols-2">
          <div>
            <dt className="text-slate-500">Peran platform</dt>
            <dd className="font-medium">{user?.is_admin ? "Platform Admin" : "Pengguna"}</dd>
          </div>
          <div>
            <dt className="text-slate-500">Leader group</dt>
            <dd className="font-medium">{leaderGroups.length ? leaderGroups.map((g) => g.name).join(", ") : "—"}</dd>
          </div>
        </dl>
        {leaderGroups.length > 0 && (
          <Link href="/approvals" className="mt-4 inline-flex min-h-11 items-center text-sm font-medium text-indigo-700 hover:underline">
            Buka inbox approval →
          </Link>
        )}
      </Card>
    </div>
  );
}
