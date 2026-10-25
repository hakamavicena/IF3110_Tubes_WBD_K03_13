package httpdb

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hakamavicena/if3110-dbengine/pkg/dbviewer"
	"github.com/hakamavicena/if3110-dbengine/pkg/migration"
	"github.com/hakamavicena/if3110-dbengine/pkg/ports"
)

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
