"use client";

import React, { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import {
  DatabaseLockInfo,
  fetchMigrations,
  MigrationItem,
} from "./api";
import { statusBadgeClass } from "./components/JobProgressMonitor";

export default function MigrationListPage() {
  const params = useParams<{ dbId: string }>();
  const dbId = params?.dbId || "";

  const [migrations, setMigrations] = useState<MigrationItem[]>([]);
  const [canEdit, setCanEdit] = useState(false);
  const [activeLock, setActiveLock] = useState<DatabaseLockInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const loadData = useCallback(async () => {
    if (!dbId) return;
    setLoading(true);
    setError(null);
    try {
      const res = await fetchMigrations(dbId);
      setMigrations(res.migrations || []);
      setCanEdit(Boolean(res.can_edit));
      setActiveLock(res.active_lock || null);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Gagal memuat daftar migration"
      );
    } finally {
      setLoading(false);
    }
  }, [dbId]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- fetch saat mount; state baru diset setelah await
    void loadData();
  }, [loadData]);

  return (
    <main className="mx-auto max-w-6xl px-4 py-6 sm:px-6 lg:px-8">
      {dbId && (
        <nav
          aria-label="Navigasi database"
          className="mb-6 flex gap-2 overflow-x-auto pb-1"
        >
          <Link
            href="/dashboard"
            className="inline-flex min-h-11 items-center rounded-xl px-3 py-2 text-sm font-semibold text-slate-600 transition hover:bg-slate-100 hover:text-slate-950"
          >
            Databases
          </Link>
          <Link
            href={`/databases/${encodeURIComponent(dbId)}/viewer`}
            className="inline-flex min-h-11 items-center rounded-xl px-3 py-2 text-sm font-semibold text-slate-600 transition hover:bg-slate-100 hover:text-slate-950"
          >
            Explorer
          </Link>
          <Link
            href={`/databases/${encodeURIComponent(dbId)}/migrations`}
            className="inline-flex min-h-11 items-center rounded-xl bg-slate-900 px-3 py-2 text-sm font-semibold text-white transition"
          >
            Migrations
          </Link>
          <Link
            href={`/databases/${encodeURIComponent(dbId)}/history`}
            className="inline-flex min-h-11 items-center rounded-xl px-3 py-2 text-sm font-semibold text-slate-600 transition hover:bg-slate-100 hover:text-slate-950"
          >
            Migration history
          </Link>
        </nav>
      )}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="flex items-center gap-2 text-xs text-slate-500">
            <Link href="/dashboard" className="hover:text-blue-600">
              Dashboard
            </Link>
            <span>/</span>
            <span>Database #{dbId}</span>
            <span>/</span>
            <span className="font-semibold text-slate-700">Migrations</span>
          </div>
          <h1 className="mt-1 text-xl font-bold text-slate-900 sm:text-2xl">
            Daftar Migration SQL (Database #{dbId})
          </h1>
          <p className="mt-0.5 text-xs text-slate-500">
            Kelola definisi SQL Up/Down, eksekusi per batch dengan checkpoint,
            dan pantau antrean job secara asynchronous.
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Link
            href={`/databases/${dbId}/history`}
            className="rounded-lg border border-slate-300 bg-white px-3.5 py-2 text-xs font-semibold text-slate-700 hover:bg-slate-50"
          >
            Lihat Migration History
          </Link>
          {canEdit && (
            <Link
              href={`/databases/${dbId}/migrations/new`}
              className="rounded-lg bg-blue-600 px-4 py-2 text-xs font-semibold text-white shadow-sm hover:bg-blue-700"
            >
              + Buat Migration Baru
            </Link>
          )}
        </div>
      </div>

      {activeLock && (
        <div className="mt-4 rounded-xl border border-amber-300 bg-amber-50 p-4 text-xs text-amber-900">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <span className="font-bold uppercase">
                Database Lock Aktif ({activeLock.lock_state})
              </span>{" "}
              &mdash; Sedang dipegang oleh <strong>Job #{activeLock.job_id}</strong>{" "}
              (Worker: <code className="font-mono">{activeLock.worker_id}</code>)
            </div>
            <span className="font-mono text-[11px] text-amber-700">
              Heartbeat:{" "}
              {new Date(activeLock.heartbeat_at).toLocaleTimeString()}
            </span>
          </div>
        </div>
      )}

      {error && (
        <div className="mt-4 rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          {error}
        </div>
      )}

      {loading ? (
        <div className="mt-6 rounded-xl border border-slate-200 bg-white p-8 text-center text-sm text-slate-500">
          Memuat daftar migration...
        </div>
      ) : migrations.length === 0 ? (
        <div className="mt-6 rounded-xl border border-slate-200 bg-white p-10 text-center">
          <p className="text-sm font-medium text-slate-700">
            Belum ada migration pada database target ini.
          </p>
          {canEdit && (
            <Link
              href={`/databases/${dbId}/migrations/new`}
              className="mt-4 inline-block rounded-lg bg-blue-600 px-4 py-2 text-xs font-semibold text-white hover:bg-blue-700"
            >
              Buat Migration Pertama
            </Link>
          )}
        </div>
      ) : (
        <>
          <div className="mt-6 hidden overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm md:block">
            <table className="min-w-full divide-y divide-slate-200 text-left text-sm">
              <thead className="bg-slate-50 text-xs font-semibold uppercase text-slate-600">
                <tr>
                  <th className="px-4 py-3">Versi</th>
                  <th className="px-4 py-3">Judul</th>
                  <th className="px-4 py-3">Mode</th>
                  <th className="px-4 py-3">Rollback</th>
                  <th className="px-4 py-3">Active State</th>
                  <th className="px-4 py-3">Job Status</th>
                  <th className="px-4 py-3 text-right">Aksi</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100">
                {migrations.map((m) => (
                  <tr key={m.id} className="hover:bg-slate-50/80">
                    <td className="px-4 py-3 font-mono text-xs font-bold text-slate-900">
                      v{m.version}
                      <span className="ml-1 font-normal text-slate-400">
                        (r{m.revision})
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <div className="font-medium text-slate-900">{m.title}</div>
                      <div className="text-xs text-slate-500">
                        Oleh {m.created_by_name}
                      </div>
                    </td>
                    <td className="px-4 py-3 text-xs">
                      <span className="rounded bg-slate-100 px-2 py-0.5 font-mono font-medium text-slate-700">
                        {m.execution_mode === "batch"
                          ? `batch (${m.batch_column}, ${m.batch_size})`
                          : "standard"}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-xs">
                      {m.is_irreversible ? (
                        <span className="rounded bg-amber-100 px-2 py-0.5 font-semibold text-amber-800">
                          Irreversible
                        </span>
                      ) : (
                        <span className="rounded bg-emerald-50 px-2 py-0.5 font-medium text-emerald-700">
                          Reversible
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-slate-700">
                      {m.active_state}
                    </td>
                    <td className="px-4 py-3">
                      <span
                        className={`inline-flex rounded-full border px-2.5 py-0.5 text-xs font-semibold uppercase ${statusBadgeClass(
                          m.status
                        )}`}
                      >
                        {m.status}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-right">
                      <Link
                        href={`/databases/${dbId}/migrations/${m.id}`}
                        className="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-100"
                      >
                        Detail &amp; Monitor
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="mt-4 space-y-3 md:hidden">
            {migrations.map((m) => (
              <div
                key={m.id}
                className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm"
              >
                <div className="flex items-center justify-between">
                  <span className="font-mono text-xs font-bold text-slate-900">
                    v{m.version} (rev {m.revision})
                  </span>
                  <span
                    className={`rounded-full border px-2.5 py-0.5 text-[11px] font-semibold uppercase ${statusBadgeClass(
                      m.status
                    )}`}
                  >
                    {m.status}
                  </span>
                </div>
                <h2 className="mt-1.5 text-sm font-semibold text-slate-900">
                  {m.title}
                </h2>
                <div className="mt-2 flex flex-wrap gap-2 text-xs text-slate-600">
                  <span className="rounded bg-slate-100 px-2 py-0.5 font-mono">
                    {m.execution_mode}
                  </span>
                  <span className="rounded bg-slate-100 px-2 py-0.5 font-mono">
                    state: {m.active_state}
                  </span>
                  {m.is_irreversible && (
                    <span className="rounded bg-amber-100 px-2 py-0.5 text-amber-800">
                      Irreversible
                    </span>
                  )}
                </div>
                <div className="mt-3 flex justify-end">
                  <Link
                    href={`/databases/${dbId}/migrations/${m.id}`}
                    className="rounded-lg bg-slate-900 px-3.5 py-1.5 text-xs font-semibold text-white"
                  >
                    Buka Detail &amp; Monitor
                  </Link>
                </div>
              </div>
            ))}
          </div>
        </>
      )}
    </main>
  );
}
