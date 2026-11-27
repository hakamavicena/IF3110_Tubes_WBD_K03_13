"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  apiRequest,
  type ExecutionAction,
  type MigrationExecution,
} from "@/lib/api";
import {
  ErrorPanel,
  LoadingPanel,
  WorkspaceShell,
} from "@/components/workspace-shell";

interface HistoryResponse {
  history: MigrationExecution[];
}

interface MigrationActionResponse {
  job_id: number;
  status: string;
  migration: {
    id: number;
  };
}

interface MigrationActionTarget {
  migrationId: number;
  title: string;
  version: number;
  action: "rollback" | "forward";
}

const actionLabels: Record<ExecutionAction, string> = {
  apply: "Apply",
  rollback: "Rollback",
  forward: "Forward",
};

export function MigrationHistory({ databaseId }: { databaseId: string }) {
  const [history, setHistory] = useState<MigrationExecution[]>([]);
  const [allExecutions, setAllExecutions] = useState<MigrationExecution[]>([]);
  const [hideRollback, setHideRollback] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [reloadCount, setReloadCount] = useState(0);
  const [pendingMigration, setPendingMigration] = useState<number | null>(null);
  const [notice, setNotice] = useState<{ kind: "success" | "error"; text: string } | null>(null);

  const loadHistory = useCallback(
    async (signal: AbortSignal) => {
      try {
        const basePath = `/api/databases/${encodeURIComponent(databaseId)}/history`;
        const complete = await apiRequest<HistoryResponse>(basePath, { signal });
        const displayed = hideRollback
          ? await apiRequest<HistoryResponse>(
              `${basePath}?hide_rollback=true`,
              { signal },
            )
          : complete;
        if (!signal.aborted) {
          setAllExecutions(
            [...complete.history].sort(
              (left, right) => left.execution_id - right.execution_id,
            ),
          );
          setHistory(
            [...displayed.history].sort(
              (left, right) => left.execution_id - right.execution_id,
            ),
          );
        }
      } catch (loadError) {
        if (!signal.aborted) {
          setError(
            loadError instanceof Error
              ? loadError.message
              : "Unable to load migration history",
          );
        }
      } finally {
        if (!signal.aborted) {
          setLoading(false);
        }
      }
    },
    [databaseId, hideRollback],
  );

  useEffect(() => {
    const controller = new AbortController();
    void Promise.resolve().then(() => {
      if (!controller.signal.aborted) {
        return loadHistory(controller.signal);
      }
    });
    return () => controller.abort();
  }, [loadHistory, reloadCount]);

  const actionTargets = useMemo(() => {
    const latestByMigration = new Map<number, MigrationExecution>();
    for (const execution of allExecutions) {
      if (execution.status === "success") {
        const current = latestByMigration.get(execution.migration_id);
        if (!current || execution.execution_id > current.execution_id) {
          latestByMigration.set(execution.migration_id, execution);
        }
      }
    }

    return [...latestByMigration.values()]
      .sort((left, right) => right.execution_id - left.execution_id)
      .map<MigrationActionTarget>((execution) => ({
        migrationId: execution.migration_id,
        title: execution.title,
        version: execution.version,
        action: execution.action === "rollback" ? "forward" : "rollback",
      }));
  }, [allExecutions]);

  async function runAction(target: MigrationActionTarget) {
    const actionName = target.action === "rollback" ? "roll back" : "move forward";
    const confirmed = window.confirm(
      `Ajukan ${actionName} untuk "${target.title}" (versi ${target.version})? Pengajuan ini harus di-approve leader sebelum dijalankan.`,
    );
    if (!confirmed) {
      return;
    }

    setPendingMigration(target.migrationId);
    setNotice(null);
    try {
      const result = await apiRequest<MigrationActionResponse>(
        `/api/databases/${encodeURIComponent(databaseId)}/migrations/${target.migrationId}/${target.action}`,
        { method: "POST" },
      );
      try {
        await apiRequest<Record<string, unknown>>(
          `/api/databases/${encodeURIComponent(databaseId)}/jobs/${result.job_id}/submit-review`,
          { method: "POST" },
        );
      } catch (reviewError) {
        setNotice({
          kind: "error",
          text: `Job #${result.job_id} dibuat sebagai draft, tetapi gagal diajukan untuk review: ${
            reviewError instanceof Error
              ? reviewError.message
              : "kesalahan tidak diketahui"
          }. Buka detail migration untuk mengajukannya secara manual.`,
        });
        setLoading(true);
        setError(null);
        setReloadCount((count) => count + 1);
        return;
      }
      setNotice({
        kind: "success",
        text: `Pengajuan ${target.action} dibuat dan dikirim untuk review (job #${result.job_id}). Setelah semua group leader menyetujui, enqueue job dari halaman Migrations.`,
      });
      setLoading(true);
      setError(null);
      setReloadCount((count) => count + 1);
    } catch (actionError) {
      setNotice({
        kind: "error",
        text:
          actionError instanceof Error
            ? actionError.message
            : `${actionLabels[target.action]} failed`,
      });
    } finally {
      setPendingMigration(null);
    }
  }

  return (
    <WorkspaceShell
      databaseId={databaseId}
      description="Review the append-only execution history, inspect each SQL snapshot, and manage the latest migration state."
      eyebrow="Database operations"
      section="history"
      title="Migration history"
    >
      <section className="rounded-2xl border border-slate-200 bg-white shadow-sm">
        <div className="flex flex-col gap-4 border-b border-slate-100 p-4 sm:flex-row sm:items-center sm:justify-between sm:p-6">
          <div>
            <p className="text-xs font-bold uppercase tracking-wider text-slate-500">
              Execution timeline
            </p>
            <h2 className="mt-1 text-lg font-bold">History entries</h2>
          </div>
          <label className="inline-flex cursor-pointer items-center gap-3 self-start rounded-xl bg-slate-50 px-3 py-2.5 text-sm font-semibold text-slate-700">
            <input
              checked={hideRollback}
              className="size-4 accent-cyan-600"
              onChange={(event) => {
                setLoading(true);
                setError(null);
                setHideRollback(event.target.checked);
              }}
              type="checkbox"
            />
            Hide Rollback
          </label>
        </div>

        <div className="p-4 sm:p-6">
          {notice && (
            <div
              aria-live="polite"
              className={`mb-5 rounded-xl border p-4 text-sm ${
                notice.kind === "success"
                  ? "border-emerald-200 bg-emerald-50 text-emerald-900"
                  : "border-rose-200 bg-rose-50 text-rose-900"
              }`}
              role={notice.kind === "error" ? "alert" : "status"}
            >
              {notice.text}
            </div>
          )}

          {loading ? (
            <LoadingPanel label="Loading migration history…" />
          ) : error ? (
            <ErrorPanel
              message={error}
              onRetry={() => {
                setLoading(true);
                setError(null);
                setReloadCount((count) => count + 1);
              }}
            />
          ) : history.length === 0 ? (
            <div className="rounded-xl border border-dashed border-slate-300 bg-slate-50 px-5 py-10 text-center">
              <p className="font-semibold text-slate-700">No execution history</p>
              <p className="mt-1 text-sm text-slate-500">
                {hideRollback
                  ? "No entries remain after hiding successful rollback pairs."
                  : "Migration executions for this database will appear here."}
              </p>
            </div>
          ) : (
            <ol className="space-y-4">
              {history.map((execution) => (
                <HistoryEntry execution={execution} key={execution.execution_id} />
              ))}
            </ol>
          )}
        </div>
      </section>

      <section className="mt-6 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
        <div className="mb-5">
          <p className="text-xs font-bold uppercase tracking-wider text-slate-500">
            Current migration state
          </p>
          <h2 className="mt-1 text-lg font-bold">Available actions</h2>
          <p className="mt-1 text-sm leading-6 text-slate-500">
            Actions run synchronously. Access restrictions and migration state are
            validated by the server.
          </p>
        </div>
        {loading ? (
          <p className="text-sm text-slate-500">Checking available actions…</p>
        ) : error ? (
          <p className="text-sm text-slate-500">
            Actions are unavailable until history loads successfully.
          </p>
        ) : actionTargets.length === 0 ? (
          <p className="rounded-xl bg-slate-50 p-4 text-sm text-slate-500">
            No successful migration execution is available to roll back or move
            forward.
          </p>
        ) : (
          <div className="grid gap-3 lg:grid-cols-2">
            {actionTargets.map((target) => (
              <div
                className="flex flex-col justify-between gap-4 rounded-xl border border-slate-200 p-4 sm:flex-row sm:items-center"
                key={target.migrationId}
              >
                <div className="min-w-0">
                  <p className="truncate text-sm font-bold">{target.title}</p>
                  <p className="mt-1 text-xs text-slate-500">
                    Version {target.version} · migration #{target.migrationId}
                  </p>
                </div>
                <button
                  className={`shrink-0 rounded-lg px-4 py-2.5 text-sm font-bold text-white transition disabled:cursor-not-allowed disabled:opacity-50 ${
                    target.action === "rollback"
                      ? "bg-rose-600 hover:bg-rose-700"
                      : "bg-cyan-700 hover:bg-cyan-800"
                  }`}
                  disabled={pendingMigration !== null}
                  onClick={() => void runAction(target)}
                  type="button"
                >
                  {pendingMigration === target.migrationId
                    ? "Working…"
                    : target.action === "rollback"
                      ? "Rollback"
                      : "Forward"}
                </button>
              </div>
            ))}
          </div>
        )}
      </section>
    </WorkspaceShell>
  );
}

function HistoryEntry({ execution }: { execution: MigrationExecution }) {
  const succeeded = execution.status === "success";

  return (
    <li
      className={`overflow-hidden rounded-xl border ${
        succeeded ? "border-slate-200" : "border-rose-200"
      }`}
    >
      <div className="flex flex-col gap-3 bg-white p-4 sm:flex-row sm:items-start sm:justify-between sm:p-5">
        <div className="flex min-w-0 gap-3">
          <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-slate-900 text-xs font-black text-white">
            E{execution.execution_id}
          </span>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="break-words text-sm font-bold">{execution.title}</h3>
              <span className="rounded-full bg-slate-100 px-2 py-1 text-[10px] font-bold uppercase tracking-wide text-slate-600">
                {actionLabels[execution.action]}
              </span>
              <span
                className={`rounded-full px-2 py-1 text-[10px] font-bold uppercase tracking-wide ${
                  succeeded
                    ? "bg-emerald-100 text-emerald-800"
                    : "bg-rose-100 text-rose-800"
                }`}
              >
                {execution.status}
              </span>
            </div>
            <p className="mt-1 text-xs leading-5 text-slate-500">
              Version {execution.version}, revision {execution.revision}
              {" · "}by {execution.executed_by_name || `user ${execution.executed_by}`}
            </p>
            <p className="mt-1 text-xs text-slate-500">
              {new Intl.DateTimeFormat(undefined, {
                dateStyle: "medium",
                timeStyle: "medium",
              }).format(new Date(execution.executed_at))}
            </p>
          </div>
        </div>
        <div className="grid grid-cols-2 gap-x-5 gap-y-2 pl-14 text-xs sm:pl-0 sm:text-right">
          <span className="text-slate-500">Duration</span>
          <span className="font-semibold text-slate-700">{execution.duration_ms} ms</span>
          <span className="text-slate-500">Rows affected</span>
          <span className="font-semibold text-slate-700">
            {execution.rows_affected.toLocaleString()}
          </span>
          {execution.job_id !== null && (
            <>
              <span className="text-slate-500">Job</span>
              <span className="font-semibold text-slate-700">#{execution.job_id}</span>
            </>
          )}
        </div>
      </div>

      <details className="border-t border-slate-100 bg-slate-50">
        <summary className="cursor-pointer px-4 py-3 text-sm font-semibold text-cyan-800 hover:bg-cyan-50 sm:px-5">
          View SQL snapshot
        </summary>
        <pre className="max-h-96 overflow-auto border-t border-slate-200 bg-slate-950 p-4 text-xs leading-5 text-cyan-50">
          <code>{execution.sql_executed || "(No SQL snapshot recorded)"}</code>
        </pre>
        {execution.reverted_execution_id !== null && (
          <p className="px-4 py-3 text-xs text-slate-500 sm:px-5">
            References execution E{execution.reverted_execution_id}.
          </p>
        )}
      </details>
    </li>
  );
}
