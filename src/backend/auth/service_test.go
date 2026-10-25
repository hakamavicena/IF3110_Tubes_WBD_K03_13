package auth

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func setupTestService(config AuthConfig) (*AuthService, *MockRepository) {
	mockRepo := NewMockRepository()
	service := NewAuthService(mockRepo, config)
	return service, mockRepo
}

// 1. Test Login: Sukses, Password Salah, User Tidak Ditemukan
func TestLogin_Scenarios(t *testing.T) {
	cfg := DefaultAuthConfig()
	service, repo := setupTestService(cfg)

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("rahasia123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	repo.Users["admin"] = &User{
		ID:       1,
		Username: "admin",
		Password: string(hashedPassword),
	}

	// 1a. Login Sukses
	session, err := service.Login("admin", "rahasia123")
	if err != nil {
		t.Fatalf("Expected no error on successful login, got %v", err)
	}
	if session == nil {
		t.Fatalf("Expected session, got nil")
	}
	if session.UserID != 1 {
		t.Errorf("Expected UserID 1, got %d", session.UserID)
	}
	if len(session.SessionID) < 32 {
		t.Errorf("Expected strong session ID >= 32 chars, got %d", len(session.SessionID))
	}
	if session.CSRFToken == "" || session.CSRFToken == session.SessionID {
		t.Errorf("CSRF token must not be empty or equal to SessionID (csrf: %s, sid: %s)", session.CSRFToken, session.SessionID)
	}

	// 1b. Login Password Salah
	badSession, err := service.Login("admin", "passwordsalah")
	if err != ErrInvalidCredentials {
		t.Errorf("Expected ErrInvalidCredentials, got %v", err)
	}
	if badSession != nil {
		t.Errorf("Expected nil session on bad password")
	}

	// 1c. Login User Tidak Ditemukan
	noUserSession, err := service.Login("unknown_user", "apapun")
	if err != ErrInvalidCredentials {
		t.Errorf("Expected ErrInvalidCredentials, got %v", err)
	}
	if noUserSession != nil {
		t.Errorf("Expected nil session for non-existent user")
	}
}

// 2. Test Bcrypt Ber-salt: Dua hash dari password yang sama harus berbeda tapi keduanya valid
func TestBcrypt_SaltedHashes(t *testing.T) {
	password := []byte("mypassword123")
	hash1, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("Failed hash1: %v", err)
	}
	hash2, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("Failed hash2: %v", err)
	}

	if string(hash1) == string(hash2) {
		t.Fatalf("Expected two salted bcrypt hashes to be distinct, but got identical hashes")
	}

	if err := bcrypt.CompareHashAndPassword(hash1, password); err != nil {
		t.Errorf("Verification of hash1 failed: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword(hash2, password); err != nil {
		t.Errorf("Verification of hash2 failed: %v", err)
	}
}

// 3. Test Idle Timeout dengan Clock Palsu
func TestSession_IdleTimeout(t *testing.T) {
	currentTime := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	cfg := AuthConfig{
		IdleTimeout:      30 * time.Minute,
		AbsoluteTimeout:  12 * time.Hour,
		RotationInterval: 15 * time.Minute,
		GracePeriod:      30 * time.Second,
	}
	service, repo := setupTestService(cfg)
	service.SetNowFunc(func() time.Time { return currentTime })

	hashed, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	repo.Users["user1"] = &User{ID: 10, Username: "user1", Password: string(hashed)}

	sess, err := service.Login("user1", "pass")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Majukan waktu 20 menit (kurang dari idle timeout 30 menit) -> Masih valid
	currentTime = currentTime.Add(20 * time.Minute)
	validSess, _, err := service.ValidateAndRotateSession(sess.SessionID)
	if err != nil {
		t.Fatalf("Session should still be valid after 20 mins: %v", err)
	}
	if validSess == nil {
		t.Fatalf("Expected session returned")
	}

	// Majukan waktu 35 menit setelah aktivitas terakhir -> Melebihi idle timeout 30 menit
	currentTime = currentTime.Add(35 * time.Minute)
	expiredSess, _, err := service.ValidateAndRotateSession(sess.SessionID)
	if err != ErrSessionExpired {
		t.Errorf("Expected ErrSessionExpired after idle timeout, got %v", err)
	}
	if expiredSess != nil {
		t.Errorf("Expected nil session on expiration")
	}
}

// 4. Test Absolute Expiration dengan Clock Palsu
func TestSession_AbsoluteExpiration(t *testing.T) {
	currentTime := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	cfg := AuthConfig{
		IdleTimeout:      1 * time.Hour,
		AbsoluteTimeout:  12 * time.Hour,
		RotationInterval: 2 * time.Hour,
		GracePeriod:      1 * time.Minute,
	}
	service, repo := setupTestService(cfg)
	service.SetNowFunc(func() time.Time { return currentTime })

	hashed, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	repo.Users["user1"] = &User{ID: 10, Username: "user1", Password: string(hashed)}

	sess, err := service.Login("user1", "pass")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Aktivitas rutin tiap 30 menit selama 13 jam (melewati batas absolute 12 jam)
	for i := 0; i < 24; i++ {
		currentTime = currentTime.Add(30 * time.Minute)
		s, newID, err := service.ValidateAndRotateSession(sess.SessionID)
		if i < 23 { // Jam ke-11.5 masih aman
			if err != nil {
				t.Fatalf("Step %d (time: %v): expected valid session, got err %v", i, currentTime, err)
			}
			if newID != "" {
				sess.SessionID = newID
			}
			sess = s
		}
	}

	// Pada jam ke-12.5 (lebih dari 12 jam dari login awal), harus ditolak meskipun aktif terus
	currentTime = currentTime.Add(30 * time.Minute)
	_, _, err = service.ValidateAndRotateSession(sess.SessionID)
	if err != ErrSessionExpired {
		t.Errorf("Expected ErrSessionExpired past absolute expiration, got %v", err)
	}
}

// 5. Test Rotasi Saat Login & Penghapusan Sesi Lama
func TestSession_RotationOnLogin(t *testing.T) {
	cfg := DefaultAuthConfig()
	service, repo := setupTestService(cfg)

	hashed, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	repo.Users["user1"] = &User{ID: 10, Username: "user1", Password: string(hashed)}

	handler := NewAuthHandler(service)

	// Login pertama
	req1 := httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"user1","password":"pass"}`))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("X-Requested-With", "XMLHttpRequest")
	rec1 := httptest.NewRecorder()
	handler.LoginHandler(rec1, req1)

	cookie1 := rec1.Result().Cookies()[0]
	firstSessionID := cookie1.Value

	// Pastikan sesi pertama tersimpan di repo
	if _, exists := repo.Sessions[firstSessionID]; !exists {
		t.Fatalf("Session 1 not found in repo")
	}

	// Login kedua membawa cookie lama
	req2 := httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"user1","password":"pass"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Requested-With", "XMLHttpRequest")
	req2.AddCookie(cookie1)
	rec2 := httptest.NewRecorder()
	handler.LoginHandler(rec2, req2)

	cookie2 := rec2.Result().Cookies()[0]
	secondSessionID := cookie2.Value

	if secondSessionID == firstSessionID {
		t.Errorf("Login must generate a fresh session ID on each login")
	}

	// Sesi lama harus sudah dihapus dari repository
	if _, exists := repo.Sessions[firstSessionID]; exists {
		t.Errorf("Old session %s should have been deleted upon new login", firstSessionID)
	}
}

// 6 & 9. Test Rotasi Berkala: ID berubah, CSRF dan AbsoluteExpiration dipertahankan
func TestSession_PeriodicRotationAndPreservation(t *testing.T) {
	currentTime := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	cfg := AuthConfig{
		IdleTimeout:      30 * time.Minute,
		AbsoluteTimeout:  12 * time.Hour,
		RotationInterval: 15 * time.Minute,
		GracePeriod:      30 * time.Second,
	}
	service, repo := setupTestService(cfg)
	service.SetNowFunc(func() time.Time { return currentTime })

	hashed, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	repo.Users["user1"] = &User{ID: 10, Username: "user1", Password: string(hashed)}

	sess, err := service.Login("user1", "pass")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	initialID := sess.SessionID
	initialCSRF := sess.CSRFToken
	initialAbsExp := sess.AbsoluteExpiration

	// Akses sebelum interval rotasi (mis. 5 menit) -> Tidak memicu rotasi
	currentTime = currentTime.Add(5 * time.Minute)
	s1, newID1, err := service.ValidateAndRotateSession(initialID)
	if err != nil || newID1 != "" {
		t.Fatalf("Should not rotate before rotation interval. newID: %s, err: %v", newID1, err)
	}
	if s1.SessionID != initialID {
		t.Errorf("Session ID should remain identical")
	}

	// Majukan melewati interval rotasi (16 menit sejak pembuatan) -> Memicu rotasi
	currentTime = currentTime.Add(11 * time.Minute) // total 16 menit
	s2, newID2, err := service.ValidateAndRotateSession(initialID)
	if err != nil {
		t.Fatalf("Validation error: %v", err)
	}
	if newID2 == "" || newID2 == initialID {
		t.Fatalf("Expected new session ID from periodic rotation, got %s", newID2)
	}
	if s2.PreviousID != initialID {
		t.Errorf("Expected previous_id = %s, got %s", initialID, s2.PreviousID)
	}
	if s2.CSRFToken != initialCSRF {
		t.Errorf("CSRF token must be preserved during rotation. Expected %s, got %s", initialCSRF, s2.CSRFToken)
	}
	if !s2.AbsoluteExpiration.Equal(initialAbsExp) {
		t.Errorf("Absolute expiration must not be extended during rotation. Expected %v, got %v", initialAbsExp, s2.AbsoluteExpiration)
	}
}

// 7 & 8. Test Grace Period: ID lama diterima selama grace, ditolak setelah grace berakhir
func TestSession_GracePeriod(t *testing.T) {
	currentTime := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	cfg := AuthConfig{
		IdleTimeout:      30 * time.Minute,
		AbsoluteTimeout:  12 * time.Hour,
		RotationInterval: 15 * time.Minute,
		GracePeriod:      30 * time.Second,
	}
	service, repo := setupTestService(cfg)
	service.SetNowFunc(func() time.Time { return currentTime })

	hashed, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	repo.Users["user1"] = &User{ID: 10, Username: "user1", Password: string(hashed)}

	sess, _ := service.Login("user1", "pass")
	oldID := sess.SessionID

	// Pemicu rotasi pada menit ke-16
	currentTime = currentTime.Add(16 * time.Minute)
	_, newID, err := service.ValidateAndRotateSession(oldID)
	if err != nil || newID == "" {
		t.Fatalf("Rotation failed: %v", err)
	}

	// Request konkuren/paralel menggunakan ID LAMA pada detik ke-10 grace period (grace = 30s)
	currentTime = currentTime.Add(10 * time.Second)
	sGrace, reRotID, err := service.ValidateAndRotateSession(oldID)
	if err != nil {
		t.Fatalf("Old ID should be accepted during grace period, got err: %v", err)
	}
	if sGrace.SessionID != newID {
		t.Errorf("Session matching previous_id should map to active session %s, got %s", newID, sGrace.SessionID)
	}
	if reRotID != "" {
		t.Errorf("Requests using old ID during grace period must NOT trigger another rotation")
	}

	// Majukan waktu melewati grace period (mis. detik ke-35 setelah rotasi)
	currentTime = currentTime.Add(25 * time.Second) // total 35s sejak rotasi
	_, _, err = service.ValidateAndRotateSession(oldID)
	if err != ErrSessionExpired {
		t.Errorf("Expected ErrSessionExpired for old ID after grace period expired, got %v", err)
	}

	// Tapi request menggunakan ID BARU tetap berjalan sukses
	sNew, _, err := service.ValidateAndRotateSession(newID)
	if err != nil || sNew == nil {
		t.Errorf("New ID should still be perfectly valid: %v", err)
	}
}

// 10. Test Logout: Cookie lama ditolak
func TestSession_Logout(t *testing.T) {
	cfg := DefaultAuthConfig()
	service, repo := setupTestService(cfg)

	hashed, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	repo.Users["user1"] = &User{ID: 10, Username: "user1", Password: string(hashed)}

	sess, err := service.Login("user1", "pass")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	handler := NewAuthHandler(service)

	// Logout dengan metode GET -> Harus ditolak (405)
	reqGet := httptest.NewRequest("GET", "/logout", nil)
	recGet := httptest.NewRecorder()
	handler.LogoutHandler(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 Method Not Allowed for GET /logout, got %d", recGet.Code)
	}

	// Logout dengan POST tapi tanpa CSRF token -> 403 Forbidden
	reqPostNoCSRF := httptest.NewRequest("POST", "/logout", nil)
	reqPostNoCSRF.AddCookie(&http.Cookie{Name: "session_id", Value: sess.SessionID})
	recPostNoCSRF := httptest.NewRecorder()
	handler.LogoutHandler(recPostNoCSRF, reqPostNoCSRF)
	if recPostNoCSRF.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for POST /logout without CSRF, got %d", recPostNoCSRF.Code)
	}

	// Logout dengan POST dan CSRF token valid -> 303 See Other
	reqPostValid := httptest.NewRequest("POST", "/logout", strings.NewReader("csrf_token="+sess.CSRFToken))
	reqPostValid.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqPostValid.AddCookie(&http.Cookie{Name: "session_id", Value: sess.SessionID})
	recPostValid := httptest.NewRecorder()
	handler.LogoutHandler(recPostValid, reqPostValid)
	if recPostValid.Code != http.StatusSeeOther {
		t.Errorf("Expected 303 See Other for valid logout, got %d", recPostValid.Code)
	}

	// Replay cookie lama setelah logout -> Ditolak (Session expired or invalid)
	_, _, err = service.ValidateAndRotateSession(sess.SessionID)
	if err == nil {
		t.Errorf("Old session cookie should be rejected after logout")
	}
}

// 11. Test InvalidateUserSessions
func TestSession_InvalidateUserSessions(t *testing.T) {
	cfg := DefaultAuthConfig()
	service, repo := setupTestService(cfg)

	hashed, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	repo.Users["user1"] = &User{ID: 10, Username: "user1", Password: string(hashed)}
	repo.Users["user2"] = &User{ID: 20, Username: "user2", Password: string(hashed)}

	s1, _ := service.Login("user1", "pass")
	s2, _ := service.Login("user1", "pass")
	s3, _ := service.Login("user2", "pass")

	if err := service.InvalidateUserSessions(10); err != nil {
		t.Fatalf("Failed to invalidate sessions: %v", err)
	}

	if _, _, err := service.ValidateAndRotateSession(s1.SessionID); err == nil {
		t.Errorf("User1 session 1 should have been invalidated")
	}
	if _, _, err := service.ValidateAndRotateSession(s2.SessionID); err == nil {
		t.Errorf("User1 session 2 should have been invalidated")
	}
	if _, _, err := service.ValidateAndRotateSession(s3.SessionID); err != nil {
		t.Errorf("User2 session should remain unaffected, got %v", err)
	}
}

// 12. Test CSRF Middleware: Hilang / Salah / Benar
func TestCSRFMiddleware(t *testing.T) {
	cfg := DefaultAuthConfig()
	service, repo := setupTestService(cfg)
	handler := NewAuthHandler(service)

	hashed, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	repo.Users["user1"] = &User{ID: 10, Username: "user1", Password: string(hashed)}

	sess, _ := service.Login("user1", "pass")

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	protectedChain := handler.AuthMiddleware(CSRFMiddleware(dummyHandler))

	// 12a. GET request lolos tanpa CSRF token
	reqGet := httptest.NewRequest("GET", "/protected", nil)
	reqGet.AddCookie(&http.Cookie{Name: "session_id", Value: sess.SessionID})
	recGet := httptest.NewRecorder()
	protectedChain.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Errorf("Expected 200 for GET without CSRF, got %d", recGet.Code)
	}

	// 12b. POST tanpa CSRF token -> 403 Forbidden
	reqPostNoToken := httptest.NewRequest("POST", "/protected", strings.NewReader("data=1"))
	reqPostNoToken.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqPostNoToken.AddCookie(&http.Cookie{Name: "session_id", Value: sess.SessionID})
	recPostNoToken := httptest.NewRecorder()
	protectedChain.ServeHTTP(recPostNoToken, reqPostNoToken)
	if recPostNoToken.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for POST without CSRF token, got %d", recPostNoToken.Code)
	}

	// 12c. POST dengan CSRF token salah -> 403 Forbidden
	reqPostWrongToken := httptest.NewRequest("POST", "/protected", strings.NewReader("csrf_token=tokentidakvalid"))
	reqPostWrongToken.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqPostWrongToken.AddCookie(&http.Cookie{Name: "session_id", Value: sess.SessionID})
	recPostWrongToken := httptest.NewRecorder()
	protectedChain.ServeHTTP(recPostWrongToken, reqPostWrongToken)
	if recPostWrongToken.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for POST with wrong CSRF token, got %d", recPostWrongToken.Code)
	}

	// 12d. POST dengan CSRF token via Form -> 200 OK
	reqPostValidForm := httptest.NewRequest("POST", "/protected", strings.NewReader("csrf_token="+sess.CSRFToken))
	reqPostValidForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqPostValidForm.AddCookie(&http.Cookie{Name: "session_id", Value: sess.SessionID})
	recPostValidForm := httptest.NewRecorder()
	protectedChain.ServeHTTP(recPostValidForm, reqPostValidForm)
	if recPostValidForm.Code != http.StatusOK {
		t.Errorf("Expected 200 for POST with valid CSRF form token, got %d", recPostValidForm.Code)
	}

	// 12e. POST dengan CSRF token via Header X-CSRF-Token -> 200 OK
	reqPostValidHeader := httptest.NewRequest("POST", "/protected", strings.NewReader(`{"name":"test"}`))
	reqPostValidHeader.Header.Set("Content-Type", "application/json")
	reqPostValidHeader.Header.Set("X-CSRF-Token", sess.CSRFToken)
	reqPostValidHeader.AddCookie(&http.Cookie{Name: "session_id", Value: sess.SessionID})
	recPostValidHeader := httptest.NewRecorder()
	protectedChain.ServeHTTP(recPostValidHeader, reqPostValidHeader)
	if recPostValidHeader.Code != http.StatusOK {
		t.Errorf("Expected 200 for POST with valid CSRF header token, got %d", recPostValidHeader.Code)
	}
}

// 13. Test Flag Cookie Dev vs Production (Tugas 2)
func TestCookieFlags_DevVsProduction(t *testing.T) {
	// Dev config
	devCfg := AuthConfig{
		AppEnv:       "development",
		SecureCookie: false,
	}
	devService, devRepo := setupTestService(devCfg)
	hashed, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	devRepo.Users["devuser"] = &User{ID: 1, Username: "devuser", Password: string(hashed)}
	devHandler := NewAuthHandler(devService)

	reqDev := httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"devuser","password":"pass"}`))
	reqDev.Header.Set("Content-Type", "application/json")
	reqDev.Header.Set("X-Requested-With", "XMLHttpRequest")
	recDev := httptest.NewRecorder()
	devHandler.LoginHandler(recDev, reqDev)

	cookieDev := recDev.Result().Cookies()[0]
	if cookieDev.Secure != false {
		t.Errorf("Expected dev cookie Secure=false, got %v", cookieDev.Secure)
	}
	if cookieDev.HttpOnly != true {
		t.Errorf("Expected dev cookie HttpOnly=true, got %v", cookieDev.HttpOnly)
	}
	if cookieDev.SameSite != http.SameSiteLaxMode {
		t.Errorf("Expected dev cookie SameSite=Lax, got %v", cookieDev.SameSite)
	}

	// Production config
	prodCfg := AuthConfig{
		AppEnv:       "production",
		SecureCookie: true,
	}
	prodService, prodRepo := setupTestService(prodCfg)
	prodRepo.Users["produser"] = &User{ID: 2, Username: "produser", Password: string(hashed)}
	prodHandler := NewAuthHandler(prodService)

	reqProd := httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"produser","password":"pass"}`))
	reqProd.Header.Set("Content-Type", "application/json")
	reqProd.Header.Set("X-Requested-With", "XMLHttpRequest")
	recProd := httptest.NewRecorder()
	prodHandler.LoginHandler(recProd, reqProd)

	cookieProd := recProd.Result().Cookies()[0]
	if cookieProd.Secure != true {
		t.Errorf("Expected production cookie Secure=true, got %v", cookieProd.Secure)
	}
	if cookieProd.HttpOnly != true {
		t.Errorf("Expected production cookie HttpOnly=true, got %v", cookieProd.HttpOnly)
	}
	if cookieProd.SameSite != http.SameSiteLaxMode {
		t.Errorf("Expected production cookie SameSite=Lax, got %v", cookieProd.SameSite)
	}
}

// 14. Test Log Tidak Memuat Password atau Session ID
func TestLog_NoSensitiveDataLeaked(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(log.Writer())

	cfg := DefaultAuthConfig()
	service, repo := setupTestService(cfg)
	handler := NewAuthHandler(service)

	passwordPlain := "sensitive_super_secret_pw"
	hashed, _ := bcrypt.GenerateFromPassword([]byte(passwordPlain), bcrypt.DefaultCost)
	repo.Users["testuser"] = &User{ID: 1, Username: "testuser", Password: string(hashed)}

	// Login Sukses
	reqSuccess := httptest.NewRequest("POST", "/login", strings.NewReader(fmt.Sprintf(`{"username":"testuser","password":"%s"}`, passwordPlain)))
	reqSuccess.Header.Set("Content-Type", "application/json")
	reqSuccess.Header.Set("X-Requested-With", "XMLHttpRequest")
	recSuccess := httptest.NewRecorder()
	handler.LoginHandler(recSuccess, reqSuccess)

	sessionCookie := recSuccess.Result().Cookies()[0].Value

	// Login Gagal
	reqFail := httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"testuser","password":"wrong_password_attempt"}`))
	reqFail.Header.Set("Content-Type", "application/json")
	reqFail.Header.Set("X-Requested-With", "XMLHttpRequest")
	recFail := httptest.NewRecorder()
	handler.LoginHandler(recFail, reqFail)

	logOutput := buf.String()
	if strings.Contains(logOutput, passwordPlain) {
		t.Errorf("CRITICAL LEAK: Password %s leaked in log output: %s", passwordPlain, logOutput)
	}
	if strings.Contains(logOutput, "wrong_password_attempt") {
		t.Errorf("CRITICAL LEAK: Failed password attempt leaked in log output: %s", logOutput)
	}
	if strings.Contains(logOutput, sessionCookie) {
		t.Errorf("CRITICAL LEAK: Session ID %s leaked in log output: %s", sessionCookie, logOutput)
	}
}

// 15. Test Response Body Tidak Memuat Session ID (AU3)
func TestResponseBody_NoSessionIDLeaked(t *testing.T) {
	cfg := DefaultAuthConfig()
	service, repo := setupTestService(cfg)
	handler := NewAuthHandler(service)

	hashed, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	repo.Users["testuser"] = &User{ID: 1, Username: "testuser", Password: string(hashed)}

	// Login
	sess, err := service.Login("testuser", "pass")
	if err != nil {
		t.Fatalf("Login error: %v", err)
	}

	// Cek form handler dummy yang merender template form dengan CSRFToken
	formHandler := handler.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		csrf := CSRFToken(r)
		html := fmt.Sprintf(`<html><body><form><input type="hidden" name="csrf_token" value="%s"></form></body></html>`, csrf)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))

	req := httptest.NewRequest("GET", "/form", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess.SessionID})
	rec := httptest.NewRecorder()
	formHandler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, sess.SessionID) {
		t.Fatalf("CRITICAL AU3 VIOLATION: Body response contains session ID %s! Body: %s", sess.SessionID, body)
	}
	if !strings.Contains(body, sess.CSRFToken) {
		t.Errorf("Expected CSRF token to be rendered in form, but not found in body: %s", body)
	}
}
