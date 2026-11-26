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
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

interface ApiErrorBody {
  error?: unknown;
}

interface SessionResponse {
  csrf_token?: unknown;
}

const csrfStorageKey = "csrf_token";

export function storeCsrfToken(token: string): void {
  if (!token.trim()) {
    throw new Error("CSRF token cannot be empty");
  }
  sessionStorage.setItem(csrfStorageKey, token);
}

async function getCsrfToken(): Promise<string> {
  if (typeof window === "undefined") {
    throw new Error("A browser session is required to send this request");
  }

  const cached = sessionStorage.getItem(csrfStorageKey);
  if (cached) {
    return cached;
  }

  const session = await apiRequest<SessionResponse>("/api/auth/me");
  if (typeof session.csrf_token !== "string" || !session.csrf_token.trim()) {
    throw new Error("The authenticated session did not provide a CSRF token");
  }
  storeCsrfToken(session.csrf_token);
  return session.csrf_token;
}

function errorMessage(payload: unknown, fallback: string): string {
  if (
    typeof payload === "object" &&
    payload !== null &&
    "error" in payload
  ) {
    const body = payload as ApiErrorBody;
    if (typeof body.error === "string" && body.error.trim()) {
      return body.error;
    }
  }
  return fallback;
}

export async function apiRequest<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");

  if (!["GET", "HEAD", "OPTIONS"].includes(method)) {
    headers.set("X-CSRF-Token", await getCsrfToken());
  }

  const response = await fetch(path, {
    ...init,
    method,
    headers,
    credentials: "include",
    cache: "no-store",
  });
  const text = await response.text();
  let payload: unknown = null;

  if (text) {
    try {
      payload = JSON.parse(text) as unknown;
    } catch {
      if (response.ok) {
        throw new ApiError("The server returned an invalid JSON response", response.status);
      }
      payload = { error: text.trim() };
    }
  }

  if (!response.ok) {
    throw new ApiError(
      errorMessage(payload, response.statusText || "Request failed"),
      response.status,
    );
  }
  if (payload === null) {
    throw new ApiError("The server returned an empty response", response.status);
  }

  return payload as T;
}
