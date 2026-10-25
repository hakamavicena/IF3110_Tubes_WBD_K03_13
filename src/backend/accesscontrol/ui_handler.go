package accesscontrol

import (
	"html/template"
	"net/http"
	"path/filepath"
)

func (h *AdminHandler) RenderAdminDashboard(w http.ResponseWriter, r *http.Request) {
	tmplPath := filepath.Join("..", "frontend", "admin_dashboard.html") 
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		http.Error(w, "Gagal load template UI: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = tmpl.Execute(w, nil)
}