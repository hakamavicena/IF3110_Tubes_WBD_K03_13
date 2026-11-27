"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { apiRequest, type TableInfo } from "@/lib/api";
import {
  ErrorPanel,
  LoadingPanel,
  WorkspaceShell,
} from "@/components/workspace-shell";

interface SchemaListResponse {
  schemas: string[];
}

interface TableListResponse {
  tables: TableInfo[];
}

export function DatabaseExplorer({ databaseId }: { databaseId: string }) {
  const [schemas, setSchemas] = useState<string[]>([]);
  const [selectedSchema, setSelectedSchema] = useState("");
  const [tables, setTables] = useState<TableInfo[]>([]);
  const [filter, setFilter] = useState("");
  const [schemasLoading, setSchemasLoading] = useState(true);
  const [tablesLoading, setTablesLoading] = useState(false);
  const [schemaError, setSchemaError] = useState<string | null>(null);
  const [tableError, setTableError] = useState<string | null>(null);
  const [schemaRetry, setSchemaRetry] = useState(0);
  const [tableRetry, setTableRetry] = useState(0);
  const selectedSchemaRef = useRef("");

  const loadSchemas = useCallback(
    async (signal: AbortSignal) => {
      try {
        const result = await apiRequest<SchemaListResponse>(
          `/api/databases/${encodeURIComponent(databaseId)}/schemas`,
          { signal },
        );
        setSchemas(result.schemas);
        const preferred = selectedSchemaRef.current;
        const nextSchema =
          preferred && result.schemas.includes(preferred)
            ? preferred
            : (result.schemas[0] ?? "");
        selectedSchemaRef.current = nextSchema;
        if (nextSchema !== preferred) {
          setTables([]);
          setTableError(null);
          setTablesLoading(Boolean(nextSchema));
        } else if (!nextSchema) {
          setTables([]);
          setTablesLoading(false);
          setTableError(null);
        }
        setSelectedSchema(nextSchema);
      } catch (loadError) {
        if (!signal.aborted) {
          setSchemaError(
            loadError instanceof Error
              ? loadError.message
              : "Unable to load schemas",
          );
        }
      } finally {
        if (!signal.aborted) {
          setSchemasLoading(false);
        }
      }
    },
    [databaseId],
  );

  useEffect(() => {
    const controller = new AbortController();
    void Promise.resolve().then(() => {
      if (!controller.signal.aborted) {
        return loadSchemas(controller.signal);
      }
    });
    return () => controller.abort();
  }, [loadSchemas, schemaRetry]);

  const loadTables = useCallback(
    async (schema: string, signal: AbortSignal) => {
      setTablesLoading(true);
      setTableError(null);
      try {
        const result = await apiRequest<TableListResponse>(
          `/api/databases/${encodeURIComponent(databaseId)}/schemas/${encodeURIComponent(schema)}/tables`,
          { signal },
        );
        setTables(result.tables);
      } catch (loadError) {
        if (!signal.aborted) {
          setTableError(
            loadError instanceof Error
              ? loadError.message
              : "Unable to load tables",
          );
        }
      } finally {
        if (!signal.aborted) {
          setTablesLoading(false);
        }
      }
    },
    [databaseId],
  );

  useEffect(() => {
    if (!selectedSchema) {
      return;
    }
    const controller = new AbortController();
    void Promise.resolve().then(() => {
      if (!controller.signal.aborted) {
        return loadTables(selectedSchema, controller.signal);
      }
    });
    return () => controller.abort();
  }, [loadTables, selectedSchema, tableRetry]);

  const visibleTables = useMemo(() => {
    const normalized = filter.trim().toLocaleLowerCase();
    if (!normalized) {
      return tables;
    }
    return tables.filter(
      (table) =>
        table.name.toLocaleLowerCase().includes(normalized) ||
        table.type.toLocaleLowerCase().includes(normalized),
    );
  }, [filter, tables]);

  return (
    <WorkspaceShell
      databaseId={databaseId}
      description="Explore schemas and tables in this target. Open a table to inspect its structure and paginated rows."
      eyebrow={`Database target · ${databaseId}`}
      section="viewer"
      title="Schema explorer"
    >
      <div className="grid gap-5 lg:grid-cols-[minmax(230px,0.75fr)_minmax(0,2fr)]">
        <aside className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-xs font-bold uppercase tracking-wider text-slate-500">
                Structure
              </p>
              <h2 className="mt-1 text-lg font-bold">Schemas</h2>
            </div>
            <span className="rounded-full bg-slate-100 px-2.5 py-1 text-xs font-bold text-slate-600">
              {schemas.length}
            </span>
          </div>

          {schemasLoading ? (
            <div className="mt-5">
              <LoadingPanel label="Loading schemas…" />
            </div>
          ) : schemaError ? (
            <div className="mt-5">
              <ErrorPanel
                message={schemaError}
                onRetry={() => {
                  setSchemasLoading(true);
                  setSchemaError(null);
                  setSchemaRetry((value) => value + 1);
                }}
              />
            </div>
          ) : schemas.length === 0 ? (
            <p className="mt-5 rounded-xl bg-slate-50 p-4 text-sm text-slate-500">
              This database has no visible schemas.
            </p>
          ) : (
            <ul className="mt-5 space-y-1">
              {schemas.map((schema) => (
                <li key={schema}>
                  <button
                    aria-current={selectedSchema === schema ? "true" : undefined}
                    className={`flex w-full items-center justify-between rounded-xl px-3 py-2.5 text-left text-sm font-semibold transition ${
                      selectedSchema === schema
                        ? "bg-cyan-50 text-cyan-950 ring-1 ring-cyan-200"
                        : "text-slate-600 hover:bg-slate-50 hover:text-slate-950"
                    }`}
                    onClick={() => {
                      if (schema !== selectedSchema) {
                        selectedSchemaRef.current = schema;
                        setTables([]);
                        setTablesLoading(true);
                        setTableError(null);
                        setSelectedSchema(schema);
                      }
                    }}
                    type="button"
                  >
                    <span className="truncate">{schema}</span>
                    <span aria-hidden="true" className="ml-2 text-cyan-700">
                      ›
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </aside>

        <section className="min-w-0 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm sm:p-6">
          <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
            <div>
              <p className="text-xs font-bold uppercase tracking-wider text-slate-500">
                {selectedSchema || "Selected schema"}
              </p>
              <h2 className="mt-1 text-lg font-bold">Tables and views</h2>
            </div>
            <label className="w-full sm:max-w-xs">
              <span className="sr-only">Search tables</span>
              <input
                className="w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-2.5 text-sm outline-none transition placeholder:text-slate-400 focus:border-cyan-500 focus:bg-white focus:ring-4 focus:ring-cyan-100"
                onChange={(event) => setFilter(event.target.value)}
                placeholder="Filter by table name"
                type="search"
                value={filter}
              />
            </label>
          </div>

          {tablesLoading ? (
            <LoadingPanel label="Loading tables…" />
          ) : tableError ? (
            <ErrorPanel
              message={tableError}
              onRetry={() => {
                setTablesLoading(true);
                setTableError(null);
                setTableRetry((value) => value + 1);
              }}
            />
          ) : !selectedSchema ? (
            <div className="rounded-xl border border-dashed border-slate-300 bg-slate-50 px-5 py-12 text-center text-sm text-slate-500">
              Select a schema to view its tables.
            </div>
          ) : visibleTables.length === 0 ? (
            <div className="rounded-xl border border-dashed border-slate-300 bg-slate-50 px-5 py-12 text-center">
              <p className="font-semibold text-slate-800">No tables found</p>
              <p className="mt-2 text-sm text-slate-500">
                {tables.length === 0
                  ? "This schema contains no visible tables or views."
                  : "Try another table name."}
              </p>
            </div>
          ) : (
            <div className="divide-y divide-slate-100">
              {visibleTables.map((table) => (
                <Link
                  className="flex min-w-0 items-center gap-3 py-3 first:pt-0 last:pb-0 hover:text-cyan-900"
                  href={`/databases/${encodeURIComponent(databaseId)}/viewer/${encodeURIComponent(table.schema)}/${encodeURIComponent(table.name)}`}
                  key={`${table.schema}.${table.name}`}
                >
                  <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-slate-100 text-xs font-black text-slate-600">
                    {table.type === "BASE TABLE" ? "TBL" : "VIEW"}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-bold">
                      {table.name}
                    </span>
                    <span className="mt-0.5 block text-xs text-slate-500">
                      {table.schema} · {table.type}
                    </span>
                  </span>
                  <span aria-hidden="true" className="shrink-0 text-lg text-cyan-700">
                    →
                  </span>
                </Link>
              ))}
            </div>
          )}
        </section>
      </div>
    </WorkspaceShell>
  );
}
