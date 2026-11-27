"use client";

import React, { useState } from "react";
import { useRouter } from "next/navigation";
import {
  createMigration,
  ExecutionMode,
  MigrationItem,
  MigrationUpsertPayload,
  submitJobForReview,
  updateMigration,
} from "../api";

interface MigrationEditorFormProps {
  dbId: string;
  initialMigration?: MigrationItem;
  mode: "create" | "edit";
}

export default function MigrationEditorForm({
  dbId,
  initialMigration,
  mode,
}: MigrationEditorFormProps) {
  const router = useRouter();

  const [title, setTitle] = useState(initialMigration?.title || "");
  const [sql, setSql] = useState(initialMigration?.sql || "");
  const [sqlDown, setSqlDown] = useState(initialMigration?.sql_down || "");
  const [isIrreversible, setIsIrreversible] = useState(
    initialMigration?.is_irreversible || false
  );
  const [irreversibleReason, setIrreversibleReason] = useState(
    initialMigration?.irreversible_reason || ""
  );
  const [executionMode, setExecutionMode] = useState<ExecutionMode>(
    initialMigration?.execution_mode || "standard"
  );
  const [batchColumn, setBatchColumn] = useState(
    initialMigration?.batch_column || "id"
  );
  const [batchSize, setBatchSize] = useState<number>(
    initialMigration?.batch_size || 500
  );

  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (mode === "edit" && initialMigration?.is_immutable) {
    return (
      <div className="rounded-xl border border-amber-300 bg-amber-50 p-6 text-sm text-amber-900">
        <h2 className="text-base font-bold">
          Migration Bersifat Immutable (Tidak Dapat Diedit)
        </h2>
        <p className="mt-1">
          Migration <strong>v{initialMigration.version}</strong> sudah pernah
          dieksekusi sehingga definisi SQL Up dan SQL Down dikunci secara
          permanen sesuai spesifikasi Milestone 2.
        </p>
        <button
          type="button"
          onClick={() =>
            router.push(`/databases/${dbId}/migrations/${initialMigration.id}`)
          }
          className="mt-4 rounded-lg bg-amber-800 px-4 py-2 text-xs font-semibold text-white hover:bg-amber-900"
        >
          Kembali ke Detail Migration
        </button>
      </div>
    );
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    if (!title.trim()) {
      setError("Judul migration wajib diisi.");
      return;
    }
    if (!sql.trim()) {
      setError("SQL Up wajib diisi.");
      return;
    }

    const missingDownSQL = !isIrreversible && !sqlDown.trim();

    if (isIrreversible && !irreversibleReason.trim()) {
      setError(
        "Alasan irreversible wajib diisi jika migration ditandai Irreversible."
      );
      return;
    }
    if (missingDownSQL) {
      setError(
        "SQL Down (Rollback) wajib diisi untuk migration yang bukan Irreversible."
      );
      return;
    }

    if (executionMode === "batch") {
      if (!batchColumn.trim()) {
        setError("Batch Column (cursor) wajib diisi pada mode Per Batch.");
        return;
      }
      if (!batchSize || batchSize <= 0) {
        setError("Batch Size harus lebih besar dari 0.");
        return;
      }
    }

    const payload: MigrationUpsertPayload = {
      title: title.trim(),
      sql,
      sql_down: isIrreversible ? "" : sqlDown,
      is_irreversible: isIrreversible,
      irreversible_reason: isIrreversible ? irreversibleReason.trim() : "",
      execution_mode: executionMode,
      batch_column: executionMode === "batch" ? batchColumn.trim() : "",
      batch_size: executionMode === "batch" ? Number(batchSize) : 0,
    };

    setSubmitting(true);
    try {
      if (mode === "create") {
        const res = await createMigration(dbId, payload);
        router.push(`/databases/${dbId}/migrations/${res.migration.id}`);
      } else if (initialMigration) {
        const res = await updateMigration(dbId, initialMigration.id, payload);
        if (initialMigration.status === "failed") {
          try {
            await submitJobForReview(dbId, res.job.id);
          } catch (reviewErr) {
            setError(
              `Migration tersimpan, tetapi gagal diajukan untuk review: ${
                reviewErr instanceof Error
                  ? reviewErr.message
                  : "kesalahan tidak diketahui"
              }`
            );
            return;
          }
        }
        router.push(`/databases/${dbId}/migrations/${res.migration.id}`);
      }
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Gagal menyimpan migration"
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form
      onSubmit={handleSubmit}
      className="space-y-6 rounded-xl border border-slate-200 bg-white p-5 shadow-sm sm:p-6"
    >
      {error && (
        <div className="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700">
          {error}
        </div>
      )}

      <div>
        <label className="block text-sm font-semibold text-slate-800">
          Judul Migration
        </label>
        <input
          type="text"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Contoh: Backfill status tier pelanggan aktif"
          maxLength={200}
          required
          className="mt-1.5 w-full rounded-lg border border-slate-300 px-3.5 py-2 text-sm text-slate-900 focus:border-blue-500 focus:outline-none"
        />
      </div>

      <div className="grid grid-cols-1 gap-4 rounded-xl border border-slate-200 bg-slate-50 p-4 sm:grid-cols-2">
        <div>
          <label className="block text-xs font-semibold uppercase tracking-wide text-slate-600">
            Mode Eksekusi
          </label>
          <div className="mt-2 flex gap-3">
            <label className="flex cursor-pointer items-center gap-2 rounded-lg border border-slate-300 bg-white px-3 py-2 text-xs font-medium text-slate-800">
              <input
                type="radio"
                name="executionMode"
                value="standard"
                checked={executionMode === "standard"}
                onChange={() => setExecutionMode("standard")}
              />
              Biasa (1 Transaksi Utuh)
            </label>
            <label className="flex cursor-pointer items-center gap-2 rounded-lg border border-slate-300 bg-white px-3 py-2 text-xs font-medium text-slate-800">
              <input
                type="radio"
                name="executionMode"
                value="batch"
                checked={executionMode === "batch"}
                onChange={() => setExecutionMode("batch")}
              />
              Per Batch (Cursor Checkpoint)
            </label>
          </div>
        </div>

        {executionMode === "batch" && (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="block text-xs font-semibold text-slate-700">
                Batch Column (Cursor)
              </label>
              <input
                type="text"
                value={batchColumn}
                onChange={(e) => setBatchColumn(e.target.value)}
                placeholder="id"
                className="mt-1 w-full rounded-lg border border-slate-300 bg-white px-3 py-1.5 font-mono text-xs text-slate-900"
              />
            </div>
            <div>
              <label className="block text-xs font-semibold text-slate-700">
                Batch Size (Rows / Batch)
              </label>
              <input
                type="number"
                min={1}
                value={batchSize}
                onChange={(e) => setBatchSize(Number(e.target.value))}
                className="mt-1 w-full rounded-lg border border-slate-300 bg-white px-3 py-1.5 font-mono text-xs text-slate-900"
              />
            </div>
          </div>
        )}
      </div>

      <div>
        <div className="flex items-center justify-between">
          <label className="block text-sm font-semibold text-slate-800">
            SQL Up (Perubahan Maju / Forward)
          </label>
          {executionMode === "batch" && (
            <span className="text-xs text-slate-500">
              Mode Batch: Gunakan statement{" "}
              <code className="font-mono">UPDATE &lt;tabel&gt;</code> atau{" "}
              <code className="font-mono">DELETE FROM &lt;tabel&gt;</code>
            </span>
          )}
        </div>
        <textarea
          rows={6}
          value={sql}
          onChange={(e) => setSql(e.target.value)}
          placeholder={
            executionMode === "batch"
              ? "UPDATE orders SET status = 'archived' WHERE status = 'completed'"
              : "ALTER TABLE users ADD COLUMN phone VARCHAR(30);"
          }
          required
          className="mt-1.5 w-full rounded-lg border border-slate-300 bg-slate-950 p-3.5 font-mono text-xs text-emerald-300 focus:border-blue-500 focus:outline-none"
        />
      </div>

      <div className="rounded-xl border border-slate-200 p-4">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <span className="text-sm font-semibold text-slate-800">
              Konfigurasi Rollback (SQL Down vs Irreversible)
            </span>
            <p className="text-xs text-slate-500">
              Setiap migration wajib menyediakan SQL Down untuk rollback, atau
              ditandai Irreversible beserta alasannya.
            </p>
          </div>
          <label className="inline-flex cursor-pointer items-center gap-2 self-start rounded-lg border border-amber-300 bg-amber-50 px-3 py-1.5 text-xs font-semibold text-amber-900 sm:self-auto">
            <input
              type="checkbox"
              checked={isIrreversible}
              onChange={(e) => setIsIrreversible(e.target.checked)}
            />
            Tandai Irreversible (Tidak Dapat Di-Rollback)
          </label>
        </div>

        {isIrreversible ? (
          <div className="mt-4">
            <label className="block text-xs font-semibold text-amber-900">
              Alasan Irreversible (Wajib)
            </label>
            <textarea
              rows={3}
              value={irreversibleReason}
              onChange={(e) => setIrreversibleReason(e.target.value)}
              placeholder="Jelaskan mengapa perubahan ini tidak dapat dibatalkan (misal: penghapusan kolom legacy permanen)..."
              className="mt-1.5 w-full rounded-lg border border-amber-300 bg-amber-50/40 p-3 text-xs text-slate-900 focus:border-amber-500 focus:outline-none"
            />
          </div>
        ) : (
          <div className="mt-4">
            <label className="block text-xs font-semibold text-slate-700">
              SQL Down (Perintah Rollback / Kompensasi Batch Terbalik)
            </label>
            <textarea
              rows={5}
              value={sqlDown}
              onChange={(e) => setSqlDown(e.target.value)}
              placeholder={
                executionMode === "batch"
                  ? "UPDATE orders SET status = 'completed' WHERE status = 'archived'"
                  : "ALTER TABLE users DROP COLUMN phone;"
              }
              className="mt-1.5 w-full rounded-lg border border-slate-300 bg-slate-950 p-3.5 font-mono text-xs text-amber-300 focus:border-blue-500 focus:outline-none"
            />
          </div>
        )}
      </div>

      <div className="flex flex-col-reverse gap-3 border-t border-slate-100 pt-4 sm:flex-row sm:justify-end">
        <button
          type="button"
          onClick={() => router.back()}
          className="rounded-lg border border-slate-300 bg-white px-4 py-2 text-xs font-semibold text-slate-700 hover:bg-slate-50"
        >
          Batal
        </button>
        <button
          type="submit"
          disabled={submitting}
          className="rounded-lg bg-blue-600 px-5 py-2 text-xs font-semibold text-white shadow-sm hover:bg-blue-700 disabled:opacity-50"
        >
          {submitting
            ? "Menyimpan..."
            : mode === "create"
            ? "Buat Draft Migration"
            : "Simpan Perubahan Draft"}
        </button>
      </div>
    </form>
  );
}
