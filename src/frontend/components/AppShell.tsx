"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { Button } from "@/components/ui";

interface NavItem {
  href: string;
  label: string;
  show?: (ctx: { isAdmin: boolean; isLeader: boolean }) => boolean;
}

// Tambahkan entri navigasi modul lain (viewer, history, migration) di sini.
const NAV: NavItem[] = [
  { href: "/dashboard", label: "Dashboard" },
  { href: "/approvals", label: "Approval", show: ({ isLeader }) => isLeader },
  { href: "/admin", label: "Administrasi", show: ({ isAdmin }) => isAdmin },
];

export default function AppShell({ children }: { children: React.ReactNode }) {
  const { user, leaderGroups, logout } = useAuth();
  const pathname = usePathname();
  const router = useRouter();
  const [open, setOpen] = useState(false);

  const ctx = { isAdmin: !!user?.is_admin, isLeader: leaderGroups.length > 0 };
  const items = NAV.filter((n) => !n.show || n.show(ctx));

  async function onLogout() {
    await logout();
    router.replace("/login");
  }

  return (
    <div className="flex min-h-screen flex-col bg-slate-50">
      <header className="sticky top-0 z-20 border-b border-slate-200 bg-white/95 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between px-4">
          <Link href="/dashboard" className="text-base font-semibold text-indigo-700">
            DB Platform
          </Link>

          <nav className="hidden items-center gap-1 md:flex" aria-label="Navigasi utama">
            {items.map((n) => (
              <Link
                key={n.href}
                href={n.href}
                aria-current={pathname.startsWith(n.href) ? "page" : undefined}
                className={`rounded-lg px-3 py-2 text-sm font-medium ${
                  pathname.startsWith(n.href) ? "bg-indigo-50 text-indigo-700" : "text-slate-600 hover:bg-slate-100"
                }`}
              >
                {n.label}
              </Link>
            ))}
          </nav>

          <div className="hidden items-center gap-3 md:flex">
            <span className="text-sm text-slate-600">
              {user?.username}
              {user?.is_admin && <span className="ml-2 rounded bg-indigo-100 px-1.5 py-0.5 text-xs text-indigo-700">admin</span>}
            </span>
            <Button variant="secondary" onClick={onLogout}>
              Keluar
            </Button>
          </div>

          <button
            type="button"
            className="inline-flex h-11 w-11 items-center justify-center rounded-lg text-slate-700 hover:bg-slate-100 md:hidden"
            aria-label={open ? "Tutup menu" : "Buka menu"}
            aria-expanded={open}
            aria-controls="mobile-menu"
            onClick={() => setOpen((v) => !v)}
          >
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
              {open ? <path d="M6 6l12 12M18 6L6 18" /> : <path d="M4 7h16M4 12h16M4 17h16" />}
            </svg>
          </button>
        </div>

        {open && (
          <nav id="mobile-menu" className="border-t border-slate-200 bg-white px-4 py-3 md:hidden" aria-label="Navigasi utama">
            <ul className="space-y-1">
              {items.map((n) => (
                <li key={n.href}>
                  <Link
                    href={n.href}
                    onClick={() => setOpen(false)}
                    className={`block min-h-11 rounded-lg px-3 py-3 text-sm font-medium ${
                      pathname.startsWith(n.href) ? "bg-indigo-50 text-indigo-700" : "text-slate-700"
                    }`}
                  >
                    {n.label}
                  </Link>
                </li>
              ))}
            </ul>
            <div className="mt-3 flex items-center justify-between border-t border-slate-100 pt-3">
              <span className="text-sm text-slate-600">{user?.username}</span>
              <Button variant="secondary" onClick={onLogout}>
                Keluar
              </Button>
            </div>
          </nav>
        )}
      </header>

      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6">{children}</main>
    </div>
  );
}
