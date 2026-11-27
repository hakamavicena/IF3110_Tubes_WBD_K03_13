"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  apiRequest,
  type DatabaseAccess,
} from "@/lib/api";
import { useAuth } from "@/components/AuthProvider";
import { ErrorPanel, LoadingPanel, WorkspaceShell } from "@/components/workspace-shell";

interface DatabaseListResponse {
  databases: DatabaseAccess[];
}

export default function DashboardPage() {
  const { user, leaderGroups } = useAuth();
  const [databases, setDatabases] = useState<DatabaseAccess[]>([]);
  const [filter, setFilter] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [retry, setRetry] = useState(0);

  const loadDatabases = useCallback(async (signal: AbortSignal) => {
    try {
      const response = await apiRequest<DatabaseListResponse>("/api/databases", {
        signal,
      });
      setDatabases(response.databases);
    } catch (loadError) {
      if (!signal.aborted) {
        setError(
          loadError instanceof Error
            ? loadError.message
            : "Unable to load databases",
        );
      }
    } finally {
      if (!signal.aborted) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void Promise.resolve().then(() => {
      if (!controller.signal.aborted) {
        return loadDatabases(controller.signal);
      }
    });
    return () => controller.abort();
  }, [loadDatabases, retry]);

  const visibleDatabases = useMemo(() => {
    const normalized = filter.trim().toLocaleLowerCase();
    if (!normalized) {
      return databases;
    }
    return databases.filter(
      (database) =>
        database.name.toLocaleLowerCase().includes(normalized) ||
        String(database.id).includes(normalized),
    );
  }, [databases, filter]);

  return (
    <WorkspaceShell
      description="Choose a database you can access, then explore its schemas, tables, and migration history."
      eyebrow="Your workspace"
      section="dashboard"
      title="Databases"
    >
      <section className="mb-6 flex flex-col gap-2 rounded-2xl border border-slate-200 bg-white p-5 text-sm shadow-sm sm:flex-row sm:items-center sm:justify-between">
        <p>
          Masuk sebagai <span className="font-semibold">{user?.username}</span>
          {" · "}
          {user?.is_admin ? "Platform Admin" : "Pengguna"}
          {leaderGroups.length > 0 && <> · Leader: {leaderGroups.map((g) => g.name).join(", ")}</>}
        </p>
        {leaderGroups.length > 0 && (
          <Link href="/approvals" className="inline-flex min-h-11 items-center font-medium text-indigo-700 hover:underline">
            Buka inbox approval →
          </Link>
        )}
      </section>

      <section className="mb-6 grid gap-4 sm:grid-cols-3">
        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <p className="text-xs font-bold uppercase tracking-wider text-slate-500">
            Accessible databases
          </p>
          <p className="mt-3 text-3xl font-bold">{databases.length}</p>
        </div>
        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <p className="text-xs font-bold uppercase tracking-wider text-slate-500">
            Edit access
          </p>
          <p className="mt-3 text-3xl font-bold">
            {databases.filter((database) => database.access === "edit").length}
          </p>
        </div>
        <div className="flex items-center rounded-2xl bg-slate-900 p-5 text-white shadow-sm">
          <div>
            <p className="text-xs font-bold uppercase tracking-wider text-cyan-300">
              Explore
            </p>
            <p className="mt-2 text-sm leading-5 text-slate-200">
              Browse database structure and inspect paginated table data.
            </p>
          </div>
        </div>
      </section>

      <section className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
        <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <h2 className="text-lg font-bold">Available targets</h2>
            <p className="mt-1 text-sm text-slate-500">
              Select a target to open its schema explorer.
            </p>
          </div>
          <label className="w-full sm:max-w-xs">
            <span className="sr-only">Search databases</span>
            <input
              className="w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-2.5 text-sm outline-none transition placeholder:text-slate-400 focus:border-cyan-500 focus:bg-white focus:ring-4 focus:ring-cyan-100"
              onChange={(event) => setFilter(event.target.value)}
              placeholder="Search name or ID"
              type="search"
              value={filter}
            />
          </label>
        </div>

        {loading ? (
          <LoadingPanel label="Loading accessible databases…" />
        ) : error ? (
          <div className="space-y-4">
            <ErrorPanel
              message={error}
              onRetry={() => {
                setLoading(true);
                setError(null);
                setRetry((value) => value + 1);
              }}
            />
            {error.includes("401") && (
              <Link
                className="inline-flex rounded-xl bg-slate-900 px-4 py-2.5 text-sm font-bold text-white hover:bg-slate-700"
                href="/login"
              >
                Go to sign in
              </Link>
            )}
          </div>
        ) : visibleDatabases.length === 0 ? (
          <div className="rounded-xl border border-dashed border-slate-300 bg-slate-50 px-5 py-12 text-center">
            <p className="font-semibold text-slate-800">
              {databases.length === 0
                ? "No database targets are available"
                : "No databases match your search"}
            </p>
            <p className="mt-2 text-sm text-slate-500">
              {databases.length === 0
                ? "Ask an administrator to configure a target and grant your group access."
                : "Try another name or database ID."}
            </p>
          </div>
        ) : (
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {visibleDatabases.map((database) => (
              <Link
                className="group rounded-2xl border border-slate-200 p-4 transition hover:-translate-y-0.5 hover:border-cyan-300 hover:bg-cyan-50/40 hover:shadow-md focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-600"
                href={`/databases/${encodeURIComponent(database.id)}/viewer`}
                key={database.id}
              >
                <div className="flex items-start justify-between gap-3">
                  <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-cyan-100 text-sm font-black text-cyan-900">
                    DB
                  </span>
                  <span
                    className={`rounded-full px-2.5 py-1 text-xs font-bold ${
                      database.access === "edit"
                        ? "bg-emerald-100 text-emerald-800"
                        : "bg-slate-100 text-slate-600"
                    }`}
                  >
                    {database.access === "edit" ? "View + edit" : "View only"}
                  </span>
                </div>
                <h3 className="mt-4 truncate text-base font-bold group-hover:text-cyan-900">
                  {database.name}
                </h3>
                <div className="mt-2 flex items-center justify-between text-xs text-slate-500">
                  <span>Target ID {database.id}</span>
                  <span aria-hidden="true" className="text-base text-cyan-700">
                    →
                  </span>
                </div>
              </Link>
            ))}
          </div>
        )}
      </section>
    </WorkspaceShell>
  );
}
