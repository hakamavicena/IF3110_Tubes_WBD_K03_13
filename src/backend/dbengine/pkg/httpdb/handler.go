package httpdb

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"backend/dbengine/pkg/apperr"
	"backend/dbengine/pkg/dbviewer"
	"backend/dbengine/pkg/migration"
	"backend/dbengine/pkg/ports"
)

//go:embed templates/*.html
var templateFiles embed.FS

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
	templates  *template.Template
	csrfToken  func(*http.Request) string
}

func NewHandler(viewer *dbviewer.Service, migrations *migration.Service, authz ports.Authorizer, csrfToken func(*http.Request) string) (*Handler, error) {
	if viewer == nil || migrations == nil || authz == nil || csrfToken == nil {
		return nil, errors.New("httpdb dependencies must be configured")
	}
	tpl, err := template.New("").Funcs(template.FuncMap{
		"path": url.PathEscape,
		"add":  func(a, b int) int { return a + b },
		"sub":  func(a, b int) int { return a - b },
	}).ParseFS(templateFiles, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse httpdb templates: %w", err)
	}
	return &Handler{
		viewer: viewer, migrations: migrations, authz: authz,
		templates: tpl, csrfToken: csrfToken,
	}, nil
}

func (h *Handler) Register(mux *http.ServeMux, protect func(http.Handler) http.Handler) {
	if mux == nil || protect == nil {
		panic("httpdb requires a mux and authentication/CSRF middleware")
	}
	route := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, protect(handler))
	}
	route("GET /databases", h.listDatabases)
	route("GET /api/databases", h.apiListDatabases)
	route("GET /api/databases/{dbID}/schemas", h.apiListSchemas)
	route("GET /api/databases/{dbID}/schemas/{schema}/tables", h.apiListTables)
	route("GET /api/databases/{dbID}/schemas/{schema}/tables/{table}", h.apiTableDetail)
	route("GET /api/databases/{dbID}/schemas/{schema}/tables/{table}/data", h.apiTableData)
	route("GET /databases/{dbID}", h.listSchemas)
	route("GET /databases/{dbID}/schemas/{schema}/tables", h.listTables)
	route("GET /databases/{dbID}/schemas/{schema}/tables/{table}", h.tableDetail)
	route("GET /databases/{dbID}/schemas/{schema}/tables/{table}/data", h.tableData)
	route("GET /databases/{dbID}/migrations", h.listMigrations)
	route("GET /databases/{dbID}/migrations/new", h.newMigrationForm)
	route("POST /databases/{dbID}/migrations", h.createMigration)
	route("GET /databases/{dbID}/migrations/{migID}", h.showMigration)
	route("GET /databases/{dbID}/migrations/{migID}/edit", h.editMigrationForm)
	route("POST /databases/{dbID}/migrations/{migID}", h.updateMigration)
	route("POST /databases/{dbID}/migrations/{migID}/execute", h.executeMigration)
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: id tidak valid", apperr.ErrInvalidInput)
	}
	return id, nil
}

func (h *Handler) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return 0, false
	}
	return id, true
}

func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	if err := h.templates.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render database page failed", "template", name, "err", err)
	}
}

func userMessage(err error) string {
	_, message, ok := strings.Cut(err.Error(), ": ")
	if ok && message != "" {
		return message
	}
	return err.Error()
}

func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	code, message := http.StatusInternalServerError, "Terjadi kesalahan pada server."
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
		code, message = http.StatusBadGateway, "Tidak dapat terhubung ke database target. Coba lagi atau hubungi admin."
	default:
		slog.Error("unhandled database request error", "path", r.URL.Path, "err", err)
	}
	w.WriteHeader(code)
	h.render(w, "error.html", map[string]any{"Code": code, "Message": message})
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

func (h *Handler) parseID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := pathID(r, name)
	if err != nil {
		h.handleError(w, r, err)
		return 0, false
	}
	return id, true
}

func (h *Handler) listDatabases(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	databases, err := h.authz.AccessibleDatabases(r.Context(), userID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	h.render(w, "databases.html", map[string]any{"Databases": databases})
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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode JSON response failed", "err", err)
	}
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

func (h *Handler) listSchemas(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	dbID, ok := h.parseID(w, r, "dbID")
	if !ok {
		return
	}
	schemas, err := h.viewer.ListSchemas(r.Context(), userID, dbID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	h.render(w, "schemas.html", map[string]any{"DBID": dbID, "Schemas": schemas})
}

func (h *Handler) listTables(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	dbID, ok := h.parseID(w, r, "dbID")
	if !ok {
		return
	}
	schema := r.PathValue("schema")
	tables, err := h.viewer.ListTables(r.Context(), userID, dbID, schema)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	h.render(w, "tables.html", map[string]any{"DBID": dbID, "Schema": schema, "Tables": tables})
}

func (h *Handler) tableDetail(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	dbID, ok := h.parseID(w, r, "dbID")
	if !ok {
		return
	}
	schema, table := r.PathValue("schema"), r.PathValue("table")
	detail, err := h.viewer.GetTableDetail(r.Context(), userID, dbID, schema, table)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	h.render(w, "table_detail.html", map[string]any{"DBID": dbID, "Detail": detail})
}

func (h *Handler) tableData(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	dbID, ok := h.parseID(w, r, "dbID")
	if !ok {
		return
	}
	page := 1
	if value := r.URL.Query().Get("page"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			h.handleError(w, r, fmt.Errorf("%w: halaman tidak valid", apperr.ErrInvalidInput))
			return
		}
		page = parsed
	}
	schema, table := r.PathValue("schema"), r.PathValue("table")
	data, err := h.viewer.GetTableData(r.Context(), userID, dbID, schema, table, page, 0)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	h.render(w, "table_data.html", map[string]any{
		"DBID": dbID, "Schema": schema, "Table": table, "Data": data,
	})
}

func (h *Handler) listMigrations(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	dbID, ok := h.parseID(w, r, "dbID")
	if !ok {
		return
	}
	items, err := h.migrations.List(r.Context(), userID, dbID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	canEdit, err := h.authz.CanEdit(r.Context(), userID, dbID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	h.render(w, "migrations.html", map[string]any{"DBID": dbID, "Migrations": items, "CanEdit": canEdit})
}

func (h *Handler) newMigrationForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	dbID, ok := h.parseID(w, r, "dbID")
	if !ok {
		return
	}
	canEdit, err := h.authz.CanEdit(r.Context(), userID, dbID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	if !canEdit {
		h.handleError(w, r, apperr.ErrForbidden)
		return
	}
	h.render(w, "migration_form.html", map[string]any{"DBID": dbID, "CSRFToken": h.csrfToken(r)})
}

func (h *Handler) readForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := r.ParseForm(); err != nil {
		return fmt.Errorf("%w: form tidak valid atau terlalu besar", apperr.ErrInvalidInput)
	}
	return nil
}

func (h *Handler) createMigration(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	dbID, ok := h.parseID(w, r, "dbID")
	if !ok {
		return
	}
	if err := h.readForm(w, r); err != nil {
		h.handleError(w, r, err)
		return
	}
	title, sqlText := r.FormValue("title"), r.FormValue("sql")
	m, err := h.migrations.Create(r.Context(), userID, dbID, title, sqlText)
	if errors.Is(err, apperr.ErrInvalidInput) {
		w.WriteHeader(http.StatusBadRequest)
		h.render(w, "migration_form.html", map[string]any{
			"DBID": dbID, "Title": title, "SQL": sqlText,
			"Error": userMessage(err), "CSRFToken": h.csrfToken(r),
		})
		return
	}
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/databases/%d/migrations/%d", dbID, m.ID), http.StatusSeeOther)
}

func (h *Handler) showMigration(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	dbID, ok := h.parseID(w, r, "dbID")
	if !ok {
		return
	}
	migrationID, ok := h.parseID(w, r, "migID")
	if !ok {
		return
	}
	item, err := h.migrations.Get(r.Context(), userID, dbID, migrationID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	canEdit, err := h.authz.CanEdit(r.Context(), userID, dbID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	h.render(w, "migration_detail.html", map[string]any{
		"DBID": dbID, "Migration": item, "CanEdit": canEdit, "CSRFToken": h.csrfToken(r),
	})
}

func (h *Handler) editMigrationForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	dbID, ok := h.parseID(w, r, "dbID")
	if !ok {
		return
	}
	migrationID, ok := h.parseID(w, r, "migID")
	if !ok {
		return
	}
	item, err := h.migrations.Get(r.Context(), userID, dbID, migrationID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	canEdit, err := h.authz.CanEdit(r.Context(), userID, dbID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	if !canEdit {
		h.handleError(w, r, apperr.ErrForbidden)
		return
	}
	if item.Status != migration.StatusDraft && item.Status != migration.StatusFailed {
		h.handleError(w, r, apperr.ErrNotExecutable)
		return
	}
	h.render(w, "migration_form.html", map[string]any{
		"DBID": dbID, "Migration": item, "Title": item.Title, "SQL": item.SQL,
		"CSRFToken": h.csrfToken(r), "Editing": true,
	})
}

func (h *Handler) updateMigration(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	dbID, ok := h.parseID(w, r, "dbID")
	if !ok {
		return
	}
	migrationID, ok := h.parseID(w, r, "migID")
	if !ok {
		return
	}
	if err := h.readForm(w, r); err != nil {
		h.handleError(w, r, err)
		return
	}
	title, sqlText := r.FormValue("title"), r.FormValue("sql")
	err := h.migrations.Update(r.Context(), userID, dbID, migrationID, title, sqlText)
	if errors.Is(err, apperr.ErrInvalidInput) {
		w.WriteHeader(http.StatusBadRequest)
		h.render(w, "migration_form.html", map[string]any{
			"DBID": dbID, "Migration": &migration.Migration{ID: migrationID},
			"Title": title, "SQL": sqlText, "Error": userMessage(err),
			"CSRFToken": h.csrfToken(r), "Editing": true,
		})
		return
	}
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/databases/%d/migrations/%d", dbID, migrationID), http.StatusSeeOther)
}

func (h *Handler) executeMigration(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	dbID, ok := h.parseID(w, r, "dbID")
	if !ok {
		return
	}
	migrationID, ok := h.parseID(w, r, "migID")
	if !ok {
		return
	}
	if _, err := h.migrations.Execute(r.Context(), userID, dbID, migrationID); err != nil {
		h.handleError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/databases/%d/migrations/%d", dbID, migrationID), http.StatusSeeOther)
}
