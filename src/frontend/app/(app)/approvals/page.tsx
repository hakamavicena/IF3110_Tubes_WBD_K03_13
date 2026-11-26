"use client";

import { useState } from "react";
import ApprovalPanel from "@/components/ApprovalPanel";
import { useAuth } from "@/components/AuthProvider";
import { Alert, Badge, Button, Card, Spinner } from "@/components/ui";
import { api } from "@/lib/api";
import type { InboxItem } from "@/lib/types";
import { useLoad } from "@/lib/useLoad";

const ACTION_LABEL: Record<string, string> = {
  apply: "Migration",
  rollback: "Rollback",
  forward: "Forward",
  retry_batch: "Retry batch",
  cancel_batch: "Cancel batch",
};

export default function ApprovalInboxPage() {
  const { leaderGroups } = useAuth();
  const { data, error, loading, reload } = useLoad(() => api<InboxItem[]>("/api/approvals/inbox"));
  const [open, setOpen] = useState<number | null>(null);
  const [handled, setHandled] = useState<Set<number>>(new Set()); // job yang sudah selesai Anda proses (tetap tampil sampai dimuat ulang)

  if (leaderGroups.length === 0) return <Alert kind="info">Halaman ini untuk Group Leader. Anda belum menjadi leader group mana pun.</Alert>;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">Inbox Approval</h1>
          <p className="text-sm text-slate-500">Anda memimpin: {leaderGroups.map((g) => g.name).join(", ")}</p>
        </div>
        <Button variant="secondary" onClick={() => { setHandled(new Set()); setOpen(null); void reload(); }}>Muat ulang</Button>
      </div>

      {loading && <Spinner />}
      {error && <Alert>{error}</Alert>}
      {data && data.length === 0 && <Alert kind="success">Tidak ada perubahan yang menunggu persetujuan Anda.</Alert>}

      {data?.map((it) => (
        <Card
          key={it.job_id}
          title={
            <span className="flex flex-wrap items-center gap-2">
              <Badge tone="indigo">{ACTION_LABEL[it.action] ?? it.action}</Badge>
              v{it.migration_version} · {it.migration_title}
              {handled.has(it.job_id) && <Badge tone="green">Sudah Anda proses</Badge>}
            </span>
          }
          actions={
            <Button variant="secondary" onClick={() => setOpen(open === it.job_id ? null : it.job_id)}>
              {open === it.job_id ? "Tutup" : "Tinjau"}
            </Button>
          }
        >
          <p className="text-sm text-slate-600">
            Database <strong>{it.database_name}</strong> · diajukan oleh {it.requested_by} ·{" "}
            {new Date(it.created_at).toLocaleString("id-ID")}
          </p>
          <p className="mt-1 text-xs text-slate-500">Menunggu approval group: {it.waiting_groups.map((g) => g.name).join(", ")}</p>
          {open === it.job_id && (
            <div className="mt-4 border-t border-slate-100 pt-4">
              <ApprovalPanel
                dbId={it.database_id}
                jobId={it.job_id}
                onChange={(s) => {
                  if (s.status !== "pending_review" || s.can_approve_groups.length === 0) {
                    setHandled((h) => (h.has(it.job_id) ? h : new Set(h).add(it.job_id)));
                  }
                }}
              />
            </div>
          )}
        </Card>
      ))}
    </div>
  );
}
