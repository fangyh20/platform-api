package api

import (
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/rapidbuildapp/rapidbuild/internal/middleware"
	"github.com/rapidbuildapp/rapidbuild/internal/services"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type PreviewHandler struct {
	AppService     *services.AppService
	VersionService *services.VersionService
	MongoClient    *mongo.Client
}

func NewPreviewHandler(
	appService *services.AppService,
	versionService *services.VersionService,
	mongoClient *mongo.Client,
) *PreviewHandler {
	return &PreviewHandler{
		AppService:     appService,
		VersionService: versionService,
		MongoClient:    mongoClient,
	}
}

// GeneratePreviewToken generates a JWT for the owner to preview their app
func (h *PreviewHandler) GeneratePreviewToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	appID := vars["id"]

	// Get authenticated user
	user, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		middleware.RespondError(w, http.StatusUnauthorized, "User not found in context")
		return
	}

	// 1. Verify ownership and get owner email from PostgreSQL
	_, ownerEmail, err := h.AppService.GetAppWithOwnerEmail(ctx, appID, user.Sub)
	if err != nil {
		middleware.RespondError(w, http.StatusForbidden, "App not found or unauthorized")
		return
	}

	// 2. Get MongoDB app configuration (for JWT secret)
	mongoDb := h.MongoClient.Database("system_db")
	appsCol := mongoDb.Collection("apps")

	var mongoApp struct {
		ID           string `bson:"_id"`
		DatabaseName string `bson:"databaseName"`
		JWT          struct {
			Secret string `bson:"secret"`
		} `bson:"jwt"`
	}

	err = appsCol.FindOne(ctx, bson.M{"_id": appID}).Decode(&mongoApp)
	if err != nil {
		middleware.RespondError(w, http.StatusNotFound, "App configuration not found in MongoDB")
		return
	}

	// 3. Get admin user from app_users
	appUsersCol := mongoDb.Collection("app_users")

	var adminUser struct {
		ID    string   `bson:"_id"`
		Email string   `bson:"email"`
		Roles []string `bson:"roles"`
	}

	err = appUsersCol.FindOne(ctx, bson.M{
		"appId": appID,
		"email": ownerEmail,
	}).Decode(&adminUser)

	if err != nil {
		middleware.RespondError(w, http.StatusNotFound, "Admin user not found. App may need to be rebuilt.")
		return
	}

	// 4. Generate JWT using app's secret
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":    adminUser.ID,
		"userId": adminUser.ID,
		"email":  adminUser.Email,
		"appId":  appID,
		"roles":  adminUser.Roles,
		"exp":    time.Now().Add(5 * time.Minute).Unix(),
		"iat":    time.Now().Unix(),
	})

	signedToken, err := token.SignedString([]byte(mongoApp.JWT.Secret))
	if err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	// 5. Get latest completed version URL
	versions, err := h.VersionService.ListVersions(ctx, appID)
	if err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, "Failed to get versions")
		return
	}

	var latestURL *string
	for _, v := range versions {
		if v.Status == "completed" && v.VercelURL != nil {
			latestURL = v.VercelURL
			break
		}
	}

	if latestURL == nil {
		middleware.RespondError(w, http.StatusNotFound, "No deployed version found")
		return
	}

	// 6. Return token + preview URL
	middleware.RespondJSON(w, http.StatusOK, map[string]string{
		"token":      signedToken,
		"previewUrl": *latestURL,
	})
}
