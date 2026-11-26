export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

let csrfToken = "";
let onUnauthorized: (() => void) | null = null;

export function setCsrfToken(token: string) {
  csrfToken = token;
}

export function setUnauthorizedHandler(fn: (() => void) | null) {
  onUnauthorized = fn;
}

export async function api<T = unknown>(
  path: string,
  options: { method?: string; body?: unknown; signal?: AbortSignal } = {},
): Promise<T> {
  const method = options.method ?? "GET";
  const headers: Record<string, string> = { Accept: "application/json" };
  if (options.body !== undefined) headers["Content-Type"] = "application/json";
  if (method !== "GET" && csrfToken) headers["X-CSRF-Token"] = csrfToken;

  let res: Response;
  try {
    res = await fetch(path, {
      method,
      headers,
      credentials: "same-origin",
      body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
      signal: options.signal,
    });
  } catch {
    throw new ApiError(0, "Tidak dapat menghubungi server. Periksa koneksi Anda.");
  }

  let data: unknown = null;
  const text = await res.text();
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }

  if (!res.ok) {
    let message = `Permintaan gagal (${res.status})`;
    if (data && typeof data === "object" && "error" in data) {
      const e = (data as { error: unknown }).error;
      if (typeof e === "string" && e) message = e;
    }
    if (res.status === 401 && !path.startsWith("/api/auth/login")) onUnauthorized?.();
    throw new ApiError(res.status, message);
  }
  // Endpoint yang merotasi sesi mengembalikan csrf_token baru.
  if (data && typeof data === "object" && "csrf_token" in data && typeof (data as { csrf_token: unknown }).csrf_token === "string") {
    csrfToken = (data as { csrf_token: string }).csrf_token;
  }
  return data as T;
}

export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : "Terjadi kesalahan";
}
