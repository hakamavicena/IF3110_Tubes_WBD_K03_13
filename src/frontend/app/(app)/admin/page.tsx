"use client";

import { useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import GroupsPanel from "@/components/admin/GroupsPanel";
import TargetsPanel from "@/components/admin/TargetsPanel";
import UsersPanel from "@/components/admin/UsersPanel";
import { Alert } from "@/components/ui";

const TABS = [
  { id: "users", label: "Users" },
  { id: "groups", label: "Groups & Akses" },
  { id: "targets", label: "Target Database" },
] as const;

export default function AdminPage() {
  const { user } = useAuth();
  const [tab, setTab] = useState<(typeof TABS)[number]["id"]>("users");

  if (!user?.is_admin) return <Alert>Halaman ini hanya untuk Platform Admin.</Alert>;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold text-slate-900">Administrasi Platform</h1>
      <div role="tablist" aria-label="Bagian administrasi" className="-mx-4 flex gap-1 overflow-x-auto border-b border-slate-200 px-4">
        {TABS.map((t) => (
          <button
            key={t.id}
            role="tab"
            id={`tab-${t.id}`}
            aria-selected={tab === t.id}
            aria-controls={`panel-${t.id}`}
            onClick={() => setTab(t.id)}
            className={`min-h-11 shrink-0 border-b-2 px-4 text-sm font-medium ${
              tab === t.id ? "border-indigo-600 text-indigo-700" : "border-transparent text-slate-500 hover:text-slate-700"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === "users" && <UsersPanel />}
        {tab === "groups" && <GroupsPanel />}
        {tab === "targets" && <TargetsPanel />}
      </div>
    </div>
  );
}
