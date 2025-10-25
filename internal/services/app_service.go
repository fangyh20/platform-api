package services

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/rapidbuildapp/rapidbuild/config"
	"github.com/rapidbuildapp/rapidbuild/internal/db"
	"github.com/rapidbuildapp/rapidbuild/internal/models"
	"github.com/rapidbuildapp/rapidbuild/internal/utils"
	"go.mongodb.org/mongo-driver/mongo"
)

type AppService struct {
	DB             *db.PostgresClient
	MongoClient    *mongo.Client
	GeminiService  *GeminiService
	RunwareService *RunwareService
	S3Client       *s3.Client
	Config         *config.Config
}

func NewAppService(
	dbClient *db.PostgresClient,
	mongoClient *mongo.Client,
	geminiService *GeminiService,
	runwareService *RunwareService,
	s3Client *s3.Client,
	cfg *config.Config,
) *AppService {
	return &AppService{
		DB:             dbClient,
		MongoClient:    mongoClient,
		GeminiService:  geminiService,
		RunwareService: runwareService,
		S3Client:       s3Client,
		Config:         cfg,
	}
}

// CreateApp creates a new app with temporary defaults, then extracts AI config async
func (s *AppService) CreateApp(ctx context.Context, userID string, req models.CreateAppRequest) (*models.App, error) {
	// 1. Create app immediately with temporary defaults (fast PostgreSQL-only operation)
	appID := uuid.New().String()
	tempName := "MyApp"
	tempDisplayName := "My App"
	tempCategory := "other"
	tempColorScheme := "blue"
	productionURL := utils.GenerateProductionDomain(tempName, appID)

	app := models.App{
		ID:            appID,
		UserID:        userID,
		Name:          tempName,
		DisplayName:   &tempDisplayName,
		Description:   req.Description,
		Category:      &tempCategory,
		ColorScheme:   &tempColorScheme,
		Status:        "building",
		ProductionURL: &productionURL,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	query := `
		INSERT INTO apps (id, user_id, name, display_name, description, category, color_scheme, status, production_url, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, user_id, name, display_name, description, category, color_scheme, logo, status, prod_version, production_url, created_at, updated_at
	`

	err := s.DB.QueryRow(ctx, query,
		app.ID, app.UserID, app.Name, app.DisplayName, app.Description,
		app.Category, app.ColorScheme, app.Status, app.ProductionURL, app.CreatedAt, app.UpdatedAt,
	).Scan(
		&app.ID, &app.UserID, &app.Name, &app.DisplayName, &app.Description,
		&app.Category, &app.ColorScheme, &app.Logo, &app.Status,
		&app.ProdVersion, &app.ProductionURL, &app.CreatedAt, &app.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create app: %w", err)
	}

	// 2. Launch async AI config extraction and MongoDB creation (non-blocking)
	go s.extractConfigAndSetupApp(appID, userID, req.Description)

	return &app, nil
}

// extractConfigAndSetupApp runs async to extract AI config and setup MongoDB with final name
func (s *AppService) extractConfigAndSetupApp(appID, userID, description string) {
	ctx := context.Background()

	log.Printf("[AI Setup] Starting async config extraction for app %s", appID)

	// 1. Use Gemini to extract app configuration from description
	appConfig, err := s.GeminiService.ExtractAppConfig(description)
	if err != nil {
		log.Printf("[AI Setup] Warning: Failed to extract config with Gemini, using defaults: %v", err)
		// Fallback to defaults if Gemini fails
		appConfig = &AppConfig{
			AppName:      "MyApp",
			DisplayName:  "My App",
			RequiresAuth: true,
			AllowSignup:  true,
			Category:     "other",
			Keywords:     []string{"app"},
			ColorScheme:  "blue",
		}
	}

	log.Printf("[AI Setup] Extracted config for app %s: name=%s, category=%s, color=%s",
		appID, appConfig.AppName, appConfig.Category, appConfig.ColorScheme)

	// 2. Update PostgreSQL with AI-generated configuration
	productionURL := utils.GenerateProductionDomain(appConfig.AppName, appID)
	updateQuery := `
		UPDATE apps
		SET name = $1, display_name = $2, category = $3, color_scheme = $4, production_url = $5, updated_at = $6
		WHERE id = $7
	`
	_, err = s.DB.Exec(ctx, updateQuery,
		appConfig.AppName, appConfig.DisplayName, appConfig.Category,
		appConfig.ColorScheme, productionURL, time.Now(), appID,
	)
	if err != nil {
		log.Printf("[AI Setup] Failed to update PostgreSQL with AI config: %v", err)
		return
	}

	log.Printf("[AI Setup] Updated PostgreSQL for app %s with AI-generated name '%s'", appID, appConfig.AppName)

	// 3. Get owner email for MongoDB app creation
	ownerEmail, err := s.GetOwnerEmail(ctx, userID)
	if err != nil {
		log.Printf("[AI Setup] Warning: Failed to get owner email: %v", err)
		ownerEmail = "unknown@example.com"
	}

	// 4. Create app in MongoDB with final AI-generated name (not temporary)
	cmd := exec.CommandContext(ctx, "app-manager", "create", appID, "--name", appConfig.AppName, "--owner-email", ownerEmail)
	cmd.Env = append(os.Environ(),
		"PATH=/home/ubuntu/.local/share/pnpm:/usr/local/bin:/usr/bin:/bin",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		log.Printf("[AI Setup] Warning: Failed to create MongoDB app: %v (stderr: %s)", err, stderr.String())
	} else {
		log.Printf("[AI Setup] Created MongoDB app with AI-generated name '%s'", appConfig.AppName)
	}

	// 5. Launch async logo generation (non-blocking)
	go s.generateAndUploadLogo(appID, appConfig.AppName, appConfig.Category, appConfig.ColorScheme)

	log.Printf("[AI Setup] Completed async setup for app %s", appID)
}

// GetApp retrieves an app by ID
func (s *AppService) GetApp(ctx context.Context, appID, userID string) (*models.App, error) {
	app := &models.App{}
	query := `
		SELECT id, user_id, name, display_name, description, logo, category, color_scheme, status, prod_version, production_url, created_at, updated_at
		FROM apps
		WHERE id = $1 AND user_id = $2
	`

	err := s.DB.QueryRow(ctx, query, appID, userID).Scan(
		&app.ID, &app.UserID, &app.Name, &app.DisplayName, &app.Description,
		&app.Logo, &app.Category, &app.ColorScheme, &app.Status,
		&app.ProdVersion, &app.ProductionURL, &app.CreatedAt, &app.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("app not found: %w", err)
	}

	return app, nil
}

// ListApps retrieves all apps for a user
func (s *AppService) ListApps(ctx context.Context, userID string) ([]models.App, error) {
	query := `
		SELECT id, user_id, name, display_name, description, logo, category, color_scheme, status, prod_version, production_url, created_at, updated_at
		FROM apps
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.DB.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list apps: %w", err)
	}
	defer rows.Close()

	var apps []models.App
	for rows.Next() {
		var app models.App
		err := rows.Scan(
			&app.ID, &app.UserID, &app.Name, &app.DisplayName, &app.Description,
			&app.Logo, &app.Category, &app.ColorScheme, &app.Status,
			&app.ProdVersion, &app.ProductionURL, &app.CreatedAt, &app.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan app: %w", err)
		}
		apps = append(apps, app)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating apps: %w", err)
	}

	return apps, nil
}

// UpdateApp updates an app
func (s *AppService) UpdateApp(ctx context.Context, appID, userID string, updates map[string]interface{}) (*models.App, error) {
	// Build dynamic UPDATE query based on provided updates
	query := `
		UPDATE apps
		SET updated_at = $1
	`
	args := []interface{}{time.Now()}
	argCount := 2

	if name, ok := updates["name"].(string); ok {
		query += fmt.Sprintf(", name = $%d", argCount)
		args = append(args, name)
		argCount++
	}

	if description, ok := updates["description"].(string); ok {
		query += fmt.Sprintf(", description = $%d", argCount)
		args = append(args, description)
		argCount++
	}

	if status, ok := updates["status"].(string); ok {
		query += fmt.Sprintf(", status = $%d", argCount)
		args = append(args, status)
		argCount++
	}

	if prodVersion, ok := updates["prod_version"].(int); ok {
		query += fmt.Sprintf(", prod_version = $%d", argCount)
		args = append(args, prodVersion)
		argCount++
	}

	query += fmt.Sprintf(" WHERE id = $%d", argCount)
	args = append(args, appID)
	argCount++

	if userID != "" {
		query += fmt.Sprintf(" AND user_id = $%d", argCount)
		args = append(args, userID)
		argCount++
	}

	query += " RETURNING id, user_id, name, description, status, prod_version, created_at, updated_at"

	app := &models.App{}
	err := s.DB.QueryRow(ctx, query, args...).Scan(
		&app.ID, &app.UserID, &app.Name, &app.Description, &app.Status,
		&app.ProdVersion, &app.CreatedAt, &app.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to update app: %w", err)
	}

	return app, nil
}

// DeleteApp deletes an app
func (s *AppService) DeleteApp(ctx context.Context, appID, userID string) error {
	query := `DELETE FROM apps WHERE id = $1 AND user_id = $2`
	rowsAffected, err := s.DB.Exec(ctx, query, appID, userID)
	if err != nil {
		return fmt.Errorf("failed to delete app: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("app not found")
	}

	return nil
}

// GetOwnerEmail retrieves the email of the app owner
func (s *AppService) GetOwnerEmail(ctx context.Context, userID string) (string, error) {
	var email string
	query := `SELECT email FROM users WHERE id = $1`

	err := s.DB.QueryRow(ctx, query, userID).Scan(&email)
	if err != nil {
		return "", fmt.Errorf("failed to get owner email: %w", err)
	}

	return email, nil
}

// GetAppWithOwnerEmail retrieves app and owner email for preview token generation
func (s *AppService) GetAppWithOwnerEmail(ctx context.Context, appID, userID string) (*models.App, string, error) {
	var email string
	app := &models.App{}

	query := `
		SELECT a.id, a.user_id, a.name, a.display_name, a.description, a.logo,
		       a.category, a.color_scheme, a.status, a.prod_version, a.production_url,
		       a.created_at, a.updated_at, u.email
		FROM apps a
		JOIN users u ON a.user_id = u.id
		WHERE a.id = $1 AND a.user_id = $2
	`

	err := s.DB.QueryRow(ctx, query, appID, userID).Scan(
		&app.ID, &app.UserID, &app.Name, &app.DisplayName, &app.Description,
		&app.Logo, &app.Category, &app.ColorScheme, &app.Status,
		&app.ProdVersion, &app.ProductionURL, &app.CreatedAt, &app.UpdatedAt, &email,
	)

	if err != nil {
		return nil, "", fmt.Errorf("app not found: %w", err)
	}

	return app, email, nil
}

// generateAndUploadLogo generates logo using AI and uploads to S3 (runs async)
func (s *AppService) generateAndUploadLogo(appID, appName, category, colorScheme string) {
	ctx := context.Background()

	log.Printf("[Logo] Starting async logo generation for app %s (%s)", appID, appName)

	// 1. Generate logo using Runware
	imageURL, err := s.RunwareService.GenerateLogo(appName, category, colorScheme)
	if err != nil {
		log.Printf("[Logo] Failed to generate logo for app %s: %v", appID, err)
		return
	}
	log.Printf("[Logo] Generated logo for app %s: %s", appID, imageURL)

	// 2. Download image
	imageData, err := s.RunwareService.DownloadImage(imageURL)
	if err != nil {
		log.Printf("[Logo] Failed to download logo for app %s: %v", appID, err)
		return
	}
	log.Printf("[Logo] Downloaded logo for app %s (%d bytes)", appID, len(imageData))

	// 3. Upload to S3 (bucket policy handles public access)
	s3Key := fmt.Sprintf("apps/%s/logo.png", appID)
	_, err = s.S3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.Config.S3Bucket),
		Key:         aws.String(s3Key),
		Body:        bytes.NewReader(imageData),
		ContentType: aws.String("image/png"),
	})
	if err != nil {
		log.Printf("[Logo] Failed to upload logo to S3 for app %s: %v", appID, err)
		return
	}

	// Convert S3 path to HTTPS URL
	httpsURL := fmt.Sprintf("https://%s.s3.amazonaws.com/%s", s.Config.S3Bucket, s3Key)
	log.Printf("[Logo] Uploaded logo to S3 for app %s: %s", appID, httpsURL)

	// 4. Update PostgreSQL with HTTPS URL
	query := `UPDATE apps SET logo = $1, updated_at = $2 WHERE id = $3`
	_, err = s.DB.Exec(ctx, query, httpsURL, time.Now(), appID)
	if err != nil {
		log.Printf("[Logo] Failed to update PostgreSQL for app %s: %v", appID, err)
		return
	}
	log.Printf("[Logo] Updated PostgreSQL with logo for app %s", appID)

	// 5. Update MongoDB (both name and logo) using app-manager CLI
	cmd := exec.CommandContext(ctx, "app-manager", "update", appID, "--name", appName, "--logo", httpsURL)
	cmd.Env = append(os.Environ(),
		"PATH=/home/ubuntu/.local/share/pnpm:/usr/local/bin:/usr/bin:/bin",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		log.Printf("[Logo] Failed to update MongoDB via app-manager for app %s: %v (stderr: %s)", appID, err, stderr.String())
		return
	}
	log.Printf("[Logo] Updated MongoDB with name '%s' and logo for app %s via app-manager", appName, appID)

	log.Printf("[Logo] Successfully completed logo generation for app %s", appID)
}
