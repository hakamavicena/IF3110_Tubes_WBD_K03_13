"use client";

import React, { useEffect, useState } from "react";
import {
  enqueueJob,
  fetchJobStatus,
  MigrationJob,
  MigrationStatus,
  requestCancelBatch,
  requestRetryBatch,
} from "../api";

interface JobProgressMonitorProps {
  dbId: string | number;
  migrationId: number;
  isIrreversible: boolean;
  hasSQLDown: boolean;
  canEdit: boolean;
  initialJob: MigrationJob | null;
  onJobUpdated?: (job: MigrationJob) => void;
}

export function statusBadgeClass(status: MigrationStatus): string {
  switch (status) {
    case "success":
      return "bg-emerald-100 text-emerald-800 border-emerald-300";
    case "running":
    case "pending_queue":
      return "bg-blue-100 text-blue-800 border-blue-300";
    case "approved":
      return "bg-indigo-100 text-indigo-800 border-indigo-300";
    case "pending_review":
      return "bg-amber-100 text-amber-800 border-amber-300";
    case "partial":
    case "partially_reverted":
      return "bg-orange-100 text-orange-800 border-orange-300";
    case "failed":
      return "bg-red-100 text-red-800 border-red-300";
    default:
      return "bg-slate-100 text-slate-800 border-slate-300";
  }
}

export default function JobProgressMonitor({
  dbId,
  migrationId,
  isIrreversible,
  hasSQLDown,
  canEdit,
  initialJob,
  onJobUpdated,
}: JobProgressMonitorProps) {
  const [job, setJob] = useState<MigrationJob | null>(initialJob);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    setJob(initialJob);
  }, [initialJob]);

  useEffect(() => {
    if (!job) return;

    const isActivePolling =
      job.status === "pending_queue" || job.status === "running";

    if (!isActivePolling) return;

    const timer = setInterval(async () => {
      try {
        const res = await fetchJobStatus(dbId, job.id);
        setJob(res.job);
        onJobUpdated?.(res.job);
      } catch {
        // Abaikan error jaringan sesaat saat polling
      }
    }, 1500);

    return () => clearInterval(timer);
  }, [dbId, job, onJobUpdated]);

  if (!job) {
    return (
      <div className="rounded-xl border border-slate-200 bg-slate-50 p-4 text-sm text-slate-600">
        Belum ada job eksekusi untuk migration ini.
      </div>
    );
  }

  const handleEnqueue = async () => {
    setActionError(null);
    setActionLoading("enqueue");
    try {
      const res = await enqueueJob(dbId, job.id);
      setJob(res.job);
      onJobUpdated?.(res.job);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Gagal melakukan enqueue job");
    } finally {
      setActionLoading(null);
    }
  };

  const handleRetryBatch = async () => {
    setActionError(null);
    setActionLoading("retry");
    try {
      const res = await requestRetryBatch(dbId, migrationId);
      setJob(res.job);
      onJobUpdated?.(res.job);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Gagal mengajukan Retry dari Checkpoint");
    } finally {
      setActionLoading(null);
    }
  };

  const handleCancelBatch = async () => {
    setActionError(null);
    setActionLoading("cancel");
    try {
      const res = await requestCancelBatch(dbId, migrationId);
      setJob(res.job);
      onJobUpdated?.(res.job);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Gagal mengajukan Cancel Batch");
    } finally {
      setActionLoading(null);
    }
  };

  const isPolling = job.status === "pending_queue" || job.status === "running";
  const isPartialState =
    job.status === "partial" || job.status === "partially_reverted";

  return (
    <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="flex items-center gap-2">
            <h3 className="text-base font-semibold text-slate-900">
              Live Job &amp; Batch Progress Monitor
            </h3>
            {isPolling && (
              <span className="inline-flex items-center gap-1.5 rounded-full bg-blue-50 px-2.5 py-0.5 text-xs font-medium text-blue-700">
                <span className="h-2 w-2 animate-ping rounded-full bg-blue-600" />
                Live Polling
              </span>
            )}
          </div>
          <p className="mt-0.5 text-xs text-slate-500">
            Job #{job.id} &bull; Action:{" "}
            <span className="font-mono font-medium text-slate-700">
              {job.action}
            </span>{" "}
            &bull; Mode:{" "}
            <span className="font-mono font-medium text-slate-700">
              {job.execution_mode}
            </span>
          </p>
        </div>

        <span
          className={`inline-flex items-center self-start rounded-full border px-3 py-1 text-xs font-semibold uppercase tracking-wide sm:self-auto ${statusBadgeClass(
            job.status
          )}`}
        >
          {job.status}
        </span>
      </div>

      <div className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <div className="rounded-lg border border-slate-100 bg-slate-50 p-3">
          <div className="text-xs text-slate-500">Committed Batches</div>
          <div className="mt-1 text-xl font-bold text-slate-900">
            {job.committed_batches}
          </div>
        </div>
        <div className="rounded-lg border border-slate-100 bg-slate-50 p-3">
          <div className="text-xs text-slate-500">Committed Rows</div>
          <div className="mt-1 text-xl font-bold text-slate-900">
            {job.committed_rows.toLocaleString()}
          </div>
        </div>
        <div className="rounded-lg border border-slate-100 bg-slate-50 p-3">
          <div className="text-xs text-slate-500">Last Checkpoint Cursor</div>
          <div className="mt-1 truncate font-mono text-sm font-semibold text-slate-800">
            {job.last_cursor || "—"}
          </div>
        </div>
        <div className="rounded-lg border border-slate-100 bg-slate-50 p-3">
          <div className="text-xs text-slate-500">Duration</div>
          <div className="mt-1 text-sm font-semibold text-slate-800">
            {typeof job.duration_ms === "number" ? `${job.duration_ms} ms` : "—"}
          </div>
        </div>
      </div>

      {job.execution_mode === "batch" && (
        <div className="mt-3 flex flex-wrap items-center gap-4 text-xs text-slate-600">
          <span>
            Batch Column:{" "}
            <code className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-slate-800">
              {job.batch_column || "id"}
            </code>
          </span>
          <span>
            Batch Size:{" "}
            <code className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-slate-800">
              {job.batch_size || 0} rows/tx
            </code>
          </span>
          <span>Retry Count: {job.retry_count}</span>
        </div>
      )}

      {job.error_message && (
        <div className="mt-4 rounded-lg border border-red-200 bg-red-50 p-3 text-xs text-red-800">
          <div className="font-semibold">Pesan Error Eksekusi:</div>
          <pre className="mt-1 whitespace-pre-wrap font-mono">
            {job.error_message}
          </pre>
        </div>
      )}

      {isPartialState && (
        <div className="mt-4 rounded-lg border border-orange-200 bg-orange-50 p-3 text-xs text-orange-900">
          <div className="font-semibold">
            Perhatian: Eksekusi Batch Berhenti Sebagian ({job.status})
          </div>
          <p className="mt-1">
            Sebanyak <strong>{job.committed_batches} batch</strong> (
            {job.committed_rows.toLocaleString()} baris) hingga cursor{" "}
            <code className="font-mono font-semibold">
              {job.last_cursor || "-"}
            </code>{" "}
            telah di-commit secara permanen di database target. Lock database
            ditahan (<code className="font-mono">holding_partial</code>) agar
            migration berikutnya tidak mendahului. Pilih{" "}
            <strong>Retry dari Checkpoint</strong> untuk melanjutkan sisa batch,
            atau <strong>Cancel / Revert Batch</strong> untuk membatalkan batch
            dalam urutan terbalik (LIFO).
          </p>
        </div>
      )}

      {actionError && (
        <div className="mt-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">
          {actionError}
        </div>
      )}

      {canEdit && (
        <div className="mt-4 flex flex-wrap items-center gap-2.5 border-t border-slate-100 pt-4">
          {job.status === "approved" && (
            <button
              type="button"
              onClick={handleEnqueue}
              disabled={actionLoading !== null}
              className="inline-flex items-center justify-center rounded-lg bg-blue-600 px-4 py-2 text-xs font-semibold text-white shadow-sm transition hover:bg-blue-700 disabled:opacity-50"
            >
              {actionLoading === "enqueue"
                ? "Mengirim ke Redis Queue..."
                : "Enqueue & Execute Job"}
            </button>
          )}

          {isPartialState && (
            <>
              <button
                type="button"
                onClick={handleRetryBatch}
                disabled={actionLoading !== null}
                className="inline-flex items-center justify-center rounded-lg bg-amber-600 px-4 py-2 text-xs font-semibold text-white shadow-sm transition hover:bg-amber-700 disabled:opacity-50"
              >
                {actionLoading === "retry"
                  ? "Membuat Job Retry..."
                  : "Lanjutkan dari Checkpoint (Retry Batch)"}
              </button>

              {!isIrreversible && hasSQLDown && (
                <button
                  type="button"
                  onClick={handleCancelBatch}
                  disabled={actionLoading !== null}
                  className="inline-flex items-center justify-center rounded-lg border border-red-300 bg-white px-4 py-2 text-xs font-semibold text-red-700 shadow-sm transition hover:bg-red-50 disabled:opacity-50"
                >
                  {actionLoading === "cancel"
                    ? "Membuat Job Revert..."
                    : "Batalkan Batch Committed (Reverse Cancel)"}
                </button>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
