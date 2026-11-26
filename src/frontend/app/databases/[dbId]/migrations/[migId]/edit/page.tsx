"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { fetchMigrationDetail, MigrationItem } from "../../api";
import MigrationEditorForm from "../../components/MigrationEditorForm";

export default function EditMigrationPage() {
  const params = useParams<{ dbId: string; migId: string }>();
  const dbId = params?.dbId || "";
  const migId = params?.migId || "";

  const [migration, setMigration] = useState<MigrationItem | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!dbId || !migId) return;
    fetchMigrationDetail(dbId, migId)
      .then((res) => setMigration(res.migration))
      .catch((err) =>
        setError(
          err instanceof Error ? err.message : "Gagal memuat data migration"
        )
      )
      .finally(() => setLoading(false));
  }, [dbId, migId]);

  return (
    <main className="mx-auto max-w-4xl px-4 py-6 sm:px-6 lg:px-8">
      <div className="mb-5">
        <Link
          href={`/databases/${dbId}/migrations/${migId}`}
          className="text-xs font-semibold text-blue-600 hover:underline"
        >
          &larr; Kembali ke Detail Migration
        </Link>
        <h1 className="mt-1 text-xl font-bold text-slate-900 sm:text-2xl">
          Edit Draft Migration {migration ? `v${migration.version}` : ""}
        </h1>
      </div>

      {loading ? (
        <div className="rounded-xl border border-slate-200 bg-white p-8 text-center text-sm text-slate-500">
          Memuat form editor...
        </div>
      ) : error || !migration ? (
        <div className="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          {error || "Migration tidak ditemukan"}
        </div>
      ) : (
        <MigrationEditorForm
          dbId={dbId}
          initialMigration={migration}
          mode="edit"
        />
      )}
    </main>
  );
}
