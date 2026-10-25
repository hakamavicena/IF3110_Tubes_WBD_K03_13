package accesscontrol

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/auth"
	"backend/dbengine/pkg/ports"
)

// fakeAccessRepo mengimplementasikan AccessRepository untuk unit testing tanpa DB nyata
type fakeAccessRepo struct {
	AccessRepository
	users         map[int]*User
	groups        map[int]*Group
	userGroups    map[int][]int          // userID -> []groupID
	groupAccess   map[string]AccessLevel // "groupID:dbID" -> level
	accessibleDBs map[int][]DatabaseWithAccess
	failWith      error
}

func newFakeRepo() *fakeAccessRepo {
	return &fakeAccessRepo{
		users:         make(map[int]*User),
		groups:        make(map[int]*Group),
		userGroups:    make(map[int][]int),
		groupAccess:   make(map[string]AccessLevel),
		accessibleDBs: make(map[int][]DatabaseWithAccess),
	}
}

func (f *fakeAccessRepo) GetUser(userID int) (*User, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	u, ok := f.users[userID]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (f *fakeAccessRepo) GetGroupsByUserID(userID int) ([]Group, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	groupIDs, ok := f.userGroups[userID]
	if !ok {
		return []Group{}, nil
	}
	res := make([]Group, 0, len(groupIDs))
	for _, gid := range groupIDs {
		if g, ok := f.groups[gid]; ok {
			res = append(res, *g)
		}
	}
	return res, nil
}

func (f *fakeAccessRepo) GetAccessByGroupAndDB(groupID, databaseID int) (AccessLevel, error) {
	if f.failWith != nil {
		return AccessNone, f.failWith
	}
	key := fmt.Sprintf("%d:%d", groupID, databaseID)
	lvl, ok := f.groupAccess[key]
	if !ok {
		return AccessNone, nil
	}
	return lvl, nil
}

func (f *fakeAccessRepo) GetAccessibleDatabases(userID int) ([]DatabaseWithAccess, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	return f.accessibleDBs[userID], nil
}

// 1. Admin tanpa group -> level None (CanView=false, CanEdit=false)
func TestAdmin_WithoutGroup_HasNoDatabaseAccess(t *testing.T) {
	repo := newFakeRepo()
	repo.users[1] = &User{ID: 1, Username: "admin", IsAdmin: true}

	service := NewAccessService(repo)
	authz := NewAuthorizer(service, repo)
	ctx := context.Background()

	level, err := service.GetEffectiveAccess(1, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if level != AccessNone {
		t.Fatalf("expected AccessNone for admin without group, got %v", level)
	}

	canView, err := authz.CanView(ctx, 1, 100)
	if err != nil || canView {
		t.Fatalf("expected CanView=false for admin without group, got %v (err: %v)", canView, err)
	}

	canEdit, err := authz.CanEdit(ctx, 1, 100)
	if err != nil || canEdit {
		t.Fatalf("expected CanEdit=false for admin without group, got %v (err: %v)", canEdit, err)
	}
}

// 2. User tanpa group -> level None
func TestUser_WithoutGroup_HasNoAccess(t *testing.T) {
	repo := newFakeRepo()
	repo.users[2] = &User{ID: 2, Username: "user_polos", IsAdmin: false}

	service := NewAccessService(repo)
	authz := NewAuthorizer(service, repo)
	ctx := context.Background()

	level, err := service.GetEffectiveAccess(2, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if level != AccessNone {
		t.Fatalf("expected AccessNone for user without group, got %v", level)
	}

	canView, err := authz.CanView(ctx, 2, 100)
	if err != nil || canView {
		t.Fatalf("expected CanView=false, got %v", canView)
	}
}

// 3. Dua group pada DB yang sama (View dan Edit) -> Edit; setelah keluar dari group Edit -> View
func TestUser_TwoGroupsSameDB_HighestWins_And_Revoke(t *testing.T) {
	repo := newFakeRepo()
	repo.users[3] = &User{ID: 3, Username: "multi_group", IsAdmin: false}
	repo.groups[10] = &Group{ID: 10, Name: "Viewers"}
	repo.groups[20] = &Group{ID: 20, Name: "Editors"}

	dbID := 50
	repo.groupAccess["10:50"] = AccessViewOnly
	repo.groupAccess["20:50"] = AccessViewEdit

	// User ada di kedua group
	repo.userGroups[3] = []int{10, 20}

	service := NewAccessService(repo)
	level, err := service.GetEffectiveAccess(3, dbID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if level != AccessViewEdit {
		t.Fatalf("expected AccessViewEdit when in both View and Edit groups, got %v", level)
	}

	// Simulasi keluar dari group Editors (hanya tersisa Viewers)
	repo.userGroups[3] = []int{10}
	levelAfter, err := service.GetEffectiveAccess(3, dbID)
	if err != nil {
		t.Fatalf("unexpected error after leaving Edit group: %v", err)
	}
	if levelAfter != AccessViewOnly {
		t.Fatalf("expected AccessViewOnly after leaving Edit group, got %v", levelAfter)
	}
}

// 4. Satu group dengan level berbeda pada dua database
func TestGroup_DifferentLevelPerDatabase(t *testing.T) {
	repo := newFakeRepo()
	repo.users[4] = &User{ID: 4, Username: "analyst", IsAdmin: false}
	repo.groups[30] = &Group{ID: 30, Name: "MixedAccess"}
	repo.userGroups[4] = []int{30}

	repo.groupAccess["30:1"] = AccessViewOnly
	repo.groupAccess["30:2"] = AccessViewEdit

	service := NewAccessService(repo)

	levelDB1, err := service.GetEffectiveAccess(4, 1)
	if err != nil || levelDB1 != AccessViewOnly {
		t.Fatalf("expected AccessViewOnly on DB 1, got %v (err: %v)", levelDB1, err)
	}

	levelDB2, err := service.GetEffectiveAccess(4, 2)
	if err != nil || levelDB2 != AccessViewEdit {
		t.Fatalf("expected AccessViewEdit on DB 2, got %v (err: %v)", levelDB2, err)
	}
}

// 5. Edit mencakup View pada Authorizer.CanView / CanEdit
func TestAuthorizer_EditImpliesView(t *testing.T) {
	repo := newFakeRepo()
	repo.users[5] = &User{ID: 5, Username: "editor_user", IsAdmin: false}
	repo.groups[40] = &Group{ID: 40, Name: "Devs"}
	repo.userGroups[5] = []int{40}
	repo.groupAccess["40:10"] = AccessViewEdit

	service := NewAccessService(repo)
	authz := NewAuthorizer(service, repo)
	ctx := context.Background()

	canView, err := authz.CanView(ctx, 5, 10)
	if err != nil || !canView {
		t.Fatalf("expected CanView=true for ViewEdit user, got %v (err: %v)", canView, err)
	}

	canEdit, err := authz.CanEdit(ctx, 5, 10)
	if err != nil || !canEdit {
		t.Fatalf("expected CanEdit=true for ViewEdit user, got %v (err: %v)", canEdit, err)
	}

	// Untuk user ViewOnly: CanView=true, tapi CanEdit=false
	repo.users[6] = &User{ID: 6, Username: "viewer_user", IsAdmin: false}
	repo.groups[41] = &Group{ID: 41, Name: "Auditors"}
	repo.userGroups[6] = []int{41}
	repo.groupAccess["41:10"] = AccessViewOnly

	vCanView, err := authz.CanView(ctx, 6, 10)
	if err != nil || !vCanView {
		t.Fatalf("expected CanView=true for ViewOnly user, got %v", vCanView)
	}
	vCanEdit, err := authz.CanEdit(ctx, 6, 10)
	if err != nil || vCanEdit {
		t.Fatalf("expected CanEdit=false for ViewOnly user, got %v", vCanEdit)
	}
}

// 6. RequireAdmin: tanpa session -> 401, user biasa -> 403, admin -> lolos
func TestRequireAdmin_Middleware(t *testing.T) {
	repo := newFakeRepo()
	repo.users[1] = &User{ID: 1, Username: "admin_user", IsAdmin: true}
	repo.users[2] = &User{ID: 2, Username: "regular_user", IsAdmin: false}

	service := NewAccessService(repo)

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok admin"))
	})

	protected := RequireAdmin(service, dummyHandler)

	// Kasus A: Tanpa session -> 401
	reqA := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rrA := httptest.NewRecorder()
	protected.ServeHTTP(rrA, reqA)
	if rrA.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session, got %d", rrA.Code)
	}

	// Kasus B: User biasa (bukan admin) -> 403
	reqB := httptest.NewRequest(http.MethodGet, "/admin", nil)
	ctxB := context.WithValue(reqB.Context(), auth.SessionContextKey, &auth.Session{UserID: 2})
	rrB := httptest.NewRecorder()
	protected.ServeHTTP(rrB, reqB.WithContext(ctxB))
	if rrB.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for regular user, got %d", rrB.Code)
	}

	// Kasus C: User admin -> 200
	reqC := httptest.NewRequest(http.MethodGet, "/admin", nil)
	ctxC := context.WithValue(reqC.Context(), auth.SessionContextKey, &auth.Session{UserID: 1})
	rrC := httptest.NewRecorder()
	protected.ServeHTTP(rrC, reqC.WithContext(ctxC))
	if rrC.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin user, got %d", rrC.Code)
	}
}

// 7. Error repo (bukan ErrNoRows) -> akses ditolak dan error dikembalikan (fail closed)
func TestService_RepoError_FailsClosed(t *testing.T) {
	repo := newFakeRepo()
	repo.failWith = errors.New("database connection reset")

	service := NewAccessService(repo)
	authz := NewAuthorizer(service, repo)
	ctx := context.Background()

	level, err := service.GetEffectiveAccess(1, 10)
	if err == nil {
		t.Fatalf("expected error when repo fails, got nil")
	}
	if level != AccessNone {
		t.Fatalf("expected AccessNone on failure, got %v", level)
	}

	canView, err := authz.CanView(ctx, 1, 10)
	if err == nil || canView {
		t.Fatalf("expected CanView=false and error on DB failure, got %v (err: %v)", canView, err)
	}
}

// Uji AccessibleDatabases pada Authorizer
func TestAuthorizer_AccessibleDatabases(t *testing.T) {
	repo := newFakeRepo()
	repo.accessibleDBs[10] = []DatabaseWithAccess{
		{DatabaseID: 1, Name: "db_alpha", Level: AccessViewOnly},
		{DatabaseID: 2, Name: "db_beta", Level: AccessViewEdit},
		{DatabaseID: 3, Name: "db_zero", Level: AccessNone}, // harus disaring keluar
	}

	service := NewAccessService(repo)
	authz := NewAuthorizer(service, repo)

	dbs, err := authz.AccessibleDatabases(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dbs) != 2 {
		t.Fatalf("expected 2 databases with level > None, got %d", len(dbs))
	}
	if dbs[0].Name != "db_alpha" || dbs[0].Level != ports.LevelView {
		t.Errorf("db_alpha access mismatch: %+v", dbs[0])
	}
	if dbs[1].Name != "db_beta" || dbs[1].Level != ports.LevelEdit {
		t.Errorf("db_beta access mismatch: %+v", dbs[1])
	}
}
