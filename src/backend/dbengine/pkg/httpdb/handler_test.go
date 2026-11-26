package httpdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"backend/dbengine/pkg/dbviewer"
	"backend/dbengine/pkg/migration"
	"backend/dbengine/pkg/ports"

	"github.com/jackc/pgx/v5/pgxpool"
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

type migrationCreateAuthorizer struct {
	ports.DenyAll
}

func (migrationCreateAuthorizer) CanEdit(context.Context, int64, int64) (bool, error) {
	return true, nil
}

type migrationCreateRepository struct {
	created *migration.Migration
}

func (r *migrationCreateRepository) Create(_ context.Context, item *migration.Migration) (*migration.Migration, error) {
	created := *item
	created.ID = 12
	created.Version = 1
	created.Status = migration.StatusDraft
	r.created = &created
	return &created, nil
}

func (*migrationCreateRepository) Get(context.Context, int64) (*migration.Migration, error) {
	panic("unexpected Get")
}

func (*migrationCreateRepository) List(context.Context, int64) ([]migration.Migration, error) {
	panic("unexpected List")
}

func (*migrationCreateRepository) RecordExecution(
	context.Context,
	*migration.Execution,
) (*migration.Execution, error) {
	panic("unexpected RecordExecution")
}

func (*migrationCreateRepository) ListHistory(
	context.Context,
	int64,
) ([]migration.Execution, error) {
	panic("unexpected ListHistory")
}

func (*migrationCreateRepository) UpdateDraft(
	context.Context,
	int64,
	int64,
	string,
	string,
	string,
	bool,
	string,
) error {
	panic("unexpected UpdateDraft")
}

func (*migrationCreateRepository) ClaimRollback(ctx context.Context, id, databaseID int64) (sqlDown string, revertedExecutionID int64, ok bool, err error) {
	panic("unexpected ClaimRollback")
}

func (*migrationCreateRepository) ClaimForward(
	ctx context.Context,
	id, databaseID int64,
) (string, int64, bool, error) {
	panic("unexpected ClaimForward")
}

func (*migrationCreateRepository) Claim(context.Context, int64, int64) (string, bool, error) {
	panic("unexpected Claim")
}

func (*migrationCreateRepository) Finish(context.Context, int64, migration.Status, string, int) error {
	panic("unexpected Finish")
}

func (*migrationCreateRepository) RecoverStuck(context.Context) (int64, error) {
	panic("unexpected RecoverStuck")
}

func newMigrationCreateTestMux(
	t *testing.T,
	repo migration.Repository,
) *http.ServeMux {
	t.Helper()

	authz := migrationCreateAuthorizer{}
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

func TestAPICreateMigrationValidatesAndPersistsDefinition(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantStatus  int
		wantSQLDown string
	}{
		{
			name: "reversible definition is persisted",
			body: `{
                "title": "create items",
                "sql": "CREATE TABLE items(id int);",
                "sql_down": "DROP TABLE items;"
            }`,
			wantStatus:  http.StatusCreated,
			wantSQLDown: "DROP TABLE items;",
		},
		{
			name: "irreversible definition requires a reason",
			body: `{
                "title": "delete old data",
                "sql": "DELETE FROM items;",
                "is_irreversible": true
            }`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &migrationCreateRepository{}
			mux := newMigrationCreateTestMux(t, repo)

			request := httptest.NewRequest(
				http.MethodPost,
				"/api/databases/1/migrations",
				strings.NewReader(test.body),
			)
			request.Header.Set("Content-Type", "application/json")
			request = WithUserID(request, 42)
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("POST /api/databases/1/migrations status = %d, want %d; body: %s",
					recorder.Code, test.wantStatus, recorder.Body.String())
			}

			if test.wantStatus == http.StatusCreated {
				if got := recorder.Header().Get("Content-Type"); got != "application/json" {
					t.Fatalf("Content-Type = %q, want application/json", got)
				}
				if repo.created == nil || repo.created.SQLDown != test.wantSQLDown {
					t.Fatalf("stored migration = %+v, want SQLDown %q",
						repo.created, test.wantSQLDown)
				}

				var response createMigrationResponse
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode response JSON: %v", err)
				}
				if response.Migration.SQLDown != test.wantSQLDown {
					t.Fatalf("response SQLDown = %q, want %q",
						response.Migration.SQLDown, test.wantSQLDown)
				}
			} else if repo.created != nil {
				t.Fatal("invalid migration definition was unexpectedly persisted")
			}
		})
	}
}

type migrationActionTestAuthorizer struct {
	ports.DenyAll
	canEdit bool
}

func (a migrationActionTestAuthorizer) CanEdit(
	context.Context,
	int64,
	int64,
) (bool, error) {
	return a.canEdit, nil
}

type migrationActionTestRepository struct {
	migration.Repository

	item migration.Migration

	rollbackSQL         string
	rollbackExecutionID int64
	rollbackClaimed     bool

	forwardSQL         string
	forwardExecutionID int64
	forwardClaimed     bool

	execution *migration.Execution
	finished  bool
}

func (r *migrationActionTestRepository) Get(
	context.Context,
	int64,
) (*migration.Migration, error) {
	item := r.item
	return &item, nil
}

func (r *migrationActionTestRepository) ClaimRollback(
	context.Context,
	int64,
	int64,
) (string, int64, bool, error) {
	return r.rollbackSQL, r.rollbackExecutionID, r.rollbackClaimed, nil
}

func (r *migrationActionTestRepository) ClaimForward(
	context.Context,
	int64,
	int64,
) (string, int64, bool, error) {
	return r.forwardSQL, r.forwardExecutionID, r.forwardClaimed, nil
}

func (r *migrationActionTestRepository) Finish(
	_ context.Context,
	_ int64,
	status migration.Status,
	message string,
	durationMs int,
) error {
	r.finished = true
	r.item.Status = status
	r.item.ErrorMessage = message
	r.item.DurationMs = &durationMs
	return nil
}

func (r *migrationActionTestRepository) RecordExecution(
	_ context.Context,
	execution *migration.Execution,
) (*migration.Execution, error) {
	recorded := *execution
	r.execution = &recorded
	return &recorded, nil
}

type migrationActionTestPools struct{}

func (migrationActionTestPools) Pool(
	context.Context,
	int64,
) (*pgxpool.Pool, error) {
	return nil, nil
}

type migrationActionTestRunner struct{}

func (migrationActionTestRunner) Run(
	context.Context,
	*pgxpool.Pool,
	string,
) error {
	return nil
}

func newMigrationActionTestMux(
	t *testing.T,
	repo *migrationActionTestRepository,
	authz ports.Authorizer,
) *http.ServeMux {
	t.Helper()

	viewer := dbviewer.NewService(authz, nil)
	migrations := migration.NewService(
		repo,
		authz,
		migrationActionTestPools{},
		migrationActionTestRunner{},
		time.Second,
	)
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

func TestAPIMigrationActionsSucceed(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		action      migration.ExecutionAction
		sql         string
		executionID int64
	}{
		{
			name:        "rollback",
			path:        "/api/databases/10/migrations/5/rollback",
			action:      migration.ActionRollback,
			sql:         "DROP TABLE items",
			executionID: 41,
		},
		{
			name:        "forward",
			path:        "/api/databases/10/migrations/5/forward",
			action:      migration.ActionForward,
			sql:         "CREATE TABLE items (id int)",
			executionID: 42,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &migrationActionTestRepository{
				item: migration.Migration{
					ID:         5,
					DatabaseID: 10,
					Version:    2,
					Title:      "second migration",
					Status:     migration.StatusSuccess,
				},
			}

			if test.action == migration.ActionRollback {
				repo.rollbackSQL = test.sql
				repo.rollbackExecutionID = test.executionID
				repo.rollbackClaimed = true
			} else {
				repo.forwardSQL = test.sql
				repo.forwardExecutionID = test.executionID
				repo.forwardClaimed = true
			}

			mux := newMigrationActionTestMux(
				t,
				repo,
				migrationActionTestAuthorizer{canEdit: true},
			)
			request := httptest.NewRequest(http.MethodPost, test.path, nil)
			request = WithUserID(request, 42)
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("POST %s status = %d, want %d; body: %s",
					test.path, recorder.Code, http.StatusOK, recorder.Body.String())
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			if !repo.finished {
				t.Fatal("migration action was not finalized")
			}
			if repo.execution == nil {
				t.Fatal("migration execution was not recorded")
			}
			if repo.execution.Action != test.action {
				t.Errorf("execution action = %q, want %q",
					repo.execution.Action, test.action)
			}
			if repo.execution.SQLExecuted != test.sql {
				t.Errorf("executed SQL = %q, want %q",
					repo.execution.SQLExecuted, test.sql)
			}
			if repo.execution.RevertedExecutionID == nil ||
				*repo.execution.RevertedExecutionID != test.executionID {
				t.Errorf("reverted execution ID = %v, want %d",
					repo.execution.RevertedExecutionID, test.executionID)
			}

			var response migrationActionResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response JSON: %v", err)
			}
			if response.Migration.ID != 5 {
				t.Errorf("response migration ID = %d, want 5", response.Migration.ID)
			}
			if response.Migration.Status != migration.StatusSuccess {
				t.Errorf("response status = %q, want %q",
					response.Migration.Status, migration.StatusSuccess)
			}
		})
	}
}

func TestAPIMigrationActionsRejectInvalidRequests(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		withUser   bool
		canEdit    bool
		claim      bool
		wantStatus int
	}{
		{
			name:       "missing user",
			path:       "/api/databases/10/migrations/5/rollback",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid database ID",
			path:       "/api/databases/not-an-id/migrations/5/rollback",
			withUser:   true,
			canEdit:    true,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid migration ID",
			path:       "/api/databases/10/migrations/not-an-id/forward",
			withUser:   true,
			canEdit:    true,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "user cannot edit database",
			path:       "/api/databases/10/migrations/5/rollback",
			withUser:   true,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "rollback claim rejected",
			path:       "/api/databases/10/migrations/5/rollback",
			withUser:   true,
			canEdit:    true,
			claim:      false,
			wantStatus: http.StatusConflict,
		},
		{
			name:       "forward claim rejected",
			path:       "/api/databases/10/migrations/5/forward",
			withUser:   true,
			canEdit:    true,
			claim:      false,
			wantStatus: http.StatusConflict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &migrationActionTestRepository{
				item: migration.Migration{
					ID:         5,
					DatabaseID: 10,
					Version:    2,
					Status:     migration.StatusSuccess,
				},
				rollbackSQL:     "DROP TABLE items",
				rollbackClaimed: test.claim,
				forwardSQL:      "CREATE TABLE items (id int)",
				forwardClaimed:  test.claim,
			}
			mux := newMigrationActionTestMux(
				t,
				repo,
				migrationActionTestAuthorizer{canEdit: test.canEdit},
			)

			request := httptest.NewRequest(http.MethodPost, test.path, nil)
			if test.withUser {
				request = WithUserID(request, 42)
			}
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("POST %s status = %d, want %d; body: %s",
					test.path, recorder.Code, test.wantStatus, recorder.Body.String())
			}

			var response apiErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode error response JSON: %v", err)
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
