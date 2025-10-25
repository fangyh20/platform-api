package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/rapidbuildapp/rapidbuild/internal/middleware"
	"github.com/rapidbuildapp/rapidbuild/internal/models"
)

// ListVersions handles GET /apps/{appId}/versions
func (h *AppHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
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

	versions, err := h.VersionService.ListVersions(r.Context(), appID)
	if err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	middleware.RespondJSON(w, http.StatusOK, versions)
}

// GetVersion handles GET /apps/{appId}/versions/{versionId}
func (h *AppHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
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

	version, err := h.VersionService.GetVersion(r.Context(), versionID)
	if err != nil {
		middleware.RespondError(w, http.StatusNotFound, "Version not found")
		return
	}

	middleware.RespondJSON(w, http.StatusOK, version)
}

// CreateVersion handles POST /apps/{appId}/versions
func (h *AppHandler) CreateVersion(w http.ResponseWriter, r *http.Request) {
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

	var req models.CreateVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Create version
	version, err := h.VersionService.CreateVersion(r.Context(), appID)
	if err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get comments for this version
	var comments []models.Comment

	// Submit the comments
	if len(req.Comments) > 0 {
		if err := h.CommentService.SubmitComments(r.Context(), req.Comments, version.ID); err != nil {
			middleware.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Fetch the submitted comments for the build
		comments, _ = h.CommentService.GetVersionComments(r.Context(), version.ID)
	}

	// Start build process in background
	// Use context.Background() instead of r.Context() so the build continues even after HTTP response
	// Pass empty string for ownerEmail since admin user was created during app creation
	go h.Builder.BuildApp(context.Background(), version.ID, appID, "", comments, "")

	middleware.RespondJSON(w, http.StatusCreated, version)
}

// PromoteVersion handles POST /apps/{appId}/versions/{versionId}/promote
func (h *AppHandler) PromoteVersion(w http.ResponseWriter, r *http.Request) {
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

	if err := h.VersionService.PromoteVersion(r.Context(), versionID); err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	middleware.RespondJSON(w, http.StatusOK, map[string]string{"status": "promoted"})
}

// DeleteVersion handles DELETE /apps/{appId}/versions/{versionId}
func (h *AppHandler) DeleteVersion(w http.ResponseWriter, r *http.Request) {
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

	if err := h.VersionService.DeleteVersion(r.Context(), versionID); err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
