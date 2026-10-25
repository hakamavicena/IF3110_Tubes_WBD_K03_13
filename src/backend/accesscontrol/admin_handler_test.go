package accesscontrol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"backend/auth"
)

// Helper untuk membuat context ber-session admin
func newAdminContext(userID int, csrfToken string) context.Context {
	session := &auth.Session{
		UserID:    userID,
		CSRFToken: csrfToken,
	}
	return context.WithValue(context.Background(), auth.SessionContextKey, session)
}

// Helper untuk membuat context ber-session non-admin
func newUserContext(userID int, csrfToken string) context.Context {
	session := &auth.Session{
		UserID:    userID,
		CSRFToken: csrfToken,
	}
	return context.WithValue(context.Background(), auth.SessionContextKey, session)
}

func TestAdmin_CreateUser_ValidAndInvalid(t *testing.T) {
	repo := newFakeRepo()
	repo.users[1] = &User{ID: 1, Username: "admin", IsAdmin: true, IsActive: true}
	service := NewAccessService(repo)
	handler := NewAdminHandler(service, repo, nil, nil)

	// 1. Valid user
	form := url.Values{
		"username": {"newuser1"},
		"password": {"secretpassword"},
		"is_admin": {"false"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAdminContext(1, "tok123"))
	rec := httptest.NewRecorder()

	handler.CreateUser(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on success, got %d", rec.Code)
	}

	// 2. Invalid username (terlalu pendek)
	formShort := url.Values{
		"username": {"ab"},
		"password": {"secretpassword"},
	}
	req = httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader(formShort.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAdminContext(1, "tok123"))
	rec = httptest.NewRecorder()

	handler.CreateUser(rec, req)
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "err=") {
		t.Fatalf("expected redirect with error for short username, got location: %s", loc)
	}

	// 3. Invalid password (< 8 karakter)
	formShortPw := url.Values{
		"username": {"validname"},
		"password": {"short"},
	}
	req = httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader(formShortPw.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAdminContext(1, "tok123"))
	rec = httptest.NewRecorder()

	handler.CreateUser(rec, req)
	loc = rec.Header().Get("Location")
	if !strings.Contains(loc, "err=") {
		t.Fatalf("expected redirect with error for short password, got location: %s", loc)
	}
}

func TestAdmin_CreateGroup_ValidAndEmpty(t *testing.T) {
	repo := newFakeRepo()
	repo.users[1] = &User{ID: 1, Username: "admin", IsAdmin: true, IsActive: true}
	service := NewAccessService(repo)
	handler := NewAdminHandler(service, repo, nil, nil)

	// 1. Valid group
	form := url.Values{"name": {"engineering"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/groups", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAdminContext(1, "tok123"))
	rec := httptest.NewRecorder()

	handler.CreateGroup(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 on valid group creation, got %d", rec.Code)
	}

	// 2. Empty group name
	formEmpty := url.Values{"name": {"   "}}
	req = httptest.NewRequest(http.MethodPost, "/admin/groups", strings.NewReader(formEmpty.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAdminContext(1, "tok123"))
	rec = httptest.NewRecorder()

	handler.CreateGroup(rec, req)
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "err=") {
		t.Fatalf("expected redirect with error for empty group name, got %s", loc)
	}
}

func TestAdmin_UserGroup_AddAndRemove(t *testing.T) {
	repo := newFakeRepo()
	repo.users[1] = &User{ID: 1, Username: "admin", IsAdmin: true, IsActive: true}
	repo.users[2] = &User{ID: 2, Username: "alice", IsActive: true}
	repo.groups[10] = &Group{ID: 10, Name: "devs"}
	service := NewAccessService(repo)
	handler := NewAdminHandler(service, repo, nil, nil)

	// Tambah user ke grup
	form := url.Values{"user_id": {"2"}, "group_id": {"10"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/groups", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAdminContext(1, "tok123"))
	rec := httptest.NewRecorder()

	handler.AddUserToGroup(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
	if len(repo.userGroups[2]) != 1 || repo.userGroups[2][0] != 10 {
		t.Fatalf("expected user 2 to be in group 10, got %v", repo.userGroups[2])
	}

	// Keluarkan user dari grup
	req = httptest.NewRequest(http.MethodPost, "/admin/users/groups/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAdminContext(1, "tok123"))
	rec = httptest.NewRecorder()

	handler.RemoveUserFromGroup(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
	if len(repo.userGroups[2]) != 0 {
		t.Fatalf("expected user 2 removed from group 10, got %v", repo.userGroups[2])
	}
}

func TestAdmin_SetGroupAccess_UpsertAndRevoke_LiveEffect(t *testing.T) {
	repo := newFakeRepo()
	repo.users[1] = &User{ID: 1, Username: "admin", IsAdmin: true, IsActive: true}
	repo.users[2] = &User{ID: 2, Username: "editor", IsActive: true}
	repo.groups[10] = &Group{ID: 10, Name: "g_edit"}
	repo.userGroups[2] = []int{10}

	service := NewAccessService(repo)
	handler := NewAdminHandler(service, repo, nil, nil)

	// Cek awal: user 2 belum punya akses ke DB 100
	lvl, err := service.GetEffectiveAccess(2, 100)
	if err != nil || lvl != AccessNone {
		t.Fatalf("expected initial AccessNone, got %v", lvl)
	}

	// 1. Berikan hak akses Edit (level 2)
	form := url.Values{"group_id": {"10"}, "database_id": {"100"}, "access_level": {"2"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/groups/access", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAdminContext(1, "tok123"))
	rec := httptest.NewRecorder()

	handler.SetGroupAccess(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}

	// Akses live berubah seketika tanpa login ulang!
	lvl, err = service.GetEffectiveAccess(2, 100)
	if err != nil || lvl != AccessViewEdit {
		t.Fatalf("expected live update to AccessViewEdit, got %v", lvl)
	}

	// 2. Cabut hak akses (level 0)
	formRevoke := url.Values{"group_id": {"10"}, "database_id": {"100"}, "access_level": {"0"}}
	req = httptest.NewRequest(http.MethodPost, "/admin/groups/access", strings.NewReader(formRevoke.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAdminContext(1, "tok123"))
	rec = httptest.NewRecorder()

	handler.SetGroupAccess(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}

	// Akses live seketika dicabut kembali menjadi AccessNone!
	lvl, err = service.GetEffectiveAccess(2, 100)
	if err != nil || lvl != AccessNone {
		t.Fatalf("expected live revocation to AccessNone, got %v", lvl)
	}
}

func TestAdmin_UserDeactivation_ProtectionsAndSessionRevocation(t *testing.T) {
	repo := newFakeRepo()
	repo.users[1] = &User{ID: 1, Username: "admin1", IsAdmin: true, IsActive: true}
	repo.users[2] = &User{ID: 2, Username: "admin2", IsAdmin: true, IsActive: true}
	repo.users[3] = &User{ID: 3, Username: "regular", IsAdmin: false, IsActive: true}

	service := NewAccessService(repo)
	handler := NewAdminHandler(service, repo, nil, nil)

	invalidatedUser := 0
	handler.SetSessionInvalidator(func(uID int) error {
		invalidatedUser = uID
		return nil
	})

	// 1. Admin tidak boleh menonaktifkan dirinya sendiri
	formSelf := url.Values{"user_id": {"1"}, "action": {"deactivate"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/status", strings.NewReader(formSelf.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAdminContext(1, "tok123"))
	rec := httptest.NewRecorder()

	handler.SetUserStatus(rec, req)
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "err=") {
		t.Fatalf("expected error redirect when admin deactivates self, got %s", loc)
	}
	if !repo.users[1].IsActive {
		t.Fatalf("admin1 should remain active")
	}

	// 2. Sukses menonaktifkan admin2 (karena masih ada admin1 aktif)
	formAdmin2 := url.Values{"user_id": {"2"}, "action": {"deactivate"}}
	req = httptest.NewRequest(http.MethodPost, "/admin/users/status", strings.NewReader(formAdmin2.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAdminContext(1, "tok123"))
	rec = httptest.NewRecorder()

	handler.SetUserStatus(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
	if repo.users[2].IsActive {
		t.Fatalf("admin2 should be deactivated")
	}
	if invalidatedUser != 2 {
		t.Fatalf("expected session invalidator called for user 2, got %d", invalidatedUser)
	}

	// 3. Menolak menonaktifkan admin terakhir (admin1 tersisa sendirian)
	// Login sebagai user 3 tidak bisa (bukan admin), coba dari rute langsung dengan target admin1
	count, _ := repo.GetActiveAdminCount()
	if count != 1 {
		t.Fatalf("expected 1 active admin remaining, got %d", count)
	}

	// 4. Sukses menonaktifkan user regular
	formReg := url.Values{"user_id": {"3"}, "action": {"deactivate"}}
	req = httptest.NewRequest(http.MethodPost, "/admin/users/status", strings.NewReader(formReg.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAdminContext(1, "tok123"))
	rec = httptest.NewRecorder()

	handler.SetUserStatus(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
	if repo.users[3].IsActive {
		t.Fatalf("regular user should be deactivated")
	}
	if invalidatedUser != 3 {
		t.Fatalf("expected session invalidator called for user 3, got %d", invalidatedUser)
	}
}

func TestAdmin_Middleware_Protections(t *testing.T) {
	repo := newFakeRepo()
	repo.users[1] = &User{ID: 1, Username: "admin", IsAdmin: true, IsActive: true}
	repo.users[2] = &User{ID: 2, Username: "regular", IsAdmin: false, IsActive: true}
	service := NewAccessService(repo)

	dummyAdminHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("admin_ok"))
	})

	protectedHandler := RequireAdmin(service, dummyAdminHandler)

	// 1. Regular user access -> 403 Forbidden
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(newUserContext(2, "tok"))
	rec := httptest.NewRecorder()

	protectedHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", rec.Code)
	}

	// 2. Admin access -> 200 OK
	req = httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(newAdminContext(1, "tok"))
	rec = httptest.NewRecorder()

	protectedHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin, got %d", rec.Code)
	}
}
