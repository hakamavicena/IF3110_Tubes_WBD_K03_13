package httpdb

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/hakamavicena/if3110-dbengine/pkg/apperr"
	"github.com/hakamavicena/if3110-dbengine/pkg/dbviewer"
	"github.com/hakamavicena/if3110-dbengine/pkg/migration"
	"github.com/hakamavicena/if3110-dbengine/pkg/ports"
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
