"use client";

import { useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { Alert, Badge, Button, Card, Field, Input, Spinner } from "@/components/ui";
import { api, errorMessage } from "@/lib/api";
import type { AdminUser } from "@/lib/types";
import { useLoad } from "@/lib/useLoad";

export default function UsersPanel() {
  const { user: me, refresh } = useAuth();
  const { data: users, error, loading, reload } = useLoad(() => api<AdminUser[]>("/api/admin/users"));
  const [msg, setMsg] = useState<{ kind: "error" | "success"; text: string } | null>(null);
  const [form, setForm] = useState({ username: "", password: "", is_admin: false });
  const [busy, setBusy] = useState(false);

  async function run(action: () => Promise<unknown>, success: string) {
    setMsg(null);
    setBusy(true);
    try {
      await action();
      setMsg({ kind: "success", text: success });
      await reload();
      await refresh(); // privilege diri sendiri bisa berubah
    } catch (e) {
      setMsg({ kind: "error", text: errorMessage(e) });
    } finally {
      setBusy(false);
    }
  }

  const create = (e: React.FormEvent) => {
    e.preventDefault();
    void run(async () => {
      await api("/api/admin/users", { method: "POST", body: form });
      setForm({ username: "", password: "", is_admin: false });
    }, "User berhasil dibuat.");
  };

  const patch = (u: AdminUser, body: { is_active?: boolean; is_admin?: boolean }, success: string) =>
    run(() => api(`/api/admin/users/${u.id}/status`, { method: "PATCH", body }), success);

  return (
    <div className="space-y-6">
      {msg && <Alert kind={msg.kind}>{msg.text}</Alert>}
      <Card title="Tambah User">
        <form onSubmit={create} className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4 lg:items-end">
          <Field label="Username" hint="3–50 karakter: huruf, angka, _ . -">
            <Input required value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} />
          </Field>
          <Field label="Password" hint="Minimal 8 karakter">
            <Input type="password" autoComplete="new-password" required minLength={8} value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
          </Field>
          <label className="flex min-h-11 items-center gap-2 text-sm">
            <input type="checkbox" className="h-4 w-4" checked={form.is_admin} onChange={(e) => setForm({ ...form, is_admin: e.target.checked })} />
            Platform Admin
          </label>
          <Button type="submit" disabled={busy}>
            Tambah
          </Button>
        </form>
      </Card>

      <Card title="Daftar User">
        {loading && <Spinner />}
        {error && <Alert>{error}</Alert>}
        <ul className="divide-y divide-slate-100">
          {users?.map((u) => (
            <li key={u.id} className="flex flex-col gap-3 py-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="truncate font-medium text-slate-900">{u.username}</span>
                  {u.is_admin && <Badge tone="indigo">admin</Badge>}
                  <Badge tone={u.is_active ? "green" : "red"}>{u.is_active ? "aktif" : "nonaktif"}</Badge>
                </div>
                <p className="mt-1 text-xs text-slate-500">
                  Group: {u.groups.length ? u.groups.map((g) => g.name).join(", ") : "—"}
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button variant="secondary" disabled={busy} onClick={() => patch(u, { is_admin: !u.is_admin }, `Peran ${u.username} diperbarui; sesinya perlu login ulang.`)}>
                  {u.is_admin ? "Cabut admin" : "Jadikan admin"}
                </Button>
                <Button
                  variant={u.is_active ? "danger" : "secondary"}
                  disabled={busy || u.id === me?.id}
                  onClick={() => patch(u, { is_active: !u.is_active }, `Status ${u.username} diperbarui.`)}
                >
                  {u.is_active ? "Nonaktifkan" : "Aktifkan"}
                </Button>
              </div>
            </li>
          ))}
        </ul>
      </Card>
    </div>
  );
}
