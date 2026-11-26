"use client";

import { useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { Alert, Badge, Button, Card, Field, Input, Select, Spinner } from "@/components/ui";
import { api, errorMessage } from "@/lib/api";
import type { AdminGroup, AdminUser, TargetDatabase } from "@/lib/types";
import { useLoad } from "@/lib/useLoad";

export default function GroupsPanel() {
  const { refresh } = useAuth();
  const groups = useLoad(() => api<AdminGroup[]>("/api/admin/groups"));
  const users = useLoad(() => api<AdminUser[]>("/api/admin/users"));
  const targets = useLoad(() => api<TargetDatabase[]>("/api/admin/targets"));
  const [name, setName] = useState("");
  const [msg, setMsg] = useState<{ kind: "error" | "success"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  async function run(action: () => Promise<unknown>, success: string) {
    setMsg(null);
    setBusy(true);
    try {
      await action();
      setMsg({ kind: "success", text: success });
      await Promise.all([groups.reload(), users.reload()]);
      await refresh();
    } catch (e) {
      setMsg({ kind: "error", text: errorMessage(e) });
    } finally {
      setBusy(false);
    }
  }

  const createGroup = (e: React.FormEvent) => {
    e.preventDefault();
    void run(async () => {
      await api("/api/admin/groups", { method: "POST", body: { name } });
      setName("");
    }, "Group dibuat.");
  };

  return (
    <div className="space-y-6">
      {msg && <Alert kind={msg.kind}>{msg.text}</Alert>}
      <Card title="Buat Group">
        <form onSubmit={createGroup} className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div className="sm:w-80">
            <Field label="Nama group"><Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
          </div>
          <Button type="submit" disabled={busy}>Buat</Button>
        </form>
      </Card>

      {groups.loading && <Spinner />}
      {groups.error && <Alert>{groups.error}</Alert>}
      {groups.data?.length === 0 && <p className="text-sm text-slate-500">Belum ada group.</p>}

      {groups.data?.map((g) => {
        const memberIds = new Set(g.members.map((m) => m.id));
        const candidates = (users.data ?? []).filter((u) => u.is_active && !memberIds.has(u.id));
        const grantOf = (dbId: number) => g.grants.find((x) => x.database_id === dbId)?.access_level ?? 0;
        return (
          <Card
            key={g.id}
            title={
              <span className="flex flex-wrap items-center gap-2">
                {g.name}
                {g.leader_id ? <Badge tone="indigo">leader: {g.leader_username}</Badge> : <Badge tone="amber">belum ada leader</Badge>}
              </span>
            }
          >
            <div className="grid gap-6 lg:grid-cols-3">
              <div>
                <h3 className="mb-2 text-sm font-medium text-slate-700">Leader</h3>
                <Select
                  aria-label={`Leader group ${g.name}`}
                  disabled={busy || g.members.length === 0}
                  value={g.leader_id ?? ""}
                  onChange={(e) =>
                    e.target.value && run(() => api(`/api/admin/groups/${g.id}/leader`, { method: "PUT", body: { user_id: Number(e.target.value) } }), `Leader ${g.name} diperbarui; sesi leader perlu login ulang.`)
                  }
                >
                  <option value="">{g.members.length ? "— pilih leader —" : "Tambahkan anggota dulu"}</option>
                  {g.members.filter((m) => m.is_active).map((m) => (
                    <option key={m.id} value={m.id}>{m.username}</option>
                  ))}
                </Select>
                <p className="mt-1 text-xs text-slate-500">Leader wajib anggota aktif group.</p>
              </div>

              <div>
                <h3 className="mb-2 text-sm font-medium text-slate-700">Anggota ({g.members.length})</h3>
                <ul className="mb-3 space-y-1">
                  {g.members.map((m) => (
                    <li key={m.id} className="flex items-center justify-between gap-2 text-sm">
                      <span className="truncate">{m.username}{!m.is_active && " (nonaktif)"}</span>
                      <button
                        type="button"
                        className="min-h-11 rounded px-2 text-xs text-red-600 hover:bg-red-50 disabled:text-slate-300"
                        disabled={busy || g.leader_id === m.id}
                        title={g.leader_id === m.id ? "Leader tidak dapat dikeluarkan" : undefined}
                        onClick={() => run(() => api(`/api/admin/groups/${g.id}/members/${m.id}`, { method: "DELETE" }), `${m.username} dikeluarkan dari ${g.name}.`)}
                      >
                        Keluarkan
                      </button>
                    </li>
                  ))}
                </ul>
                <Select
                  aria-label={`Tambah anggota ke ${g.name}`}
                  disabled={busy || candidates.length === 0}
                  value=""
                  onChange={(e) =>
                    e.target.value && run(() => api(`/api/admin/groups/${g.id}/members`, { method: "POST", body: { user_id: Number(e.target.value) } }), "Anggota ditambahkan; sesinya perlu login ulang.")
                  }
                >
                  <option value="">+ tambah anggota…</option>
                  {candidates.map((u) => (
                    <option key={u.id} value={u.id}>{u.username}</option>
                  ))}
                </Select>
              </div>

              <div>
                <h3 className="mb-2 text-sm font-medium text-slate-700">Akses Database</h3>
                {targets.data?.length === 0 && <p className="text-xs text-slate-500">Belum ada database target.</p>}
                <ul className="space-y-2">
                  {targets.data?.map((t) => (
                    <li key={t.id} className="flex items-center justify-between gap-2">
                      <span className="min-w-0 truncate text-sm">{t.name}</span>
                      <Select
                        aria-label={`Akses ${g.name} ke ${t.name}`}
                        className="w-40 shrink-0"
                        disabled={busy}
                        value={grantOf(t.id)}
                        onChange={(e) =>
                          run(() => api(`/api/admin/groups/${g.id}/access`, { method: "PUT", body: { database_id: t.id, access_level: Number(e.target.value) } }), `Akses ${g.name} → ${t.name} diperbarui.`)
                        }
                      >
                        <option value={0}>Tanpa akses</option>
                        <option value={1}>View only</option>
                        <option value={2}>View + Edit</option>
                      </Select>
                    </li>
                  ))}
                </ul>
                <p className="mt-2 text-xs text-slate-500">Leader setiap group dengan akses (view maupun edit) wajib approve perubahan.</p>
              </div>
            </div>
          </Card>
        );
      })}
    </div>
  );
}
