"use client";

import { useCallback, useEffect, useState } from "react";
import { Alert, Badge, Button, Spinner } from "@/components/ui";
import { api, errorMessage } from "@/lib/api";
import type { ApprovalStatus } from "@/lib/types";

export const JOB_STATUS_LABEL: Record<string, { label: string; tone: "slate" | "amber" | "green" | "red" | "indigo" }> = {
  draft: { label: "Draft", tone: "slate" },
  pending_review: { label: "Menunggu review", tone: "amber" },
  approved: { label: "Disetujui", tone: "green" },
  pending_queue: { label: "Dalam antrean", tone: "indigo" },
  running: { label: "Berjalan", tone: "indigo" },
  success: { label: "Berhasil", tone: "green" },
  failed: { label: "Gagal", tone: "red" },
  partial: { label: "Partial", tone: "amber" },
  partially_reverted: { label: "Partially reverted", tone: "amber" },
};

export function JobStatusBadge({ status }: { status: string }) {
  const s = JOB_STATUS_LABEL[status] ?? { label: status, tone: "slate" as const };
  return <Badge tone={s.tone}>{s.label}</Badge>;
}

interface Props {
  dbId: number;
  jobId: number;
  /** Dipanggil setiap status berubah (mis. agar halaman induk menampilkan tombol Enqueue saat approved). */
  onChange?: (status: ApprovalStatus) => void;
}

/**
 * Daftar group yang wajib approve + status tiap leader + tombol Submit Review / Approve.
 * Dapat dipasang di halaman mana pun yang menampilkan sebuah job (migration, rollback, forward, batch).
 */
export default function ApprovalPanel({ dbId, jobId, onChange }: Props) {
  const base = `/api/databases/${dbId}/jobs/${jobId}`;
  const [st, setSt] = useState<ApprovalStatus | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const apply = useCallback(
    (s: ApprovalStatus) => {
      setSt(s);
      onChange?.(s);
    },
    [onChange],
  );

  const load = useCallback(async () => {
    try {
      const s = await api<ApprovalStatus>(`${base}/approvals`);
      setError("");
      apply(s);
    } catch (e) {
      setError(errorMessage(e));
    }
  }, [base, apply]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- fetch saat mount; state baru diset setelah await
    void load();
  }, [load]);

  async function act(path: string, body?: unknown) {
    setBusy(true);
    setError("");
    try {
      apply(await api<ApprovalStatus>(`${base}/${path}`, { method: "POST", body }));
    } catch (e) {
      setError(errorMessage(e));
      await load(); // status mungkin sudah berubah oleh pengguna lain
    } finally {
      setBusy(false);
    }
  }

  if (!st && !error) return <Spinner label="Memuat status approval…" />;

  const myGroups = st ? st.required.filter((r) => st.can_approve_groups.includes(r.group_id)) : [];
  const done = st ? st.required.filter((r) => r.approved).length : 0;

  return (
    <div className="space-y-3">
      {error && <Alert>{error}</Alert>}
      {st && (
        <>
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <JobStatusBadge status={st.status} />
            <span className="text-slate-600">
              {done} dari {st.required.length} group menyetujui
            </span>
          </div>

          <ul className="divide-y divide-slate-100 rounded-lg border border-slate-200">
            {st.required.map((r) => (
              <li key={r.group_id} className="flex flex-col gap-1 px-3 py-2 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-slate-900">
                    {r.group_name}{" "}
                    <span className="text-xs font-normal text-slate-500">({r.access_level === 2 ? "view + edit" : "view only"})</span>
                  </p>
                  <p className="truncate text-xs text-slate-500">
                    Leader: {r.leader_id ? r.leader_username : "belum ditetapkan"}
                    {r.leader_id && !r.leader_active && " (nonaktif)"}
                  </p>
                </div>
                <div className="self-start sm:self-auto">
                  {r.approved ? (
                    <Badge tone="green">
                      Disetujui{r.approved_at ? ` · ${new Date(r.approved_at).toLocaleString("id-ID")}` : ""}
                    </Badge>
                  ) : (
                    <Badge tone={r.leader_id && r.leader_active ? "amber" : "red"}>
                      {r.leader_id && r.leader_active ? "Menunggu" : "Tanpa leader aktif"}
                    </Badge>
                  )}
                </div>
              </li>
            ))}
          </ul>

          <div className="flex flex-col gap-2 sm:flex-row">
            {st.status === "draft" && (
              <Button disabled={busy} onClick={() => act("submit-review")}>
                {busy ? "Memproses…" : "Ajukan untuk Review"}
              </Button>
            )}
            {st.status === "pending_review" && myGroups.length > 0 && (
              <Button disabled={busy} onClick={() => act("approve")}>
                {busy ? "Memproses…" : `Approve (${myGroups.map((g) => g.group_name).join(", ")})`}
              </Button>
            )}
            <Button variant="secondary" disabled={busy} onClick={load}>
              Muat ulang
            </Button>
          </div>
          {st.status === "pending_review" && myGroups.length === 0 && (
            <p className="text-xs text-slate-500">Anda bukan leader dari group yang masih menunggu, atau sudah menyetujui.</p>
          )}
        </>
      )}
    </div>
  );
}
