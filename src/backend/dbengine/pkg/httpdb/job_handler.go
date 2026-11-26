package httpdb

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"backend/dbengine/pkg/apperr"
	"backend/dbengine/pkg/migration"
)

type JobAPIHandler struct {
	coordinator *migration.JobCoordinator
}

func NewJobAPIHandler(coordinator *migration.JobCoordinator) *JobAPIHandler {
	return &JobAPIHandler{coordinator: coordinator}
}

func (jh *JobAPIHandler) RegisterRoutes(mux *http.ServeMux, protect func(http.Handler) http.Handler) {
	if mux == nil || protect == nil || jh == nil || jh.coordinator == nil {
		return
	}
	route := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, protect(handler))
	}

	route("POST /api/databases/{dbID}/jobs/{jobID}/enqueue", jh.enqueueJob)
	route("GET /api/databases/{dbID}/jobs/{jobID}", jh.getJobStatus)
	route("POST /api/databases/{dbID}/migrations/{migID}/retry-batch", jh.retryBatch)
	route("POST /api/databases/{dbID}/migrations/{migID}/cancel-batch", jh.cancelBatch)

	route("GET /api/databases/{dbID}/migrations", jh.listMigrationsJSON)
	route("POST /api/databases/{dbID}/migrations", jh.createMigrationJSON)
	route("GET /api/databases/{dbID}/migrations/{migID}", jh.getMigrationDetailJSON)
	route("PUT /api/databases/{dbID}/migrations/{migID}", jh.updateMigrationJSON)
}

type jobDTO struct {
	ID                 int64      `json:"id"`
	DatabaseID         int64      `json:"database_id"`
	MigrationID        int64      `json:"migration_id"`
	Action             string     `json:"action"`
	Status             string     `json:"status"`
	RevertsExecutionID *int64     `json:"reverts_execution_id,omitempty"`
	SQLSnapshot        string     `json:"sql_snapshot"`
	ExecutionMode      string     `json:"execution_mode"`
	BatchColumn        string     `json:"batch_column,omitempty"`
	BatchSize          int        `json:"batch_size,omitempty"`
	CommittedBatches   int        `json:"committed_batches"`
	CommittedRows      int64      `json:"committed_rows"`
	LastCursor         string     `json:"last_cursor,omitempty"`
	RetryCount         int        `json:"retry_count"`
	ErrorMessage       string     `json:"error_message,omitempty"`
	TraceParent        string     `json:"trace_parent,omitempty"`
	RequestedBy        int64      `json:"requested_by"`
	CreatedAt          time.Time  `json:"created_at"`
	QueuedAt           *time.Time `json:"queued_at,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
	DurationMs         *int       `json:"duration_ms,omitempty"`
}

type migrationDTO struct {
	ID                 int64      `json:"id"`
	DatabaseID         int64      `json:"database_id"`
	Version            int        `json:"version"`
	Revision           int        `json:"revision"`
	Title              string     `json:"title"`
	SQL                string     `json:"sql"`
	SQLDown            string     `json:"sql_down,omitempty"`
	IsIrreversible     bool       `json:"is_irreversible"`
	IrreversibleReason string     `json:"irreversible_reason,omitempty"`
	ExecutionMode      string     `json:"execution_mode"`
	BatchColumn        string     `json:"batch_column,omitempty"`
	BatchSize          int        `json:"batch_size,omitempty"`
	IsImmutable        bool       `json:"is_immutable"`
	ActiveState        string     `json:"active_state"`
	ActiveExecutionID  *int64     `json:"active_execution_id,omitempty"`
	Status             string     `json:"status"`
	ErrorMessage       string     `json:"error_message,omitempty"`
	CreatedBy          int64      `json:"created_by"`
	CreatedByName      string     `json:"created_by_name"`
	CreatedAt          time.Time  `json:"created_at"`
	ExecutedBy         *int64     `json:"executed_by,omitempty"`
	ExecutedByName     *string    `json:"executed_by_name,omitempty"`
	ExecutedAt         *time.Time `json:"executed_at,omitempty"`
	DurationMs         *int       `json:"duration_ms,omitempty"`
}

type lockDTO struct {
	DatabaseID  int64     `json:"database_id"`
	JobID       int64     `json:"job_id"`
	WorkerID    string    `json:"worker_id"`
	LockState   string    `json:"lock_state"`
	AcquiredAt  time.Time `json:"acquired_at"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
}

func toJobDTO(j *migration.Job) jobDTO {
	return jobDTO{
		ID:                 j.ID,
		DatabaseID:         j.DatabaseID,
		MigrationID:        j.MigrationID,
		Action:             string(j.Action),
		Status:             string(j.Status),
		RevertsExecutionID: j.RevertsExecutionID,
		SQLSnapshot:        j.SQLSnapshot,
		ExecutionMode:      string(j.ExecutionMode),
		BatchColumn:        j.BatchColumn,
		BatchSize:          j.BatchSize,
		CommittedBatches:   j.CommittedBatches,
		CommittedRows:      j.CommittedRows,
		LastCursor:         j.LastCursor,
		RetryCount:         j.RetryCount,
		ErrorMessage:       j.ErrorMessage,
		TraceParent:        j.TraceParent,
		RequestedBy:        j.RequestedBy,
		CreatedAt:          j.CreatedAt,
		QueuedAt:           j.QueuedAt,
		StartedAt:          j.StartedAt,
		FinishedAt:         j.FinishedAt,
		DurationMs:         j.DurationMs,
	}
}

func toMigrationDTO(m *migration.Migration) migrationDTO {
	return migrationDTO{
		ID:                 m.ID,
		DatabaseID:         m.DatabaseID,
		Version:            m.Version,
		Revision:           m.Revision,
		Title:              m.Title,
		SQL:                m.SQL,
		SQLDown:            m.SQLDown,
		IsIrreversible:     m.IsIrreversible,
		IrreversibleReason: m.IrreversibleReason,
		ExecutionMode:      string(m.ExecutionMode),
		BatchColumn:        m.BatchColumn,
		BatchSize:          m.BatchSize,
		IsImmutable:        m.IsImmutable,
		ActiveState:        string(m.ActiveState),
		ActiveExecutionID:  m.ActiveExecutionID,
		Status:             string(m.Status),
		ErrorMessage:       m.ErrorMessage,
		CreatedBy:          m.CreatedBy,
		CreatedByName:      m.CreatedByName,
		CreatedAt:          m.CreatedAt,
		ExecutedBy:         m.ExecutedBy,
		ExecutedByName:     m.ExecutedByName,
		ExecutedAt:         m.ExecutedAt,
		DurationMs:         m.DurationMs,
	}
}

func toLockDTO(l *migration.DatabaseLock) *lockDTO {
	if l == nil {
		return nil
	}
	return &lockDTO{
		DatabaseID:  l.DatabaseID,
		JobID:       l.JobID,
		WorkerID:    l.WorkerID,
		LockState:   string(l.LockState),
		AcquiredAt:  l.AcquiredAt,
		HeartbeatAt: l.HeartbeatAt,
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	message := "Terjadi kesalahan pada server."

	switch {
	case errors.Is(err, apperr.ErrForbidden):
		code = http.StatusForbidden
		message = "Anda tidak memiliki izin untuk melakukan aksi ini pada database target."
	case errors.Is(err, apperr.ErrNotFound):
		code = http.StatusNotFound
		message = "Resource tidak ditemukan."
	case errors.Is(err, apperr.ErrInvalidInput):
		code = http.StatusBadRequest
		message = userMessage(err)
	case errors.Is(err, apperr.ErrNotExecutable) || errors.Is(err, migration.ErrDatabaseLocked):
		code = http.StatusConflict
		message = userMessage(err)
	case errors.Is(err, apperr.ErrConnectionFailed):
		code = http.StatusBadGateway
		message = "Tidak dapat terhubung ke database target."
	}

	writeJSON(w, code, map[string]any{
		"error": message,
		"code":  code,
	})
}

func requireJSONUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID <= 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "Unauthorized",
			"code":  http.StatusUnauthorized,
		})
		return 0, false
	}
	return userID, true
}

func parseJSONPathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := pathID(r, name)
	if err != nil {
		writeJSONError(w, err)
		return 0, false
	}
	return id, true
}

func (jh *JobAPIHandler) enqueueJob(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireJSONUserID(w, r)
	if !ok {
		return
	}
	dbID, ok := parseJSONPathID(w, r, "dbID")
	if !ok {
		return
	}
	jobID, ok := parseJSONPathID(w, r, "jobID")
	if !ok {
		return
	}

	traceParent := r.Header.Get("traceparent")
	job, err := jh.coordinator.EnqueueJob(r.Context(), userID, dbID, jobID, traceParent)
	if err != nil {
		writeJSONError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"job_id": job.ID,
		"status": string(job.Status),
		"job":    toJobDTO(job),
	})
}

func (jh *JobAPIHandler) getJobStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireJSONUserID(w, r)
	if !ok {
		return
	}
	dbID, ok := parseJSONPathID(w, r, "dbID")
	if !ok {
		return
	}
	jobID, ok := parseJSONPathID(w, r, "jobID")
	if !ok {
		return
	}

	job, err := jh.coordinator.GetJobStatus(r.Context(), userID, dbID, jobID)
	if err != nil {
		writeJSONError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"job": toJobDTO(job),
	})
}

func (jh *JobAPIHandler) retryBatch(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireJSONUserID(w, r)
	if !ok {
		return
	}
	dbID, ok := parseJSONPathID(w, r, "dbID")
	if !ok {
		return
	}
	migID, ok := parseJSONPathID(w, r, "migID")
	if !ok {
		return
	}

	job, err := jh.coordinator.RequestBatchRecovery(r.Context(), userID, dbID, migID, migration.JobActionRetryBatch)
	if err != nil {
		writeJSONError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"job_id": job.ID,
		"status": string(job.Status),
		"job":    toJobDTO(job),
	})
}

func (jh *JobAPIHandler) cancelBatch(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireJSONUserID(w, r)
	if !ok {
		return
	}
	dbID, ok := parseJSONPathID(w, r, "dbID")
	if !ok {
		return
	}
	migID, ok := parseJSONPathID(w, r, "migID")
	if !ok {
		return
	}

	job, err := jh.coordinator.RequestBatchRecovery(r.Context(), userID, dbID, migID, migration.JobActionCancelBatch)
	if err != nil {
		writeJSONError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"job_id": job.ID,
		"status": string(job.Status),
		"job":    toJobDTO(job),
	})
}

func (jh *JobAPIHandler) listMigrationsJSON(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireJSONUserID(w, r)
	if !ok {
		return
	}
	dbID, ok := parseJSONPathID(w, r, "dbID")
	if !ok {
		return
	}

	items, canEdit, lock, err := jh.coordinator.ListMigrationsM2(r.Context(), userID, dbID)
	if err != nil {
		writeJSONError(w, err)
		return
	}

	dtos := make([]migrationDTO, 0, len(items))
	for i := range items {
		dtos = append(dtos, toMigrationDTO(&items[i]))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"database_id": dbID,
		"can_edit":    canEdit,
		"active_lock": toLockDTO(lock),
		"migrations":  dtos,
	})
}

func (jh *JobAPIHandler) getMigrationDetailJSON(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireJSONUserID(w, r)
	if !ok {
		return
	}
	dbID, ok := parseJSONPathID(w, r, "dbID")
	if !ok {
		return
	}
	migID, ok := parseJSONPathID(w, r, "migID")
	if !ok {
		return
	}

	mig, jobs, canEdit, lock, err := jh.coordinator.GetMigrationDetailM2(r.Context(), userID, dbID, migID)
	if err != nil {
		writeJSONError(w, err)
		return
	}

	jobDTOs := make([]jobDTO, 0, len(jobs))
	for i := range jobs {
		jobDTOs = append(jobDTOs, toJobDTO(&jobs[i]))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"database_id": dbID,
		"can_edit":    canEdit,
		"active_lock": toLockDTO(lock),
		"migration":   toMigrationDTO(mig),
		"jobs":        jobDTOs,
	})
}

type migrationUpsertRequest struct {
	Title              string `json:"title"`
	SQL                string `json:"sql"`
	SQLDown            string `json:"sql_down"`
	IsIrreversible     bool   `json:"is_irreversible"`
	IrreversibleReason string `json:"irreversible_reason"`
	ExecutionMode      string `json:"execution_mode"`
	BatchColumn        string `json:"batch_column"`
	BatchSize          int    `json:"batch_size"`
}

func (jh *JobAPIHandler) createMigrationJSON(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireJSONUserID(w, r)
	if !ok {
		return
	}
	dbID, ok := parseJSONPathID(w, r, "dbID")
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var req migrationUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, apperr.ErrInvalidInput)
		return
	}

	mig, job, err := jh.coordinator.CreateMigrationM2(r.Context(), userID, dbID, migration.MigrationInputM2{
		Title:              req.Title,
		SQL:                req.SQL,
		SQLDown:            req.SQLDown,
		IsIrreversible:     req.IsIrreversible,
		IrreversibleReason: req.IrreversibleReason,
		ExecutionMode:      migration.ExecutionMode(req.ExecutionMode),
		BatchColumn:        req.BatchColumn,
		BatchSize:          req.BatchSize,
	})
	if err != nil {
		writeJSONError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"migration": toMigrationDTO(mig),
		"job":       toJobDTO(job),
	})
}

func (jh *JobAPIHandler) updateMigrationJSON(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireJSONUserID(w, r)
	if !ok {
		return
	}
	dbID, ok := parseJSONPathID(w, r, "dbID")
	if !ok {
		return
	}
	migID, ok := parseJSONPathID(w, r, "migID")
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var req migrationUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, apperr.ErrInvalidInput)
		return
	}

	mig, job, err := jh.coordinator.UpdateMigrationM2(r.Context(), userID, dbID, migID, migration.MigrationInputM2{
		Title:              req.Title,
		SQL:                req.SQL,
		SQLDown:            req.SQLDown,
		IsIrreversible:     req.IsIrreversible,
		IrreversibleReason: req.IrreversibleReason,
		ExecutionMode:      migration.ExecutionMode(req.ExecutionMode),
		BatchColumn:        req.BatchColumn,
		BatchSize:          req.BatchSize,
	})
	if err != nil {
		writeJSONError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"migration": toMigrationDTO(mig),
		"job":       toJobDTO(job),
	})
}
