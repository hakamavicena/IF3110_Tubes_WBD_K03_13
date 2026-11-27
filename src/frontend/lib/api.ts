export type AccessLevel = "view" | "edit";

export interface DatabaseAccess {
  id: number;
  name: string;
  access: AccessLevel;
}

export interface TableInfo {
  schema: string;
  name: string;
  type: string;
}

export interface ColumnInfo {
  position: number;
  name: string;
  data_type: string;
  nullable: boolean;
  default: string | null;
}

export interface ConstraintInfo {
  name: string;
  type: string;
  definition: string;
}

export interface IndexInfo {
  name: string;
  definition: string;
  unique: boolean;
}

export interface TableDetail {
  table: TableInfo;
  columns: ColumnInfo[];
  constraints: ConstraintInfo[];
  indexes: IndexInfo[];
}

export interface TableData {
  columns: string[];
  rows: (string | null)[][];
  page: number;
  page_size: number;
  total: number;
  pages: number;
}

export type ExecutionAction = "apply" | "rollback" | "forward";
export type ExecutionStatus = "success" | "failed";

export interface MigrationExecution {
  execution_id: number;
  database_id: number;
  migration_id: number;
  job_id: number | null;
  version: number;
  revision: number;
  title: string;
  action: ExecutionAction;
  sql_executed: string;
  executed_by: number;
  executed_by_name: string;
  executed_at: string;
  duration_ms: number;
  rows_affected: number;
  status: ExecutionStatus;
  reverted_execution_id: number | null;
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

const CSRF_KEY = "csrf_token";
let csrfToken = "";
let csrfLoading: Promise<string> | null = null;
let onUnauthorized: (() => void) | null = null;

function readStoredCsrf(): string {
  if (typeof window === "undefined") return "";
  try {
    return window.sessionStorage.getItem(CSRF_KEY) ?? "";
  } catch {
    return "";
  }
}

export function setCsrfToken(token: string) {
  csrfToken = token;
  if (typeof window === "undefined") return;
  try {
    if (token) window.sessionStorage.setItem(CSRF_KEY, token);
    else window.sessionStorage.removeItem(CSRF_KEY);
  } catch {
    /* sessionStorage tidak tersedia: cukup simpan di memori */
  }
}

export function storeCsrfToken(token: string): void {
  setCsrfToken(token);
}

export function setUnauthorizedHandler(fn: (() => void) | null) {
  onUnauthorized = fn;
}

export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : "Terjadi kesalahan";
}

function messageFrom(data: unknown, fallback: string): string {
  if (data && typeof data === "object" && "error" in data) {
    const e = (data as { error: unknown }).error;
    if (typeof e === "string" && e.trim()) return e;
  }
  return fallback;
}

async function ensureCsrf(): Promise<string> {
  if (csrfToken) return csrfToken;
  const stored = readStoredCsrf();
  if (stored) {
    csrfToken = stored;
    return stored;
  }
  csrfLoading ??= (async () => {
    try {
      const res = await fetch("/api/auth/me", { credentials: "include", cache: "no-store", headers: { Accept: "application/json" } });
      const body = (await res.json().catch(() => null)) as { csrf_token?: unknown } | null;
      if (typeof body?.csrf_token === "string" && body.csrf_token) setCsrfToken(body.csrf_token);
    } finally {
      csrfLoading = null;
    }
    return csrfToken;
  })();
  return csrfLoading;
}

export async function apiRequest<T = unknown>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body !== undefined && init.body !== null && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const isLogin = path.startsWith("/api/auth/login");
  if (!["GET", "HEAD", "OPTIONS"].includes(method) && !isLogin) {
    const token = await ensureCsrf();
    if (token) headers.set("X-CSRF-Token", token);
  }

  let res: Response;
  try {
    res = await fetch(path, { ...init, method, headers, credentials: "include", cache: "no-store" });
  } catch (e) {
    if (e instanceof DOMException && e.name === "AbortError") throw e;
    throw new ApiError(0, "Tidak dapat menghubungi server. Periksa koneksi Anda.");
  }

  let data: unknown = null;
  const text = await res.text();
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = res.ok ? null : { error: text.trim() };
    }
  }

  if (!res.ok) {
    if (res.status === 401 && !isLogin) onUnauthorized?.();
    throw new ApiError(res.status, messageFrom(data, `Permintaan gagal (${res.status})`));
  }
  // Endpoint yang membuat/merotasi sesi mengembalikan csrf_token baru.
  if (data && typeof data === "object" && "csrf_token" in data) {
    const t = (data as { csrf_token: unknown }).csrf_token;
    if (typeof t === "string" && t) setCsrfToken(t);
  }
  return data as T;
}

export async function api<T = unknown>(
  path: string,
  options: { method?: string; body?: unknown; signal?: AbortSignal } = {},
): Promise<T> {
  return apiRequest<T>(path, {
    method: options.method ?? "GET",
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    signal: options.signal,
  });
}
