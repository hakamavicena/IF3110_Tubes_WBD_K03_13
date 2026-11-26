package accesscontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/auth"
)

// ctxWithSession membuat context ber-session (menggantikan AuthMiddleware pada unit test).
func ctxWithSession(userID int, csrfToken string) context.Context {
	return context.WithValue(context.Background(), auth.SessionContextKey, &auth.Session{UserID: userID, CSRFToken: csrfToken})
}

type adminFixture struct {
	repo        *fakeAccessRepo
	mux         *http.ServeMux
	revoked     []int
	rotatedFor  int
	rotateCalls int
}

// newAdminFixture merakit AdminHandler + mux nyata (agar PathValue bekerja) dengan admin ID 1.
func newAdminFixture(t *testing.T) *adminFixture {
	t.Helper()
	repo := newFakeRepo()
	repo.users[1] = &User{ID: 1, Username: "admin", IsAdmin: true, IsActive: true}
	repo.users[2] = &User{ID: 2, Username: "alice", IsActive: true}
	repo.users[3] = &User{ID: 3, Username: "bob", IsActive: true}
	fx := &adminFixture{repo: repo, mux: http.NewServeMux()}
	h := NewAdminHandler(NewAccessService(repo), repo, nil, nil)
	h.SetSessionInvalidator(func(id int) error { fx.revoked = append(fx.revoked, id); return nil })
	h.SetSessionRotator(func(w http.ResponseWriter, r *http.Request) (string, error) {
		fx.rotateCalls++
		fx.rotatedFor = actorID(r)
		return "NEW-CSRF", nil
	})
	h.Register(fx.mux, func(next http.HandlerFunc) http.Handler { return next })
	return fx
}

func (fx *adminFixture) do(t *testing.T, asUser int, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd).WithContext(ctxWithSession(asUser, "tok"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec, m
}

func TestAdminAPI_CreateUser_ValidAndInvalid(t *testing.T) {
	fx := newAdminFixture(t)
	rec, m := fx.do(t, 1, "POST", "/api/admin/users", map[string]any{"username": "newuser1", "password": "secretpassword"})
	if rec.Code != http.StatusCreated || m["username"] != "newuser1" {
		t.Fatalf("create valid: %d %v", rec.Code, m)
	}
	if strings.Contains(rec.Body.String(), "secretpassword") || strings.Contains(rec.Body.String(), "hash") {
		t.Fatalf("respons tidak boleh memuat password/hash: %s", rec.Body.String())
	}
	if rec, _ := fx.do(t, 1, "POST", "/api/admin/users", map[string]any{"username": "ab", "password": "secretpassword"}); rec.Code != http.StatusBadRequest {
		t.Errorf("username pendek harus 400, got %d", rec.Code)
	}
	if rec, _ := fx.do(t, 1, "POST", "/api/admin/users", map[string]any{"username": "validname", "password": "short"}); rec.Code != http.StatusBadRequest {
		t.Errorf("password pendek harus 400, got %d", rec.Code)
	}
	req := httptest.NewRequest("POST", "/api/admin/users", strings.NewReader("{bukan json")).WithContext(ctxWithSession(1, "t"))
	rr := httptest.NewRecorder()
	fx.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("JSON rusak harus 400, got %d", rr.Code)
	}
}

func TestAdminAPI_GroupLeader_Rules(t *testing.T) {
	fx := newAdminFixture(t)
	rec, g := fx.do(t, 1, "POST", "/api/admin/groups", map[string]any{"name": "tim-a"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group: %d", rec.Code)
	}
	gid := int(g["id"].(float64))
	path := "/api/admin/groups/" + itoa(gid)

	// Leader harus anggota group.
	if rec, _ := fx.do(t, 1, "PUT", path+"/leader", map[string]any{"user_id": 2}); rec.Code != http.StatusBadRequest {
		t.Fatalf("non-anggota tidak boleh jadi leader, got %d", rec.Code)
	}
	fx.do(t, 1, "POST", path+"/members", map[string]any{"user_id": 2})
	fx.do(t, 1, "POST", path+"/members", map[string]any{"user_id": 3})

	// Leader harus aktif.
	fx.repo.users[3].IsActive = false
	if rec, _ := fx.do(t, 1, "PUT", path+"/leader", map[string]any{"user_id": 3}); rec.Code != http.StatusBadRequest {
		t.Fatalf("user nonaktif tidak boleh jadi leader, got %d", rec.Code)
	}
	fx.repo.users[3].IsActive = true

	fx.revoked = nil
	rec, m := fx.do(t, 1, "PUT", path+"/leader", map[string]any{"user_id": 2})
	if rec.Code != http.StatusOK || m["leader_username"] != "alice" {
		t.Fatalf("set leader: %d %v", rec.Code, m)
	}
	if len(fx.revoked) != 1 || fx.revoked[0] != 2 {
		t.Errorf("sesi leader baru harus dicabut/diputar, revoked=%v", fx.revoked)
	}

	// Mengganti leader: sesi leader lama & baru ikut dicabut.
	fx.revoked = nil
	fx.do(t, 1, "PUT", path+"/leader", map[string]any{"user_id": 3})
	if len(fx.revoked) != 2 {
		t.Errorf("leader lama dan baru harus dicabut sesinya, revoked=%v", fx.revoked)
	}

	// Leader tidak boleh dikeluarkan dari group.
	if rec, _ := fx.do(t, 1, "DELETE", path+"/members/3", nil); rec.Code != http.StatusConflict {
		t.Errorf("mengeluarkan leader harus 409, got %d", rec.Code)
	}
	if rec, _ := fx.do(t, 1, "DELETE", path+"/members/2", nil); rec.Code != http.StatusOK {
		t.Errorf("mengeluarkan non-leader harus 200, got %d", rec.Code)
	}

	// Daftar group memuat leader.
	rec2 := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec2, httptest.NewRequest("GET", "/api/admin/groups", nil).WithContext(ctxWithSession(1, "t")))
	var groups []map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &groups)
	if len(groups) != 1 || groups[0]["leader_username"] != "bob" {
		t.Errorf("daftar group harus memuat leader: %v", groups)
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func TestAdminAPI_PrivilegeChange_RotatesOwnSession_RevokesOthers(t *testing.T) {
	fx := newAdminFixture(t)
	_, g := fx.do(t, 1, "POST", "/api/admin/groups", map[string]any{"name": "tim-b"})
	path := "/api/admin/groups/" + itoa(int(g["id"].(float64)))

	// Admin menambah user lain: sesi user itu dicabut, sesi admin TIDAK dirotasi.
	fx.do(t, 1, "POST", path+"/members", map[string]any{"user_id": 2})
	if len(fx.revoked) != 1 || fx.revoked[0] != 2 || fx.rotateCalls != 0 {
		t.Fatalf("revoked=%v rotate=%d", fx.revoked, fx.rotateCalls)
	}

	// Admin mengubah privilege dirinya sendiri: sesi dirotasi dan csrf baru dikembalikan.
	fx.revoked = nil
	rec, m := fx.do(t, 1, "POST", path+"/members", map[string]any{"user_id": 1})
	if rec.Code != http.StatusOK || m["csrf_token"] != "NEW-CSRF" || fx.rotateCalls != 1 || fx.rotatedFor != 1 {
		t.Fatalf("rotasi sesi sendiri gagal: %d %v rotate=%d", rec.Code, m, fx.rotateCalls)
	}
	if len(fx.revoked) != 0 {
		t.Errorf("sesi sendiri tidak boleh dicabut: %v", fx.revoked)
	}
}

func TestAdminAPI_UserStatus_Protections(t *testing.T) {
	fx := newAdminFixture(t)
	// Tidak boleh menonaktifkan diri sendiri.
	if rec, _ := fx.do(t, 1, "PATCH", "/api/admin/users/1/status", map[string]any{"is_active": false}); rec.Code != http.StatusBadRequest {
		t.Errorf("self-deactivate harus 400, got %d", rec.Code)
	}
	// Tidak boleh mencabut admin terakhir.
	if rec, _ := fx.do(t, 1, "PATCH", "/api/admin/users/1/status", map[string]any{"is_admin": false}); rec.Code != http.StatusConflict {
		t.Errorf("hapus admin terakhir harus 409, got %d", rec.Code)
	}
	// Body kosong.
	if rec, _ := fx.do(t, 1, "PATCH", "/api/admin/users/2/status", map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Errorf("body kosong harus 400, got %d", rec.Code)
	}
	if rec, _ := fx.do(t, 1, "PATCH", "/api/admin/users/99/status", map[string]any{"is_active": false}); rec.Code != http.StatusNotFound {
		t.Errorf("user tak ada harus 404, got %d", rec.Code)
	}
	// Promosi admin -> sesi target dicabut agar mendapat session ID baru saat login ulang.
	fx.revoked = nil
	rec, _ := fx.do(t, 1, "PATCH", "/api/admin/users/2/status", map[string]any{"is_admin": true})
	if rec.Code != http.StatusOK || !fx.repo.users[2].IsAdmin || len(fx.revoked) != 1 || fx.revoked[0] != 2 {
		t.Fatalf("promosi admin: %d admin=%v revoked=%v", rec.Code, fx.repo.users[2].IsAdmin, fx.revoked)
	}
	// Admin kedua ada -> admin pertama boleh dinonaktifkan oleh admin kedua; sesinya dicabut.
	fx.revoked = nil
	rec, _ = fx.do(t, 2, "PATCH", "/api/admin/users/1/status", map[string]any{"is_active": false})
	if rec.Code != http.StatusOK || fx.repo.users[1].IsActive || len(fx.revoked) != 1 || fx.revoked[0] != 1 {
		t.Fatalf("deactivate: %d active=%v revoked=%v", rec.Code, fx.repo.users[1].IsActive, fx.revoked)
	}
}

func TestAdminAPI_SetGroupAccess_Validation_And_LiveEffect(t *testing.T) {
	fx := newAdminFixture(t)
	_, g := fx.do(t, 1, "POST", "/api/admin/groups", map[string]any{"name": "tim-c"})
	gid := int(g["id"].(float64))
	path := "/api/admin/groups/" + itoa(gid) + "/access"
	fx.do(t, 1, "POST", "/api/admin/groups/"+itoa(gid)+"/members", map[string]any{"user_id": 2})
	svc := NewAccessService(fx.repo)

	if rec, _ := fx.do(t, 1, "PUT", path, map[string]any{"database_id": 5, "access_level": 3}); rec.Code != http.StatusBadRequest {
		t.Errorf("level 3 harus 400, got %d", rec.Code)
	}
	if rec, _ := fx.do(t, 1, "PUT", "/api/admin/groups/999/access", map[string]any{"database_id": 5, "access_level": 1}); rec.Code != http.StatusNotFound {
		t.Errorf("group tak ada harus 404, got %d", rec.Code)
	}
	fx.do(t, 1, "PUT", path, map[string]any{"database_id": 5, "access_level": 2})
	if lvl, _ := svc.GetEffectiveAccess(2, 5); lvl != AccessViewEdit {
		t.Fatalf("akses harus View+Edit, got %v", lvl)
	}
	fx.do(t, 1, "PUT", path, map[string]any{"database_id": 5, "access_level": 0})
	if lvl, _ := svc.GetEffectiveAccess(2, 5); lvl != AccessNone {
		t.Fatalf("akses harus dicabut, got %v", lvl)
	}
}

func TestAdminAPI_Targets_NeverExposePassword(t *testing.T) {
	fx := newAdminFixture(t)
	rec, _ := fx.do(t, 1, "POST", "/api/admin/targets", map[string]any{"name": "x", "host": "h", "port": 70000, "database_name": "d", "username": "u", "password": "pw"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("port di luar rentang harus 400, got %d", rec.Code)
	}
	rec, _ = fx.do(t, 1, "POST", "/api/admin/targets", map[string]any{"name": "x", "host": "h", "port": 5432, "database_name": "d", "username": "u", "password": "topsecretpw", "ssl_mode": "bogus"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("ssl_mode tidak valid harus 400, got %d", rec.Code)
	}
	rec, m := fx.do(t, 1, "POST", "/api/admin/targets", map[string]any{"name": "x", "host": "h", "port": 5432, "database_name": "d", "username": "u", "password": "topsecretpw"})
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), "topsecretpw") || m["password_enc"] != nil {
		t.Fatalf("create target: %d %s", rec.Code, rec.Body.String())
	}
	rec2 := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec2, httptest.NewRequest("GET", "/api/admin/targets", nil).WithContext(ctxWithSession(1, "t")))
	if strings.Contains(rec2.Body.String(), "password") {
		t.Errorf("daftar target tidak boleh memuat password: %s", rec2.Body.String())
	}
}

func TestAdmin_Middleware_Protections_JSON(t *testing.T) {
	repo := newFakeRepo()
	repo.users[1] = &User{ID: 1, Username: "admin", IsAdmin: true, IsActive: true}
	repo.users[2] = &User{ID: 2, Username: "regular", IsActive: true}
	protected := RequireAdmin(NewAccessService(repo), func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil).WithContext(ctxWithSession(2, "t"))
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("non-admin harus 403 JSON, got %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = httptest.NewRecorder()
	protected.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/admin/users", nil).WithContext(ctxWithSession(1, "t")))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin harus 200, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	protected.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/admin/users", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tanpa sesi harus 401, got %d", rec.Code)
	}
}
