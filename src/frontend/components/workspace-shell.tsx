import Link from "next/link";
import type { ReactNode } from "react";

interface WorkspaceShellProps {
  children: ReactNode;
  databaseId?: string;
  section: "dashboard" | "viewer" | "history";
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
    `rounded-xl px-3 py-2 text-sm font-semibold transition ${
      active
        ? "bg-slate-900 text-white"
        : "text-slate-600 hover:bg-slate-100 hover:text-slate-950"
    }`;

  return (
    <div className="min-h-screen bg-[#f4f6fb] text-slate-900">
      <header className="border-b border-slate-200/80 bg-white">
        <div className="mx-auto flex max-w-7xl flex-col gap-3 px-4 py-4 sm:px-6 lg:flex-row lg:items-center lg:justify-between lg:px-8">
          <Link className="flex items-center gap-3 self-start" href="/dashboard">
            <span className="grid size-10 place-items-center rounded-2xl bg-cyan-500 text-lg font-black text-slate-950 shadow-sm shadow-cyan-200">
              D
            </span>
            <span>
              <span className="block text-sm font-extrabold tracking-tight">
                Data Platform
              </span>
              <span className="block text-xs text-slate-500">
                Database workspace
              </span>
            </span>
          </Link>

          <nav
            aria-label="Primary navigation"
            className="flex gap-2 overflow-x-auto pb-1 lg:pb-0"
          >
            <Link className={navClass(section === "dashboard")} href="/dashboard">
              Databases
            </Link>
            {databaseId && (
              <>
                <Link
                  className={navClass(section === "viewer")}
                  href={`/databases/${encodeURIComponent(databaseId)}/viewer`}
                >
                  Explorer
                </Link>
                <Link
                  className={navClass(section === "history")}
                  href={`/databases/${encodeURIComponent(databaseId)}/history`}
                >
                  Migration history
                </Link>
              </>
            )}
          </nav>
          <div className="hidden items-center gap-2 text-xs font-medium text-slate-500 lg:flex">
            <span className="size-2 rounded-full bg-emerald-500" />
            Workspace
          </div>
        </div>
      </header>

      <main className="mx-auto w-full max-w-7xl px-4 py-7 sm:px-6 sm:py-10 lg:px-8">
        <div className="mb-7 max-w-3xl">
          <p className="mb-2 text-xs font-bold uppercase tracking-[0.18em] text-cyan-700">
            {eyebrow}
          </p>
          <h1 className="text-3xl font-bold tracking-tight text-slate-950 sm:text-4xl">
            {title}
          </h1>
          <p className="mt-3 text-sm leading-6 text-slate-600 sm:text-base">
            {description}
          </p>
        </div>
        {children}
      </main>
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
