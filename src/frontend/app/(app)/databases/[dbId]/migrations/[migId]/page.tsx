"use client";

import React, { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import {
  DatabaseLockInfo,
  fetchMigrationDetail,
  MigrationItem,
  MigrationJob,
  requestForwardMigration,
  requestRollbackMigration,
} from "../api";
import JobProgressMonitor, {
  statusBadgeClass,
} from "../components/JobProgressMonitor";

export default function MigrationDetailPage() {
  const params = useParams<{ dbId: string; migId: string }>();
  const dbId = params?.dbId || "";
  const migId = params?.migId || "";

  const [migration, setMigration] = useState<MigrationItem | null>(null);
  const [jobs, setJobs] = useState<MigrationJob[]>([]);
  const [canEdit, setCanEdit] = useState(false);
  const [activeLock, setActiveLock] = useState<DatabaseLockInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionBusy, setActionBusy] = useState<string | null>(null);
  const [actionErr, setActionErr] = useState<string | null>(null);

  const loadDetail = useCallback(async () => {
    if (!dbId || !migId) return;
    setLoading(true);
    setError(null);
    try {
      const res = await fetchMigrationDetail(dbId, migId);
      setMigration(res.migration);
      setJobs(res.jobs || []);
      setCanEdit(Boolean(res.can_edit));
      setActiveLock(res.active_lock || null);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Gagal memuat detail migration"
      );
    } finally {
      setLoading(false);
    }
  }, [dbId, migId]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- fetch saat mount; state baru diset setelah await
    void loadDetail();
  }, [loadDetail]);

  const latestJob = jobs.length > 0 ? jobs[0] : null;

  const handleJobUpdated = useCallback((updatedJob: MigrationJob) => {
    setJobs((prev) => {
      const exists = prev.some((j) => j.id === updatedJob.id);
      if (!exists) {
        return [updatedJob, ...prev];
      }
      return prev.map((j) => (j.id === updatedJob.id ? updatedJob : j));
    });
    setMigration((prev) =>
      prev ? { ...prev, status: updatedJob.status } : prev
    );
  }, []);

  const handleRequestRollbackOrForward = async (kind: "rollback" | "forward") => {
    setActionErr(null);
    setActionBusy(kind);
    try {
      const res =
        kind === "rollback"
          ? await requestRollbackMigration(dbId, migId)
          : await requestForwardMigration(dbId, migId);
      setMigration(res.migration);
      handleJobUpdated(res.job);
    } catch (err) {
      setActionErr(
        err instanceof Error ? err.message : `Gagal mengajukan ${kind}`
      );
    } finally {
      setActionBusy(null);
    }
  };

  if (loading) {
    return (
      <main className="mx-auto max-w-5xl px-4 py-8 text-center text-sm text-slate-500">
        Memuat detail migration...
      </main>
    );
  }

  if (error || !migration) {
    return (
      <main className="mx-auto max-w-5xl px-4 py-8">
        <div className="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          {error || "Migration tidak ditemukan"}
        </div>
        <Link
          href={`/databases/${dbId}/migrations`}
          className="mt-4 inline-block text-xs font-semibold text-blue-600 hover:underline"
        >
          &larr; Kembali ke Daftar Migration
        </Link>
      </main>
    );
  }

  return (
    <main className="mx-auto max-w-5xl space-y-6 px-4 py-6 sm:px-6 lg:px-8">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <Link
            href={`/databases/${dbId}/migrations`}
            className="text-xs font-semibold text-blue-600 hover:underline"
          >
            &larr; Kembali ke Daftar Migration
          </Link>
          <div className="mt-1 flex flex-wrap items-center gap-2.5">
            <h1 className="text-xl font-bold text-slate-900 sm:text-2xl">
              v{migration.version}: {migration.title}
            </h1>
            <span
              className={`rounded-full border px-3 py-0.5 text-xs font-semibold uppercase ${statusBadgeClass(
                migration.status
              )}`}
            >
              {migration.status}
            </span>
            {migration.is_immutable && (
              <span className="rounded-full bg-slate-800 px-2.5 py-0.5 text-[11px] font-semibold text-white">
                Immutable
              </span>
            )}
          </div>
          <p className="mt-0.5 text-xs text-slate-500">
            Revisi #{migration.revision} &bull; Dibuat oleh{" "}
            <strong>{migration.created_by_name}</strong> &bull; Active State:{" "}
            <code className="font-mono font-semibold text-slate-700">
              {migration.active_state}
            </code>
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2 self-start sm:self-auto">
          {canEdit && !migration.is_immutable && (
            <Link
              href={`/databases/${dbId}/migrations/${migration.id}/edit`}
              className="rounded-lg border border-slate-300 bg-white px-4 py-2 text-xs font-semibold text-slate-700 hover:bg-slate-50"
            >
              Edit Draft Migration
            </Link>
          )}
          {canEdit &&
            migration.active_state === "applied" &&
            !migration.is_irreversible && (
              <button
                type="button"
                disabled={actionBusy !== null}
                onClick={() => void handleRequestRollbackOrForward("rollback")}
                className="rounded-lg bg-rose-600 px-4 py-2 text-xs font-semibold text-white shadow-sm hover:bg-rose-700 disabled:opacity-50"
              >
                {actionBusy === "rollback" ? "Mengajukan..." : "Ajukan Rollback"}
              </button>
            )}
          {canEdit &&
            (migration.active_state === "rolled_back" ||
              (migration.status === "failed" &&
                (!latestJob || latestJob.status === "failed"))) && (
              <button
                type="button"
                disabled={actionBusy !== null}
                onClick={() => void handleRequestRollbackOrForward("forward")}
                className="rounded-lg bg-cyan-700 px-4 py-2 text-xs font-semibold text-white shadow-sm hover:bg-cyan-800 disabled:opacity-50"
              >
                {actionBusy === "forward"
                  ? "Mengajukan..."
                  : "Ajukan Forward / Retry Attempt"}
              </button>
            )}
        </div>
      </div>

      {actionErr && (
        <div className="rounded-xl border border-red-200 bg-red-50 p-3.5 text-xs text-red-700">
          {actionErr}
        </div>
      )}

      {activeLock && (
        <div className="rounded-xl border border-amber-300 bg-amber-50 p-3.5 text-xs text-amber-900">
          <strong>Lock Database Status:</strong>{" "}
          <code className="font-mono">{activeLock.lock_state}</code> oleh Job #
          {activeLock.job_id} ({activeLock.worker_id})
        </div>
      )}

      <JobProgressMonitor
        dbId={dbId}
        migrationId={migration.id}
        isIrreversible={migration.is_irreversible}
        hasSQLDown={Boolean(migration.sql_down?.trim())}
        canEdit={canEdit}
        initialJob={latestJob}
        onJobUpdated={handleJobUpdated}
      />

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-semibold text-slate-900">
              SQL Up (Forward / Apply)
            </h2>
            <span className="rounded bg-slate-100 px-2 py-0.5 font-mono text-xs text-slate-700">
              {migration.execution_mode}
            </span>
          </div>
          <pre className="mt-3 max-h-72 overflow-auto rounded-lg bg-slate-950 p-4 font-mono text-xs text-emerald-300">
            {migration.sql}
          </pre>
        </div>

        <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-semibold text-slate-900">
              SQL Down / Konfigurasi Rollback
            </h2>
            {migration.is_irreversible ? (
              <span className="rounded bg-amber-100 px-2 py-0.5 text-xs font-semibold text-amber-800">
                Irreversible
              </span>
            ) : (
              <span className="rounded bg-emerald-50 px-2 py-0.5 text-xs font-semibold text-emerald-700">
                Reversible
              </span>
            )}
          </div>

          {migration.is_irreversible ? (
            <div className="mt-3 rounded-lg border border-amber-200 bg-amber-50 p-4 text-xs text-amber-900">
              <div className="font-semibold">Alasan Irreversible:</div>
              <p className="mt-1">{migration.irreversible_reason}</p>
            </div>
          ) : (
            <pre className="mt-3 max-h-72 overflow-auto rounded-lg bg-slate-950 p-4 font-mono text-xs text-amber-300">
              {migration.sql_down || "-- Tidak ada SQL Down --"}
            </pre>
          )}
        </div>
      </div>

      {jobs.length > 0 && (
        <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm">
          <h2 className="text-sm font-semibold text-slate-900">
            Riwayat Job / Attempt Eksekusi Migration Ini
          </h2>
          <div className="mt-3 overflow-x-auto">
            <table className="min-w-full divide-y divide-slate-200 text-left text-xs">
              <thead className="bg-slate-50 font-semibold uppercase text-slate-600">
                <tr>
                  <th className="px-3 py-2">Job ID</th>
                  <th className="px-3 py-2">Action</th>
                  <th className="px-3 py-2">Status</th>
                  <th className="px-3 py-2">Batches</th>
                  <th className="px-3 py-2">Rows</th>
                  <th className="px-3 py-2">Last Cursor</th>
                  <th className="px-3 py-2">Duration</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100">
                {jobs.map((j) => (
                  <tr key={j.id}>
                    <td className="px-3 py-2 font-mono font-semibold">
                      #{j.id}
                    </td>
                    <td className="px-3 py-2 font-mono">{j.action}</td>
                    <td className="px-3 py-2">
                      <span
                        className={`inline-flex rounded-full border px-2 py-0.5 text-[11px] font-semibold uppercase ${statusBadgeClass(
                          j.status
                        )}`}
                      >
                        {j.status}
                      </span>
                    </td>
                    <td className="px-3 py-2 font-mono">
                      {j.committed_batches}
                    </td>
                    <td className="px-3 py-2 font-mono">
                      {j.committed_rows.toLocaleString()}
                    </td>
                    <td className="px-3 py-2 font-mono">
                      {j.last_cursor || "—"}
                    </td>
                    <td className="px-3 py-2 font-mono">
                      {typeof j.duration_ms === "number"
                        ? `${j.duration_ms} ms`
                        : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </main>
  );
}
