import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Data Platform",
  description: "Explore PostgreSQL databases and migration history.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className="h-full antialiased">
      <body className="min-h-full bg-[#f4f6fb] font-sans text-slate-900">
        {children}
      </body>
    </html>
  );
}
