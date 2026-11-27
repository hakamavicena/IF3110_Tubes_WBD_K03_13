package httpdb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/dbengine/pkg/apperr"
	"backend/dbengine/pkg/dbviewer"
	"backend/dbengine/pkg/migration"
	"backend/dbengine/pkg/ports"
)

type userIDContextKey struct{}

func WithUserID(r *http.Request, userID int64) *http.Request {
	return r.WithContext(withUserID(r, userID))
}

func withUserID(r *http.Request, userID int64) context.Context {
	return context.WithValue(r.Context(), userIDContextKey{}, userID)
}

func UserIDFromContext(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(userIDContextKey{}).(int64)
	return userID, ok && userID > 0
}

type Handler struct {
	viewer     *dbviewer.Service
	migrations *migration.Service
	authz      ports.Authorizer
}

func NewHandler(viewer *dbviewer.Service, migrations *migration.Service, authz ports.Authorizer, csrfToken func(*http.Request) string) (*Handler, error) {
	if viewer == nil || migrations == nil || authz == nil || csrfToken == nil {
		return nil, errors.New("httpdb dependencies must be configured")
	}
	return &Handler{viewer: viewer, migrations: migrations, authz: authz}, nil
}

func (h *Handler) Register(mux *http.ServeMux, protect func(http.Handler) http.Handler) {
	if mux == nil || protect == nil {
		panic("httpdb requires a mux and authentication/CSRF middleware")
	}
	route := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, protect(handler))
	}
	route("GET /api/databases", h.apiListDatabases)
	route("GET /api/databases/{dbID}/schemas", h.apiListSchemas)
	route("GET /api/databases/{dbID}/schemas/{schema}/tables", h.apiListTables)
	route("GET /api/databases/{dbID}/schemas/{schema}/tables/{table}", h.apiTableDetail)
	route("GET /api/databases/{dbID}/schemas/{schema}/tables/{table}/data", h.apiTableData)
	route("GET /api/databases/{dbID}/history", h.apiMigrationHistory)
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: id tidak valid", apperr.ErrInvalidInput)
	}
	return id, nil
}

func userMessage(err error) string {
	_, message, ok := strings.Cut(err.Error(), ": ")
	if ok && message != "" {
		return message
	}
	return err.Error()
}

func (h *Handler) handleAPIError(w http.ResponseWriter, r *http.Request, err error) {
	code, message := http.StatusInternalServerError, "internal server error"

	switch {
	case errors.Is(err, apperr.ErrForbidden):
		code, message = http.StatusForbidden, "Anda tidak memiliki akses ke database ini."
	case errors.Is(err, apperr.ErrNotFound):
		code, message = http.StatusNotFound, "Data tidak ditemukan."
	case errors.Is(err, apperr.ErrInvalidInput):
		code, message = http.StatusBadRequest, userMessage(err)
	case errors.Is(err, apperr.ErrNotExecutable):
		code, message = http.StatusConflict, userMessage(err)
	case errors.Is(err, apperr.ErrConnectionFailed):
		code, message = http.StatusBadGateway, "Tidak dapat terhubung ke database target."
	default:
		slog.Error("unhandled database API error", "path", r.URL.Path, "err", err)
	}

	writeJSON(w, code, apiErrorResponse{Error: message})
}

type apiErrorResponse struct {
	Error string `json:"error"`
}

type databaseAccessDTO struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Access string `json:"access"`
}

type databaseListResponse struct {
	Databases []databaseAccessDTO `json:"databases"`
}

type schemaListResponse struct {
	Schemas []string `json:"schemas"`
}

type tableInfoDTO struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Type   string `json:"type"`
}

type tableListResponse struct {
	Tables []tableInfoDTO `json:"tables"`
}

type columnInfoDTO struct {
	Position int     `json:"position"`
	Name     string  `json:"name"`
	DataType string  `json:"data_type"`
	Nullable bool    `json:"nullable"`
	Default  *string `json:"default"`
}

type constraintInfoDTO struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Definition string `json:"definition"`
}

type indexInfoDTO struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	Unique     bool   `json:"unique"`
}

type tableDetailResponse struct {
	Table       tableInfoDTO        `json:"table"`
	Columns     []columnInfoDTO     `json:"columns"`
	Constraints []constraintInfoDTO `json:"constraints"`
	Indexes     []indexInfoDTO      `json:"indexes"`
}

type tableDataResponse struct {
	Columns  []string    `json:"columns"`
	Rows     [][]*string `json:"rows"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
	Total    int64       `json:"total"`
	Pages    int         `json:"pages"`
}

type migrationExecutionDTO struct {
	ExecutionID         int64                     `json:"execution_id"`
	DatabaseID          int64                     `json:"database_id"`
	MigrationID         int64                     `json:"migration_id"`
	JobID               *int64                    `json:"job_id"`
	Version             int                       `json:"version"`
	Revision            int                       `json:"revision"`
	Title               string                    `json:"title"`
	Action              migration.ExecutionAction `json:"action"`
	SQLExecuted         string                    `json:"sql_executed"`
	ExecutedBy          int64                     `json:"executed_by"`
	ExecutedByName      string                    `json:"executed_by_name"`
	ExecutedAt          time.Time                 `json:"executed_at"`
	DurationMs          int                       `json:"duration_ms"`
	RowsAffected        int64                     `json:"rows_affected"`
	Status              migration.Status          `json:"status"`
	RevertedExecutionID *int64                    `json:"reverted_execution_id"`
}

type migrationHistoryResponse struct {
	History []migrationExecutionDTO `json:"history"`
}

func (h *Handler) apiMigrationHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiErrorResponse{Error: "unauthorized"})
		return
	}

	dbID, err := pathID(r, "dbID")
	if err != nil {
		h.handleAPIError(w, r, err)
		return
	}

	hideRollback := false
	if value, present := r.URL.Query()["hide_rollback"]; present {
		if len(value) != 1 {
			h.handleAPIError(w, r, fmt.Errorf(
				"%w: parameter hide_rollback tidak valid",
				apperr.ErrInvalidInput,
			))
			return
		}
		hideRollback, err = strconv.ParseBool(value[0])
		if err != nil {
			h.handleAPIError(w, r, fmt.Errorf(
				"%w: parameter hide_rollback tidak valid",
				apperr.ErrInvalidInput,
			))
			return
		}
	}

	executions, err := h.migrations.History(r.Context(), userID, dbID, hideRollback)
	if err != nil {
		h.handleAPIError(w, r, err)
		return
	}

	history := make([]migrationExecutionDTO, 0, len(executions))
	for _, execution := range executions {
		history = append(history, migrationExecutionDTO{
			ExecutionID:         execution.ExecutionID,
			DatabaseID:          execution.DatabaseID,
			MigrationID:         execution.MigrationID,
			JobID:               execution.JobID,
			Version:             execution.Version,
			Revision:            execution.Revision,
			Title:               execution.Title,
			Action:              execution.Action,
			SQLExecuted:         execution.SQLExecuted,
			ExecutedBy:          execution.ExecutedBy,
			ExecutedByName:      execution.ExecutedByName,
			ExecutedAt:          execution.ExecutedAt,
			DurationMs:          execution.DurationMs,
			RowsAffected:        execution.RowsAffected,
			Status:              execution.Status,
			RevertedExecutionID: execution.RevertedExecutionID,
		})
	}
	writeJSON(w, http.StatusOK, migrationHistoryResponse{History: history})
}

func (h *Handler) apiListDatabases(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiErrorResponse{Error: "unauthorized"})
		return
	}

	databases, err := h.authz.AccessibleDatabases(r.Context(), userID)
	if err != nil {
		slog.Error("list accessible databases failed", "user_id", userID, "err", err)
		writeJSON(w, http.StatusInternalServerError, apiErrorResponse{Error: "internal server error"})
		return
	}

	items := make([]databaseAccessDTO, 0, len(databases))
	for _, database := range databases {
		var access string
		switch database.Level {
		case ports.LevelView:
			access = "view"
		case ports.LevelEdit:
			access = "edit"
		default:
			slog.Error(
				"unexpected database access level",
				"database_id", database.DatabaseID,
				"level", database.Level,
			)
			writeJSON(w, http.StatusInternalServerError, apiErrorResponse{Error: "internal server error"})
			return
		}

		items = append(items, databaseAccessDTO{
			ID:     database.DatabaseID,
			Name:   database.Name,
			Access: access,
		})
	}

	writeJSON(w, http.StatusOK, databaseListResponse{Databases: items})
}

func (h *Handler) apiListSchemas(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiErrorResponse{Error: "unauthorized"})
		return
	}

	dbID, err := pathID(r, "dbID")
	if err != nil {
		h.handleAPIError(w, r, err)
		return
	}

	schemas, err := h.viewer.ListSchemas(r.Context(), userID, dbID)
	if err != nil {
		h.handleAPIError(w, r, err)
		return
	}
	if schemas == nil {
		schemas = []string{}
	}

	writeJSON(w, http.StatusOK, schemaListResponse{Schemas: schemas})
}

func (h *Handler) apiListTables(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiErrorResponse{Error: "unauthorized"})
		return
	}

	dbID, err := pathID(r, "dbID")
	if err != nil {
		h.handleAPIError(w, r, err)
		return
	}

	schema := r.PathValue("schema")
	tables, err := h.viewer.ListTables(r.Context(), userID, dbID, schema)
	if err != nil {
		h.handleAPIError(w, r, err)
		return
	}

	items := make([]tableInfoDTO, 0, len(tables))
	for _, table := range tables {
		items = append(items, tableInfoDTO{
			Schema: table.Schema,
			Name:   table.Name,
			Type:   table.Type,
		})
	}

	writeJSON(w, http.StatusOK, tableListResponse{Tables: items})
}

func (h *Handler) apiTableDetail(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiErrorResponse{Error: "unauthorized"})
		return
	}

	dbID, err := pathID(r, "dbID")
	if err != nil {
		h.handleAPIError(w, r, err)
		return
	}

	detail, err := h.viewer.GetTableDetail(
		r.Context(),
		userID,
		dbID,
		r.PathValue("schema"),
		r.PathValue("table"),
	)
	if err != nil {
		h.handleAPIError(w, r, err)
		return
	}

	response := tableDetailResponse{
		Table: tableInfoDTO{
			Schema: detail.Table.Schema,
			Name:   detail.Table.Name,
			Type:   detail.Table.Type,
		},
		Columns:     make([]columnInfoDTO, 0, len(detail.Columns)),
		Constraints: make([]constraintInfoDTO, 0, len(detail.Constraints)),
		Indexes:     make([]indexInfoDTO, 0, len(detail.Indexes)),
	}

	for _, column := range detail.Columns {
		response.Columns = append(response.Columns, columnInfoDTO{
			Position: column.Position,
			Name:     column.Name,
			DataType: column.DataType,
			Nullable: column.Nullable,
			Default:  column.Default,
		})
	}
	for _, constraint := range detail.Constraints {
		response.Constraints = append(response.Constraints, constraintInfoDTO{
			Name:       constraint.Name,
			Type:       constraint.Type,
			Definition: constraint.Definition,
		})
	}
	for _, index := range detail.Indexes {
		response.Indexes = append(response.Indexes, indexInfoDTO{
			Name:       index.Name,
			Definition: index.Definition,
			Unique:     index.Unique,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) apiTableData(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiErrorResponse{Error: "unauthorized"})
		return
	}

	dbID, err := pathID(r, "dbID")
	if err != nil {
		h.handleAPIError(w, r, err)
		return
	}

	page := 1
	if value := r.URL.Query().Get("page"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			h.handleAPIError(w, r, fmt.Errorf("%w: halaman tidak valid", apperr.ErrInvalidInput))
			return
		}
		page = parsed
	}

	size := 0
	if value := r.URL.Query().Get("size"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			h.handleAPIError(w, r, fmt.Errorf("%w: ukuran halaman tidak valid", apperr.ErrInvalidInput))
			return
		}
		size = parsed
	}

	data, err := h.viewer.GetTableData(
		r.Context(),
		userID,
		dbID,
		r.PathValue("schema"),
		r.PathValue("table"),
		page,
		size,
	)
	if err != nil {
		h.handleAPIError(w, r, err)
		return
	}

	rows := data.Rows
	if rows == nil {
		rows = [][]*string{}
	}

	writeJSON(w, http.StatusOK, tableDataResponse{
		Columns:  data.Columns,
		Rows:     rows,
		Page:     data.Page,
		PageSize: data.PageSize,
		Total:    data.Total,
		Pages:    data.Pages,
	})
}
