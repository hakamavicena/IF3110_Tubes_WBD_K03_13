<<<<<<< HEAD
import type { Metadata, Viewport } from "next";
import "./globals.css";
import { AuthProvider } from "@/components/AuthProvider";

export const metadata: Metadata = {
  title: "DB Management Platform",
  description: "Platform manajemen database: akses, migration, approval, dan eksekusi asynchronous.",
=======
import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Data Platform",
  description: "Explore PostgreSQL databases and migration history.",
>>>>>>> origin/feat/Rollback-Forward-Migration
};

export const viewport: Viewport = { width: "device-width", initialScale: 1 };

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
<<<<<<< HEAD
    <html lang="id" className="h-full antialiased">
      <body className="min-h-full">
        <AuthProvider>{children}</AuthProvider>
=======
    <html lang="en" className="h-full antialiased">
      <body className="min-h-full bg-[#f4f6fb] font-sans text-slate-900">
        {children}
>>>>>>> origin/feat/Rollback-Forward-Migration
      </body>
    </html>
  );
}
