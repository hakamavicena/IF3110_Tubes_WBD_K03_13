package accesscontrol

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"backend/auth"
	"backend/dbengine/pkg/apperr"
)

type ApprovalHandler struct {
	svc *ApprovalService
}

func NewApprovalHandler(svc *ApprovalService) *ApprovalHandler { return &ApprovalHandler{svc: svc} }

func (h *ApprovalHandler) Register(mux *http.ServeMux, protect func(http.Handler) http.Handler) {
	mux.Handle("POST /api/databases/{dbID}/jobs/{jobID}/submit-review", protect(http.HandlerFunc(h.SubmitReview)))
	mux.Handle("POST /api/databases/{dbID}/jobs/{jobID}/approve", protect(http.HandlerFunc(h.Approve)))
	mux.Handle("GET /api/databases/{dbID}/jobs/{jobID}/approvals", protect(http.HandlerFunc(h.Approvals)))
	mux.Handle("GET /api/approvals/inbox", protect(http.HandlerFunc(h.Inbox)))
}

func (h *ApprovalHandler) Inbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		auth.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	items, err := h.svc.Inbox(r.Context(), userID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, items)
}

func pathInt64(r *http.Request, name string) (int64, bool) {
	v, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return v, err == nil && v > 0
}

func (h *ApprovalHandler) ids(w http.ResponseWriter, r *http.Request) (userID, dbID, jobID int64, ok bool) {
	userID, authed := auth.UserIDFromContext(r.Context())
	if !authed {
		auth.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var okDB, okJob bool
	dbID, okDB = pathInt64(r, "dbID")
	jobID, okJob = pathInt64(r, "jobID")
	if !okDB || !okJob {
		auth.WriteError(w, http.StatusBadRequest, "dbID/jobID tidak valid")
		return
	}
	return userID, dbID, jobID, true
}

func (h *ApprovalHandler) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrNotLeader), errors.Is(err, apperr.ErrForbidden):
		auth.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrJobNotFound):
		auth.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrInvalidJobState), errors.Is(err, ErrGroupNoLeader):
		auth.WriteError(w, http.StatusConflict, err.Error())
	default:
		slog.ErrorContext(r.Context(), "approval request gagal", "event", "approval.error", "status", "error", "path", r.URL.Path, "err", err)
		auth.WriteError(w, http.StatusInternalServerError, "terjadi kesalahan pada server")
	}
}

func (h *ApprovalHandler) SubmitReview(w http.ResponseWriter, r *http.Request) {
	userID, dbID, jobID, ok := h.ids(w, r)
	if !ok {
		return
	}
	st, err := h.svc.SubmitReview(r.Context(), userID, dbID, jobID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, st)
}

func (h *ApprovalHandler) Approve(w http.ResponseWriter, r *http.Request) {
	userID, dbID, jobID, ok := h.ids(w, r)
	if !ok {
		return
	}
	var body struct {
		GroupID int `json:"group_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		auth.WriteError(w, http.StatusBadRequest, "body request tidak valid")
		return
	}
	if body.GroupID < 0 {
		auth.WriteError(w, http.StatusBadRequest, "group_id tidak valid")
		return
	}
	st, err := h.svc.Approve(r.Context(), userID, dbID, jobID, body.GroupID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, st)
}

func (h *ApprovalHandler) Approvals(w http.ResponseWriter, r *http.Request) {
	userID, dbID, jobID, ok := h.ids(w, r)
	if !ok {
		return
	}
	st, err := h.svc.Status(r.Context(), userID, dbID, jobID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, st)
}
