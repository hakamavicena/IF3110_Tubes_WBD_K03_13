"use client";

import React from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import MigrationEditorForm from "../components/MigrationEditorForm";

export default function NewMigrationPage() {
  const params = useParams<{ dbId: string }>();
  const dbId = params?.dbId || "";

  return (
    <main className="mx-auto max-w-4xl px-4 py-6 sm:px-6 lg:px-8">
      <div className="mb-5">
        <Link
          href={`/databases/${dbId}/migrations`}
          className="text-xs font-semibold text-blue-600 hover:underline"
        >
          &larr; Kembali ke Daftar Migration
        </Link>
        <h1 className="mt-1 text-xl font-bold text-slate-900 sm:text-2xl">
          Buat Migration SQL Baru (Database #{dbId})
        </h1>
        <p className="mt-0.5 text-xs text-slate-500">
          Tentukan script SQL Up, SQL Down / status Irreversible, serta mode
          eksekusi (Biasa atau Per Batch dengan Checkpoint).
        </p>
      </div>

      <MigrationEditorForm dbId={dbId} mode="create" />
    </main>
  );
}
