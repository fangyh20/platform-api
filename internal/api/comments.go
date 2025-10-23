package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/rapidbuildapp/rapidbuild/internal/middleware"
	"github.com/rapidbuildapp/rapidbuild/internal/models"
)

// AddComment handles POST /apps/{appId}/comments
func (h *AppHandler) AddComment(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		middleware.RespondError(w, http.StatusUnauthorized, "User not found in context")
		return
	}

	vars := mux.Vars(r)
	appID := vars["appId"]

	// Verify user owns the app
	_, err := h.AppService.GetApp(r.Context(), appID, user.Sub)
	if err != nil {
		middleware.RespondError(w, http.StatusNotFound, "App not found")
		return
	}

	var req models.AddCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	comment, err := h.CommentService.AddComment(r.Context(), user.Sub, appID, req)
	if err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	middleware.RespondJSON(w, http.StatusCreated, comment)
}

// ListComments handles GET /apps/{appId}/comments
func (h *AppHandler) ListComments(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		middleware.RespondError(w, http.StatusUnauthorized, "User not found in context")
		return
	}

	vars := mux.Vars(r)
	appID := vars["appId"]

	// Verify user owns the app
	_, err := h.AppService.GetApp(r.Context(), appID, user.Sub)
	if err != nil {
		middleware.RespondError(w, http.StatusNotFound, "App not found")
		return
	}

	// Get draft comments
	comments, err := h.CommentService.GetDraftComments(r.Context(), appID, user.Sub)
	if err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	middleware.RespondJSON(w, http.StatusOK, comments)
}

// GetVersionComments handles GET /apps/{appId}/versions/{versionId}/comments
func (h *AppHandler) GetVersionComments(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		middleware.RespondError(w, http.StatusUnauthorized, "User not found in context")
		return
	}

	vars := mux.Vars(r)
	appID := vars["appId"]
	versionID := vars["versionId"]

	// Verify user owns the app
	_, err := h.AppService.GetApp(r.Context(), appID, user.Sub)
	if err != nil {
		middleware.RespondError(w, http.StatusNotFound, "App not found")
		return
	}

	comments, err := h.CommentService.GetVersionComments(r.Context(), versionID)
	if err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	middleware.RespondJSON(w, http.StatusOK, comments)
}

// DeleteComment handles DELETE /apps/{appId}/comments/{commentId}
func (h *AppHandler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		middleware.RespondError(w, http.StatusUnauthorized, "User not found in context")
		return
	}

	vars := mux.Vars(r)
	commentID := vars["commentId"]

	if err := h.CommentService.DeleteComment(r.Context(), commentID, user.Sub); err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
