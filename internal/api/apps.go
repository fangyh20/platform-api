package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/rapidbuildapp/rapidbuild/internal/middleware"
	"github.com/rapidbuildapp/rapidbuild/internal/models"
	"github.com/rapidbuildapp/rapidbuild/internal/services"
	"github.com/rapidbuildapp/rapidbuild/internal/worker"
)

type AppHandler struct {
	AppService     *services.AppService
	VersionService *services.VersionService
	CommentService *services.CommentService
	Builder        *worker.Builder
}

func NewAppHandler(
	appService *services.AppService,
	versionService *services.VersionService,
	commentService *services.CommentService,
	builder *worker.Builder,
) *AppHandler {
	return &AppHandler{
		AppService:     appService,
		VersionService: versionService,
		CommentService: commentService,
		Builder:        builder,
	}
}

// CreateApp handles POST /apps
func (h *AppHandler) CreateApp(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		middleware.RespondError(w, http.StatusUnauthorized, "User not found in context")
		return
	}

	var req models.CreateAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Create app (fast - only PostgreSQL, ~50ms)
	app, err := h.AppService.CreateApp(r.Context(), user.Sub, req)
	if err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Create initial version with requirements (fast - only PostgreSQL, ~50ms)
	version, err := h.VersionService.CreateVersion(r.Context(), app.ID, &req.Requirements)
	if err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get owner email for build process (fast query, ~10ms)
	ownerEmail, err := h.AppService.GetOwnerEmail(r.Context(), user.Sub)
	if err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, "Failed to get owner email")
		return
	}

	// Start build process immediately in background (non-blocking)
	// Build assumes MongoDB will be created by async setup process
	go h.Builder.BuildApp(context.Background(), version.ID, app.ID, req.Requirements, nil, ownerEmail)

	// Return response immediately (~100ms total)
	middleware.RespondJSON(w, http.StatusCreated, map[string]interface{}{
		"app":     app,
		"version": version,
	})
}

// ListApps handles GET /apps
func (h *AppHandler) ListApps(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		middleware.RespondError(w, http.StatusUnauthorized, "User not found in context")
		return
	}

	apps, err := h.AppService.ListApps(r.Context(), user.Sub)
	if err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	middleware.RespondJSON(w, http.StatusOK, apps)
}

// GetApp handles GET /apps/{id}
func (h *AppHandler) GetApp(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		middleware.RespondError(w, http.StatusUnauthorized, "User not found in context")
		return
	}

	vars := mux.Vars(r)
	appID := vars["id"]

	app, err := h.AppService.GetApp(r.Context(), appID, user.Sub)
	if err != nil {
		middleware.RespondError(w, http.StatusNotFound, "App not found")
		return
	}

	middleware.RespondJSON(w, http.StatusOK, app)
}

// DeleteApp handles DELETE /apps/{id}
func (h *AppHandler) DeleteApp(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		middleware.RespondError(w, http.StatusUnauthorized, "User not found in context")
		return
	}

	vars := mux.Vars(r)
	appID := vars["id"]

	if err := h.AppService.DeleteApp(r.Context(), appID, user.Sub); err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
