import Link from "next/link";
import type { ReactNode } from "react";

interface WorkspaceShellProps {
  children: ReactNode;
  databaseId?: string;
  section: "dashboard" | "viewer" | "history" | "migrations";
  eyebrow: string;
  title: string;
  description: string;
}

export function WorkspaceShell({
  children,
  databaseId,
  section,
  eyebrow,
  title,
  description,
}: WorkspaceShellProps) {
  const navClass = (active: boolean) =>
    `inline-flex min-h-11 items-center rounded-xl px-3 py-2 text-sm font-semibold transition ${
      active
        ? "bg-slate-900 text-white"
        : "text-slate-600 hover:bg-slate-100 hover:text-slate-950"
    }`;
  const base = databaseId ? `/databases/${encodeURIComponent(databaseId)}` : "";

  // Header, navigasi global, dan guard login disediakan oleh AppShell di app/(app)/layout.tsx.
  return (
    <div className="text-slate-900">
      {databaseId && (
        <nav aria-label="Navigasi database" className="mb-6 flex gap-2 overflow-x-auto pb-1">
          <Link className={navClass(section === "dashboard")} href="/dashboard">
            Databases
          </Link>
          <Link className={navClass(section === "viewer")} href={`${base}/viewer`}>
            Explorer
          </Link>
          <Link className={navClass(section === "migrations")} href={`${base}/migrations`}>
            Migrations
          </Link>
          <Link className={navClass(section === "history")} href={`${base}/history`}>
            Migration history
          </Link>
        </nav>
      )}
      <div className="mb-7 max-w-3xl">
        <p className="mb-2 text-xs font-bold uppercase tracking-[0.18em] text-cyan-700">{eyebrow}</p>
        <h1 className="text-3xl font-bold tracking-tight text-slate-950 sm:text-4xl">{title}</h1>
        <p className="mt-3 text-sm leading-6 text-slate-600 sm:text-base">{description}</p>
      </div>
      {children}
    </div>
  );
}

export function LoadingPanel({ label = "Loading data…" }: { label?: string }) {
  return (
    <div
      aria-live="polite"
      className="rounded-2xl border border-slate-200 bg-white p-8 text-sm text-slate-500 shadow-sm"
    >
      <span className="mr-3 inline-block size-2 animate-pulse rounded-full bg-cyan-500" />
      {label}
    </div>
  );
}

export function ErrorPanel({
  message,
  onRetry,
}: {
  message: string;
  onRetry?: () => void;
}) {
  return (
    <div
      aria-live="assertive"
      className="rounded-2xl border border-rose-200 bg-rose-50 p-5 text-sm text-rose-800"
      role="alert"
    >
      <p className="font-bold">Unable to load this view</p>
      <p className="mt-1 break-words">{message}</p>
      {onRetry && (
        <button
          className="mt-4 rounded-lg border border-rose-300 bg-white px-3 py-2 font-semibold hover:bg-rose-100"
          onClick={onRetry}
          type="button"
        >
          Try again
        </button>
      )}
    </div>
  );
}
