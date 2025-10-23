package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rapidbuildapp/rapidbuild/internal/db"
	"github.com/rapidbuildapp/rapidbuild/internal/models"
)

type AppService struct {
	DB *db.PostgresClient
}

func NewAppService(dbClient *db.PostgresClient) *AppService {
	return &AppService{DB: dbClient}
}

// CreateApp creates a new app and starts the initial build
func (s *AppService) CreateApp(ctx context.Context, userID string, req models.CreateAppRequest) (*models.App, error) {
	app := models.App{
		ID:          uuid.New().String(),
		UserID:      userID,
		Name:        req.Name,
		Description: req.Description,
		Status:      "building",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	query := `
		INSERT INTO apps (id, user_id, name, description, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, user_id, name, description, status, prod_version, created_at, updated_at
	`

	err := s.DB.QueryRow(ctx, query,
		app.ID, app.UserID, app.Name, app.Description, app.Status, app.CreatedAt, app.UpdatedAt,
	).Scan(
		&app.ID, &app.UserID, &app.Name, &app.Description, &app.Status,
		&app.ProdVersion, &app.CreatedAt, &app.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create app: %w", err)
	}

	return &app, nil
}

// GetApp retrieves an app by ID
func (s *AppService) GetApp(ctx context.Context, appID, userID string) (*models.App, error) {
	app := &models.App{}
	query := `
		SELECT id, user_id, name, description, status, prod_version, created_at, updated_at
		FROM apps
		WHERE id = $1 AND user_id = $2
	`

	err := s.DB.QueryRow(ctx, query, appID, userID).Scan(
		&app.ID, &app.UserID, &app.Name, &app.Description, &app.Status,
		&app.ProdVersion, &app.CreatedAt, &app.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("app not found: %w", err)
	}

	return app, nil
}

// ListApps retrieves all apps for a user
func (s *AppService) ListApps(ctx context.Context, userID string) ([]models.App, error) {
	query := `
		SELECT id, user_id, name, description, status, prod_version, created_at, updated_at
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
			&app.ID, &app.UserID, &app.Name, &app.Description, &app.Status,
			&app.ProdVersion, &app.CreatedAt, &app.UpdatedAt,
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
		SELECT a.id, a.user_id, a.name, a.description, a.status, a.prod_version,
		       a.created_at, a.updated_at, u.email
		FROM apps a
		JOIN users u ON a.user_id = u.id
		WHERE a.id = $1 AND a.user_id = $2
	`

	err := s.DB.QueryRow(ctx, query, appID, userID).Scan(
		&app.ID, &app.UserID, &app.Name, &app.Description, &app.Status,
		&app.ProdVersion, &app.CreatedAt, &app.UpdatedAt, &email,
	)

	if err != nil {
		return nil, "", fmt.Errorf("app not found: %w", err)
	}

	return app, email, nil
}
