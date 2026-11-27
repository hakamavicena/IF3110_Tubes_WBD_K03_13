"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import {
  apiRequest,
  type TableData,
  type TableDetail,
} from "@/lib/api";
import {
  ErrorPanel,
  LoadingPanel,
  WorkspaceShell,
} from "@/components/workspace-shell";

export function TableInspector({
  databaseId,
  schemaName,
  tableName,
}: {
  databaseId: string;
  schemaName: string;
  tableName: string;
}) {
  const [detail, setDetail] = useState<TableDetail | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [detailLoading, setDetailLoading] = useState(true);
  const [tableData, setTableData] = useState<TableData | null>(null);
  const [dataError, setDataError] = useState<string | null>(null);
  const [dataLoading, setDataLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [detailRetry, setDetailRetry] = useState(0);
  const [dataRetry, setDataRetry] = useState(0);

  const encodedPath = `/api/databases/${encodeURIComponent(databaseId)}/schemas/${encodeURIComponent(schemaName)}/tables/${encodeURIComponent(tableName)}`;

  const loadDetail = useCallback(
    async (signal: AbortSignal) => {
      try {
        const result = await apiRequest<TableDetail>(encodedPath, { signal });
        setDetail(result);
      } catch (loadError) {
        if (!signal.aborted) {
          setDetailError(
            loadError instanceof Error
              ? loadError.message
              : "Unable to load table details",
          );
        }
      } finally {
        if (!signal.aborted) {
          setDetailLoading(false);
        }
      }
    },
    [encodedPath],
  );

  useEffect(() => {
    const controller = new AbortController();
    void Promise.resolve().then(() => {
      if (!controller.signal.aborted) {
        return loadDetail(controller.signal);
      }
    });
    return () => controller.abort();
  }, [loadDetail, detailRetry]);

  const loadData = useCallback(
    async (signal: AbortSignal) => {
      try {
        const query = new URLSearchParams({
          page: String(page),
          size: String(pageSize),
        });
        const result = await apiRequest<TableData>(
          `${encodedPath}/data?${query.toString()}`,
          { signal },
        );
        setTableData(result);
      } catch (loadError) {
        if (!signal.aborted) {
          setDataError(
            loadError instanceof Error
              ? loadError.message
              : "Unable to load table rows",
          );
        }
      } finally {
        if (!signal.aborted) {
          setDataLoading(false);
        }
      }
    },
    [encodedPath, page, pageSize],
  );

  useEffect(() => {
    const controller = new AbortController();
    void Promise.resolve().then(() => {
      if (!controller.signal.aborted) {
        return loadData(controller.signal);
      }
    });
    return () => controller.abort();
  }, [loadData, dataRetry]);

  const pageCount = tableData?.pages ?? 0;
  const firstRow = tableData && tableData.total > 0
    ? (tableData.page - 1) * tableData.page_size + 1
    : 0;
  const lastRow = tableData
    ? Math.min(tableData.page * tableData.page_size, tableData.total)
    : 0;

  return (
    <WorkspaceShell
      databaseId={databaseId}
      description="Inspect columns, constraints, indexes, and table rows. Wide data stays inside a horizontally scrollable table."
      eyebrow={`${schemaName} · ${tableName}`}
      section="viewer"
      title={tableName}
    >
      <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <Link
          className="inline-flex items-center gap-2 rounded-xl border border-slate-300 bg-white px-3 py-2 text-sm font-semibold text-slate-700 hover:bg-slate-50"
          href={`/databases/${encodeURIComponent(databaseId)}/viewer`}
        >
          <span aria-hidden="true">←</span>
          Back to explorer
        </Link>
        {detail?.table && (
          <span className="rounded-full bg-slate-200 px-3 py-1.5 text-xs font-bold text-slate-700">
            {detail.table.type}
          </span>
        )}
      </div>

      <section className="mb-6 grid gap-4 sm:grid-cols-3">
        {[
          { label: "Columns", value: detail?.columns.length ?? "—" },
          { label: "Constraints", value: detail?.constraints.length ?? "—" },
          { label: "Indexes", value: detail?.indexes.length ?? "—" },
        ].map((item) => (
          <div
            className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm"
            key={item.label}
          >
            <p className="text-xs font-bold uppercase tracking-wider text-slate-500">
              {item.label}
            </p>
            <p className="mt-2 text-2xl font-bold">{item.value}</p>
          </div>
        ))}
      </section>

      <section className="mb-6 grid gap-5 xl:grid-cols-3">
        <MetadataPanel
          title="Columns"
          loading={detailLoading}
          error={detailError}
          onRetry={() => {
            setDetailLoading(true);
            setDetailError(null);
            setDetailRetry((value) => value + 1);
          }}
        >
          {detail?.columns.length ? (
            <ul className="divide-y divide-slate-100">
              {detail.columns.map((column) => (
                <li className="py-3 first:pt-0 last:pb-0" key={column.name}>
                  <div className="flex items-start justify-between gap-3">
                    <span className="min-w-0">
                      <span className="block truncate text-sm font-bold">
                        {column.name}
                      </span>
                      <span className="mt-1 block break-all font-mono text-xs text-cyan-800">
                        {column.data_type}
                      </span>
                    </span>
                    <span className="shrink-0 rounded-full bg-slate-100 px-2 py-1 text-[10px] font-bold uppercase text-slate-500">
                      {column.nullable ? "Nullable" : "Required"}
                    </span>
                  </div>
                  {column.default !== null && (
                    <p className="mt-2 break-all text-xs text-slate-500">
                      Default: <code>{column.default}</code>
                    </p>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <EmptyMetadata label="No columns returned." />
          )}
        </MetadataPanel>

        <MetadataPanel
          title="Constraints"
          loading={detailLoading}
          error={detailError}
          onRetry={() => {
            setDetailLoading(true);
            setDetailError(null);
            setDetailRetry((value) => value + 1);
          }}
        >
          {detail?.constraints.length ? (
            <ul className="space-y-3">
              {detail.constraints.map((constraint) => (
                <li
                  className="rounded-xl bg-slate-50 p-3"
                  key={constraint.name}
                >
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="break-all text-sm font-bold">
                      {constraint.name}
                    </span>
                    <span className="rounded-full bg-violet-100 px-2 py-1 text-[10px] font-bold uppercase text-violet-800">
                      {constraint.type}
                    </span>
                  </div>
                  <code className="mt-2 block break-all text-xs leading-5 text-slate-600">
                    {constraint.definition}
                  </code>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyMetadata label="No constraints found." />
          )}
        </MetadataPanel>

        <MetadataPanel
          title="Indexes"
          loading={detailLoading}
          error={detailError}
          onRetry={() => {
            setDetailLoading(true);
            setDetailError(null);
            setDetailRetry((value) => value + 1);
          }}
        >
          {detail?.indexes.length ? (
            <ul className="space-y-3">
              {detail.indexes.map((index) => (
                <li className="rounded-xl bg-slate-50 p-3" key={index.name}>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="break-all text-sm font-bold">{index.name}</span>
                    {index.unique && (
                      <span className="rounded-full bg-amber-100 px-2 py-1 text-[10px] font-bold uppercase text-amber-800">
                        Unique
                      </span>
                    )}
                  </div>
                  <code className="mt-2 block break-all text-xs leading-5 text-slate-600">
                    {index.definition}
                  </code>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyMetadata label="No indexes found." />
          )}
        </MetadataPanel>
      </section>

      <section className="min-w-0 rounded-2xl border border-slate-200 bg-white shadow-sm">
        <div className="flex flex-col gap-4 border-b border-slate-100 p-4 sm:flex-row sm:items-end sm:justify-between sm:p-6">
          <div>
            <p className="text-xs font-bold uppercase tracking-wider text-slate-500">
              Data preview
            </p>
            <h2 className="mt-1 text-lg font-bold">Rows</h2>
            <p className="mt-1 text-sm text-slate-500">
              {tableData
                ? `Showing ${firstRow}–${lastRow} of ${tableData.total.toLocaleString()} rows`
                : "Paginated read-only data"}
            </p>
          </div>
          <label className="flex items-center gap-2 text-sm font-semibold text-slate-600">
            Rows per page
            <select
              className="rounded-lg border border-slate-300 bg-white px-2.5 py-2 text-sm outline-none focus:border-cyan-500 focus:ring-4 focus:ring-cyan-100"
              onChange={(event) => {
                setDataLoading(true);
                setDataError(null);
                setPageSize(Number(event.target.value));
                setPage(1);
              }}
              value={pageSize}
            >
              {[10, 25, 50, 100, 200].map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </select>
          </label>
        </div>

        {dataLoading ? (
          <div className="p-4 sm:p-6">
            <LoadingPanel label="Loading table rows…" />
          </div>
        ) : dataError ? (
          <div className="p-4 sm:p-6">
            <ErrorPanel
              message={dataError}
              onRetry={() => {
                setDataLoading(true);
                setDataError(null);
                setDataRetry((value) => value + 1);
              }}
            />
          </div>
        ) : !tableData || tableData.columns.length === 0 ? (
          <div className="p-8 text-center text-sm text-slate-500">
            No columns or rows are available for this table.
          </div>
        ) : (
          <>
            <div className="max-w-full overflow-x-auto">
              <table className="w-full min-w-max border-collapse text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase tracking-wide text-slate-500">
                  <tr>
                    <th className="sticky left-0 z-10 bg-slate-50 px-4 py-3 font-bold">
                      #
                    </th>
                    {tableData.columns.map((column) => (
                      <th className="max-w-72 px-4 py-3 font-bold" key={column}>
                        {column}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {tableData.rows.map((row, rowIndex) => (
                    <tr className="hover:bg-cyan-50/40" key={`${page}-${rowIndex}`}>
                      <td className="sticky left-0 bg-white px-4 py-3 text-xs font-bold text-slate-400">
                        {firstRow + rowIndex}
                      </td>
                      {tableData.columns.map((column, columnIndex) => {
                        const value = row[columnIndex];
                        return (
                          <td
                            className="max-w-72 px-4 py-3 align-top text-slate-700"
                            key={`${column}-${columnIndex}`}
                          >
                            <span
                              className={`block max-w-72 whitespace-pre-wrap break-words ${
                                value === null ? "italic text-slate-400" : ""
                              }`}
                            >
                              {value === null ? "NULL" : value}
                            </span>
                          </td>
                        );
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div className="flex flex-col gap-3 border-t border-slate-100 px-4 py-4 sm:flex-row sm:items-center sm:justify-between sm:px-6">
              <p className="text-sm text-slate-500">
                Page {tableData.page} of {Math.max(pageCount, 1)}
              </p>
              <div className="flex gap-2">
                <button
                  className="rounded-lg border border-slate-300 px-3 py-2 text-sm font-semibold text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                  disabled={tableData.page <= 1}
                  onClick={() => {
                    setDataLoading(true);
                    setDataError(null);
                    setPage((value) => Math.max(1, value - 1));
                  }}
                  type="button"
                >
                  Previous
                </button>
                <button
                  className="rounded-lg border border-slate-300 px-3 py-2 text-sm font-semibold text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                  disabled={tableData.page >= pageCount}
                  onClick={() => {
                    setDataLoading(true);
                    setDataError(null);
                    setPage((value) => value + 1);
                  }}
                  type="button"
                >
                  Next
                </button>
              </div>
            </div>
          </>
        )}
      </section>
    </WorkspaceShell>
  );
}

function MetadataPanel({
  title,
  loading,
  error,
  onRetry,
  children,
}: {
  title: string;
  loading: boolean;
  error: string | null;
  onRetry: () => void;
  children: React.ReactNode;
}) {
  return (
    <section className="min-w-0 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm sm:p-5">
      <h2 className="mb-4 text-base font-bold">{title}</h2>
      {loading ? (
        <LoadingPanel label={`Loading ${title.toLocaleLowerCase()}…`} />
      ) : error ? (
        <ErrorPanel message={error} onRetry={onRetry} />
      ) : (
        children
      )}
    </section>
  );
}

function EmptyMetadata({ label }: { label: string }) {
  return <p className="rounded-xl bg-slate-50 p-4 text-sm text-slate-500">{label}</p>;
}
