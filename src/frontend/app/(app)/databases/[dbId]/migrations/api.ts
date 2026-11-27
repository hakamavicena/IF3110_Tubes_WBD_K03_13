import { apiRequest } from "@/lib/api";

export type ExecutionMode = "standard" | "batch";

export type MigrationStatus =
  | "draft"
  | "pending_review"
  | "approved"
  | "pending_queue"
  | "running"
  | "success"
  | "failed"
  | "partial"
  | "partially_reverted";

export type ActiveState =
  | "not_applied"
  | "applied"
  | "rolled_back"
  | "partial"
  | "partially_reverted";

export type JobAction =
  | "apply"
  | "rollback"
  | "forward"
  | "retry_batch"
  | "cancel_batch";

export interface DatabaseLockInfo {
  database_id: number;
  job_id: number;
  worker_id: string;
  lock_state: "running" | "holding_partial" | "uncertain";
  acquired_at: string;
  heartbeat_at: string;
}

export interface MigrationItem {
  id: number;
  database_id: number;
  version: number;
  revision: number;
  title: string;
  sql: string;
  sql_down?: string;
  is_irreversible: boolean;
  irreversible_reason?: string;
  execution_mode: ExecutionMode;
  batch_column?: string;
  batch_size?: number;
  is_immutable: boolean;
  active_state: ActiveState;
  active_execution_id?: number;
  status: MigrationStatus;
  error_message?: string;
  created_by: number;
  created_by_name: string;
  created_at: string;
  executed_by?: number;
  executed_by_name?: string;
  executed_at?: string;
  duration_ms?: number;
}

export interface MigrationJob {
  id: number;
  database_id: number;
  migration_id: number;
  action: JobAction;
  status: MigrationStatus;
  reverts_execution_id?: number;
  sql_snapshot: string;
  execution_mode: ExecutionMode;
  batch_column?: string;
  batch_size?: number;
  committed_batches: number;
  committed_rows: number;
  last_cursor?: string;
  retry_count: number;
  error_message?: string;
  trace_parent?: string;
  requested_by: number;
  created_at: string;
  queued_at?: string;
  started_at?: string;
  finished_at?: string;
  duration_ms?: number;
}

export interface MigrationListResponse {
  database_id: number;
  can_edit: boolean;
  active_lock: DatabaseLockInfo | null;
  migrations: MigrationItem[];
}

export interface MigrationDetailResponse {
  database_id: number;
  can_edit: boolean;
  active_lock: DatabaseLockInfo | null;
  migration: MigrationItem;
  jobs: MigrationJob[];
}

export interface MigrationUpsertPayload {
  title: string;
  sql: string;
  sql_down: string;
  is_irreversible: boolean;
  irreversible_reason: string;
  execution_mode: ExecutionMode;
  batch_column: string;
  batch_size: number;
}

// Memakai klien bersama (@/lib/api): satu sumber CSRF, 401 ditangani terpusat.
function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  return apiRequest<T>(url, init);
}

export async function fetchMigrations(
  dbId: string | number
): Promise<MigrationListResponse> {
  return requestJSON<MigrationListResponse>(`/api/databases/${dbId}/migrations`);
}

export async function fetchMigrationDetail(
  dbId: string | number,
  migId: string | number
): Promise<MigrationDetailResponse> {
  return requestJSON<MigrationDetailResponse>(
    `/api/databases/${dbId}/migrations/${migId}`
  );
}

export async function createMigration(
  dbId: string | number,
  payload: MigrationUpsertPayload
): Promise<{ migration: MigrationItem; job: MigrationJob }> {
  return requestJSON<{ migration: MigrationItem; job: MigrationJob }>(
    `/api/databases/${dbId}/migrations`,
    {
      method: "POST",
      body: JSON.stringify(payload),
    }
  );
}

export async function updateMigration(
  dbId: string | number,
  migId: string | number,
  payload: MigrationUpsertPayload
): Promise<{ migration: MigrationItem; job: MigrationJob }> {
  return requestJSON<{ migration: MigrationItem; job: MigrationJob }>(
    `/api/databases/${dbId}/migrations/${migId}`,
    {
      method: "PUT",
      body: JSON.stringify(payload),
    }
  );
}

export async function submitJobForReview(
  dbId: string | number,
  jobId: string | number
): Promise<void> {
  await requestJSON<unknown>(
    `/api/databases/${dbId}/jobs/${jobId}/submit-review`,
    { method: "POST" }
  );
}

export async function enqueueJob(
  dbId: string | number,
  jobId: string | number
): Promise<{ job_id: number; status: MigrationStatus; job: MigrationJob }> {
  return requestJSON<{
    job_id: number;
    status: MigrationStatus;
    job: MigrationJob;
  }>(`/api/databases/${dbId}/jobs/${jobId}/enqueue`, {
    method: "POST",
  });
}

export async function fetchJobStatus(
  dbId: string | number,
  jobId: string | number
): Promise<{ job: MigrationJob }> {
  return requestJSON<{ job: MigrationJob }>(
    `/api/databases/${dbId}/jobs/${jobId}`
  );
}

export async function requestRetryBatch(
  dbId: string | number,
  migId: string | number
): Promise<{ job_id: number; status: MigrationStatus; job: MigrationJob }> {
  return requestJSON<{
    job_id: number;
    status: MigrationStatus;
    job: MigrationJob;
  }>(`/api/databases/${dbId}/migrations/${migId}/retry-batch`, {
    method: "POST",
  });
}

export async function requestCancelBatch(
  dbId: string | number,
  migId: string | number
): Promise<{ job_id: number; status: MigrationStatus; job: MigrationJob }> {
  return requestJSON<{
    job_id: number;
    status: MigrationStatus;
    job: MigrationJob;
  }>(`/api/databases/${dbId}/migrations/${migId}/cancel-batch`, {
    method: "POST",
  });
}
