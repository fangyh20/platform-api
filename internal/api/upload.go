package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/rapidbuildapp/rapidbuild/internal/services"
)

type UploadHandler struct {
	UploadService *services.UploadService
}

func NewUploadHandler(uploadService *services.UploadService) *UploadHandler {
	return &UploadHandler{
		UploadService: uploadService,
	}
}

// UploadRequirementFile handles POST /apps/{appId}/versions/{versionId}/upload
func (h *UploadHandler) UploadRequirementFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appID := vars["appId"]
	versionID := vars["versionId"]

	// Parse multipart form (max 10MB)
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		http.Error(w, `{"error":"Failed to parse form"}`, http.StatusBadRequest)
		return
	}

	// Get file from form
	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"No file provided"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Upload file
	reqFile, err := h.UploadService.UploadRequirementFile(r.Context(), appID, versionID, fileHeader)
	if err != nil {
		http.Error(w, `{"error":"Failed to upload file: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(reqFile)
}
