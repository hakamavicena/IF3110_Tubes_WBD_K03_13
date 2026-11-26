package httpdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"backend/dbengine/pkg/dbviewer"
	"backend/dbengine/pkg/migration"
	"backend/dbengine/pkg/ports"
)

type databaseListAuthorizer struct {
	ports.DenyAll
	databases []ports.DatabaseAccess
}

func (a databaseListAuthorizer) AccessibleDatabases(
	_ context.Context,
	_ int64,
) ([]ports.DatabaseAccess, error) {
	return a.databases, nil
}

func TestRegisterParsesTemplatesAndRejectsMissingUser(t *testing.T) {
	authz := ports.DenyAll{}
	viewer := dbviewer.NewService(authz, nil)
	migrations := migration.NewService(nil, authz, nil, nil, time.Second)
	handler, err := NewHandler(viewer, migrations, authz, func(*http.Request) string { return "csrf" })
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Register(mux, func(next http.Handler) http.Handler { return next })

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/databases", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("GET /databases status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func newTestHandler(t *testing.T, authz ports.Authorizer) (*Handler, *http.ServeMux) {
	t.Helper()

	viewer := dbviewer.NewService(authz, nil)
	migrations := migration.NewService(nil, authz, nil, nil, time.Second)
	handler, err := NewHandler(viewer, migrations, authz, func(*http.Request) string {
		return "csrf"
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handler.Register(mux, func(next http.Handler) http.Handler {
		return next
	})
	return handler, mux
}

func TestRegisterRejectsMissingUser(t *testing.T) {
	_, mux := newTestHandler(t, ports.DenyAll{})

	tests := []struct {
		path        string
		contentType string
	}{
		{path: "/databases"},
		{path: "/api/databases", contentType: "application/json"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)

			mux.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("GET %s status = %d, want %d",
					test.path, recorder.Code, http.StatusUnauthorized)
			}
			if test.contentType != "" &&
				recorder.Header().Get("Content-Type") != test.contentType {
				t.Fatalf("Content-Type = %q, want %q",
					recorder.Header().Get("Content-Type"), test.contentType)
			}
		})
	}
}

func TestAPIListDatabasesReturnsAccessLevels(t *testing.T) {
	authz := databaseListAuthorizer{
		databases: []ports.DatabaseAccess{
			{DatabaseID: 1, Name: "view_db", Level: ports.LevelView},
			{DatabaseID: 2, Name: "edit_db", Level: ports.LevelEdit},
		},
	}
	_, mux := newTestHandler(t, authz)

	request := httptest.NewRequest(http.MethodGet, "/api/databases", nil)
	request = WithUserID(request, 42)
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/databases status = %d, want %d",
			recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/json")
	}

	var got databaseListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response JSON: %v", err)
	}

	want := []databaseAccessDTO{
		{ID: 1, Name: "view_db", Access: "view"},
		{ID: 2, Name: "edit_db", Access: "edit"},
	}
	if !reflect.DeepEqual(got.Databases, want) {
		t.Fatalf("databases = %+v, want %+v", got.Databases, want)
	}
}

func TestAPIListSchemasValidatesIDAndAccess(t *testing.T) {
	_, mux := newTestHandler(t, ports.DenyAll{})

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "invalid database ID",
			path:       "/api/databases/not-an-id/schemas",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "user without database access",
			path:       "/api/databases/1/schemas",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request = WithUserID(request, 42)
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("GET %s status = %d, want %d",
					test.path, recorder.Code, test.wantStatus)
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want %q", got, "application/json")
			}

			var response apiErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response JSON: %v", err)
			}
			if response.Error == "" {
				t.Fatal("expected a JSON error message")
			}
		})
	}
}

func TestAPIListTablesValidatesIDAndAccess(t *testing.T) {
	_, mux := newTestHandler(t, ports.DenyAll{})

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "invalid database ID",
			path:       "/api/databases/not-an-id/schemas/public/tables",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "user without database access",
			path:       "/api/databases/1/schemas/public/tables",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request = WithUserID(request, 42)
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("GET %s status = %d, want %d",
					test.path, recorder.Code, test.wantStatus)
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want %q", got, "application/json")
			}

			var response apiErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response JSON: %v", err)
			}
			if response.Error == "" {
				t.Fatal("expected a JSON error message")
			}
		})
	}
}

func TestAPITableDetailValidatesIDAndAccess(t *testing.T) {
	_, mux := newTestHandler(t, ports.DenyAll{})

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "invalid database ID",
			path:       "/api/databases/not-an-id/schemas/public/tables/orders",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "user without database access",
			path:       "/api/databases/1/schemas/public/tables/orders",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request = WithUserID(request, 42)
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("GET %s status = %d, want %d",
					test.path, recorder.Code, test.wantStatus)
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want %q", got, "application/json")
			}

			var response apiErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response JSON: %v", err)
			}
			if response.Error == "" {
				t.Fatal("expected a JSON error message")
			}
		})
	}
}

func TestAPITableDataValidatesPaginationAndAccess(t *testing.T) {
	_, mux := newTestHandler(t, ports.DenyAll{})

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "invalid database ID",
			path:       "/api/databases/not-an-id/schemas/public/tables/orders/data",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid page",
			path:       "/api/databases/1/schemas/public/tables/orders/data?page=0",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid page size",
			path:       "/api/databases/1/schemas/public/tables/orders/data?size=abc",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "user without database access",
			path:       "/api/databases/1/schemas/public/tables/orders/data?page=1&size=25",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request = WithUserID(request, 42)
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("GET %s status = %d, want %d",
					test.path, recorder.Code, test.wantStatus)
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want %q", got, "application/json")
			}

			var response apiErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response JSON: %v", err)
			}
			if response.Error == "" {
				t.Fatal("expected a JSON error message")
			}
		})
	}
}
