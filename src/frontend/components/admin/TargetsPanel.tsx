"use client";

import { useState } from "react";
import { Alert, Button, Card, Field, Input, Select, Spinner } from "@/components/ui";
import { api, errorMessage } from "@/lib/api";
import type { TargetDatabase } from "@/lib/types";
import { useLoad } from "@/lib/useLoad";

const empty = { name: "", host: "", port: "5432", database_name: "", username: "", password: "", ssl_mode: "prefer" };

export default function TargetsPanel() {
  const { data: targets, error, loading, reload } = useLoad(() => api<TargetDatabase[]>("/api/admin/targets"));
  const [form, setForm] = useState(empty);
  const [msg, setMsg] = useState<{ kind: "error" | "success"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (k: keyof typeof empty) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setForm({ ...form, [k]: e.target.value });

  async function create(e: React.FormEvent) {
    e.preventDefault();
    setMsg(null);
    setBusy(true);
    try {
      await api("/api/admin/targets", { method: "POST", body: { ...form, port: Number(form.port) } });
      setForm(empty);
      setMsg({ kind: "success", text: "Database target terdaftar (koneksi berhasil diuji)." });
      await reload();
    } catch (err) {
      setMsg({ kind: "error", text: errorMessage(err) });
    } finally {
      setBusy(false);
    }
  }

  async function remove(t: TargetDatabase) {
    if (!window.confirm(`Hapus target "${t.name}"? Seluruh migration dan akses terkait ikut terhapus.`)) return;
    setMsg(null);
    try {
      await api(`/api/admin/targets/${t.id}`, { method: "DELETE" });
      setMsg({ kind: "success", text: `Target ${t.name} dihapus.` });
      await reload();
    } catch (err) {
      setMsg({ kind: "error", text: errorMessage(err) });
    }
  }

  return (
    <div className="space-y-6">
      {msg && <Alert kind={msg.kind}>{msg.text}</Alert>}
      <Card title="Daftarkan Database Target (PostgreSQL)">
        <form onSubmit={create} className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <Field label="Nama"><Input required value={form.name} onChange={set("name")} /></Field>
          <Field label="Host"><Input required value={form.host} onChange={set("host")} /></Field>
          <Field label="Port"><Input type="number" min={1} max={65535} required value={form.port} onChange={set("port")} /></Field>
          <Field label="Nama database"><Input required value={form.database_name} onChange={set("database_name")} /></Field>
          <Field label="Username"><Input required value={form.username} onChange={set("username")} /></Field>
          <Field label="Password" hint="Disimpan terenkripsi, tidak pernah ditampilkan lagi">
            <Input type="password" autoComplete="new-password" required value={form.password} onChange={set("password")} />
          </Field>
          <Field label="SSL mode">
            <Select value={form.ssl_mode} onChange={set("ssl_mode")}>
              {["disable", "allow", "prefer", "require", "verify-ca", "verify-full"].map((m) => (
                <option key={m}>{m}</option>
              ))}
            </Select>
          </Field>
          <div className="flex items-end">
            <Button type="submit" disabled={busy} className="w-full sm:w-auto">{busy ? "Menguji koneksi…" : "Daftarkan"}</Button>
          </div>
        </form>
      </Card>

      <Card title="Database Target">
        {loading && <Spinner />}
        {error && <Alert>{error}</Alert>}
        {targets && targets.length === 0 && <p className="text-sm text-slate-500">Belum ada target.</p>}
        <ul className="divide-y divide-slate-100">
          {targets?.map((t) => (
            <li key={t.id} className="flex flex-col gap-2 py-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="min-w-0">
                <p className="truncate font-medium text-slate-900">{t.name}</p>
                <p className="truncate text-xs text-slate-500">
                  {t.username}@{t.host}:{t.port}/{t.database_name} · ssl={t.ssl_mode}
                </p>
              </div>
              <Button variant="danger" onClick={() => remove(t)}>Hapus</Button>
            </li>
          ))}
        </ul>
      </Card>
    </div>
  );
}
