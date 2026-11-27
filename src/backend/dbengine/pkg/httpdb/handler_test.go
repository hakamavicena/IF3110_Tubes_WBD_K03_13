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

func TestRegisterDoesNotExposeLegacyOrSynchronousRoutes(t *testing.T) {
	_, mux := newTestHandler(t, ports.DenyAll{})

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/databases"},
		{http.MethodPost, "/databases/1/migrations/1/execute"},
		{http.MethodPost, "/databases/1/migrations"},
	} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
		if recorder.Code != http.StatusNotFound && recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s status = %d, want 404/405", tc.method, tc.path, recorder.Code)
		}
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

type migrationHistoryTestAuthorizer struct {
	ports.DenyAll
	canView bool
}

func (a migrationHistoryTestAuthorizer) CanView(
	context.Context,
	int64,
	int64,
) (bool, error) {
	return a.canView, nil
}

type migrationHistoryTestRepository struct {
	migration.Repository
	executions    []migration.Execution
	requestedDBID int64
}

func (r *migrationHistoryTestRepository) ListHistory(
	_ context.Context,
	databaseID int64,
) ([]migration.Execution, error) {
	r.requestedDBID = databaseID
	return r.executions, nil
}

func newMigrationHistoryTestMux(
	t *testing.T,
	repo *migrationHistoryTestRepository,
	authz ports.Authorizer,
) *http.ServeMux {
	t.Helper()

	viewer := dbviewer.NewService(authz, nil)
	migrations := migration.NewService(repo, authz, nil, nil, time.Second)
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
	return mux
}

func TestAPIListMigrationHistoryAndHideRollback(t *testing.T) {
	revertedID := int64(2)
	repo := &migrationHistoryTestRepository{
		executions: []migration.Execution{
			{
				ExecutionID: 1, DatabaseID: 10, MigrationID: 4,
				Version: 1, Revision: 1, Title: "first",
				Action: migration.ActionApply, SQLExecuted: "CREATE TABLE firsts(id int)",
				ExecutedBy: 7, ExecutedByName: "alice",
				Status: migration.StatusSuccess,
			},
			{
				ExecutionID: 2, DatabaseID: 10, MigrationID: 5,
				Version: 2, Revision: 1, Title: "second",
				Action: migration.ActionApply, SQLExecuted: "CREATE TABLE seconds(id int)",
				ExecutedBy: 7, ExecutedByName: "alice",
				Status: migration.StatusSuccess,
			},
			{
				ExecutionID: 3, DatabaseID: 10, MigrationID: 5,
				Version: 2, Revision: 1, Title: "second",
				Action: migration.ActionRollback, SQLExecuted: "DROP TABLE seconds",
				ExecutedBy: 7, ExecutedByName: "alice",
				Status: migration.StatusSuccess, RevertedExecutionID: &revertedID,
			},
			{
				ExecutionID: 4, DatabaseID: 10, MigrationID: 5,
				Version: 2, Revision: 1, Title: "second",
				Action: migration.ActionForward, SQLExecuted: "CREATE TABLE seconds(id int)",
				ExecutedBy: 7, ExecutedByName: "alice",
				Status: migration.StatusSuccess, RevertedExecutionID: ptrHistoryInt64(3),
			},
			{
				ExecutionID: 5, DatabaseID: 10, MigrationID: 6,
				Version: 3, Revision: 1, Title: "third",
				Action: migration.ActionApply, SQLExecuted: "invalid SQL",
				ExecutedBy: 7, ExecutedByName: "alice",
				Status: migration.StatusFailed,
			},
		},
	}
	mux := newMigrationHistoryTestMux(
		t,
		repo,
		migrationHistoryTestAuthorizer{canView: true},
	)

	tests := []struct {
		name    string
		query   string
		wantIDs []int64
	}{
		{name: "all executions", wantIDs: []int64{1, 2, 3, 4, 5}},
		{name: "hide successful rollback pair", query: "?hide_rollback=true", wantIDs: []int64{1, 4, 5}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				"/api/databases/10/history"+test.query,
				nil,
			)
			request = WithUserID(request, 42)
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("GET migration history status = %d, want %d; body: %s",
					recorder.Code, http.StatusOK, recorder.Body.String())
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			var response migrationHistoryResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode history response: %v", err)
			}
			if len(response.History) != len(test.wantIDs) {
				t.Fatalf("history has %d rows, want %d: %+v",
					len(response.History), len(test.wantIDs), response.History)
			}
			for i, wantID := range test.wantIDs {
				if response.History[i].ExecutionID != wantID {
					t.Errorf("history[%d].execution_id = %d, want %d",
						i, response.History[i].ExecutionID, wantID)
				}
			}
			if len(response.History) > 0 &&
				(response.History[0].SQLExecuted == "" ||
					response.History[0].ExecutedByName != "alice") {
				t.Errorf("history is missing SQL snapshot or actor: %+v", response.History[0])
			}
		})
	}
	if repo.requestedDBID != 10 {
		t.Errorf("repository database ID = %d, want 10", repo.requestedDBID)
	}
}

func TestAPIListMigrationHistoryRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		withUser   bool
		canView    bool
		wantStatus int
	}{
		{
			name:       "missing user",
			path:       "/api/databases/10/history",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid database ID",
			path:       "/api/databases/not-an-id/history",
			withUser:   true,
			canView:    true,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid hide rollback value",
			path:       "/api/databases/10/history?hide_rollback=maybe",
			withUser:   true,
			canView:    true,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "duplicate hide rollback values",
			path:       "/api/databases/10/history?hide_rollback=true&hide_rollback=false",
			withUser:   true,
			canView:    true,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "user lacks view access",
			path:       "/api/databases/10/history",
			withUser:   true,
			wantStatus: http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &migrationHistoryTestRepository{}
			mux := newMigrationHistoryTestMux(
				t,
				repo,
				migrationHistoryTestAuthorizer{canView: test.canView},
			)
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.withUser {
				request = WithUserID(request, 42)
			}
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("GET %s status = %d, want %d; body: %s",
					test.path, recorder.Code, test.wantStatus, recorder.Body.String())
			}
			var response apiErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if response.Error == "" {
				t.Fatal("expected a JSON error message")
			}
		})
	}
}

func ptrHistoryInt64(value int64) *int64 {
	return &value
}
