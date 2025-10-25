package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rapidbuildapp/rapidbuild/internal/db"
	"github.com/rapidbuildapp/rapidbuild/internal/models"
)

type CommentService struct {
	DB *db.PostgresClient
}

func NewCommentService(dbClient *db.PostgresClient) *CommentService {
	return &CommentService{DB: dbClient}
}

// AddComment creates a new draft comment
func (s *CommentService) AddComment(ctx context.Context, userID, appID string, req models.AddCommentRequest) (*models.Comment, error) {
	comment := models.Comment{
		ID:          uuid.New().String(),
		AppID:       appID,
		UserID:      userID,
		PagePath:    req.PagePath,
		ElementPath: req.ElementPath,
		Content:     req.Content,
		Status:      "draft",
		CreatedAt:   time.Now(),
	}

	query := `
		INSERT INTO comments (id, app_id, user_id, page_path, element_path, content, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, app_id, user_id, version_id, page_path, element_path, content, status, created_at, submitted_at
	`

	err := s.DB.QueryRow(ctx, query,
		comment.ID, comment.AppID, comment.UserID, comment.PagePath,
		comment.ElementPath, comment.Content, comment.Status, comment.CreatedAt,
	).Scan(
		&comment.ID, &comment.AppID, &comment.UserID, &comment.VersionID,
		&comment.PagePath, &comment.ElementPath, &comment.Content,
		&comment.Status, &comment.CreatedAt, &comment.SubmittedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create comment: %w", err)
	}

	return &comment, nil
}

// GetAllComments retrieves all comments (both draft and submitted) for an app
func (s *CommentService) GetAllComments(ctx context.Context, appID, userID string) ([]models.Comment, error) {
	query := `
		SELECT id, app_id, user_id, version_id, page_path, element_path, content, status, created_at, submitted_at
		FROM comments
		WHERE app_id = $1 AND user_id = $2
		ORDER BY created_at ASC
	`

	rows, err := s.DB.Query(ctx, query, appID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get all comments: %w", err)
	}
	defer rows.Close()

	var comments []models.Comment
	for rows.Next() {
		var comment models.Comment
		err := rows.Scan(
			&comment.ID, &comment.AppID, &comment.UserID, &comment.VersionID,
			&comment.PagePath, &comment.ElementPath, &comment.Content,
			&comment.Status, &comment.CreatedAt, &comment.SubmittedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan comment: %w", err)
		}
		comments = append(comments, comment)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating comments: %w", err)
	}

	return comments, nil
}

// GetDraftComments retrieves all draft comments for an app
func (s *CommentService) GetDraftComments(ctx context.Context, appID, userID string) ([]models.Comment, error) {
	query := `
		SELECT id, app_id, user_id, version_id, page_path, element_path, content, status, created_at, submitted_at
		FROM comments
		WHERE app_id = $1 AND user_id = $2 AND status = 'draft'
		ORDER BY created_at DESC
	`

	rows, err := s.DB.Query(ctx, query, appID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get draft comments: %w", err)
	}
	defer rows.Close()

	var comments []models.Comment
	for rows.Next() {
		var comment models.Comment
		err := rows.Scan(
			&comment.ID, &comment.AppID, &comment.UserID, &comment.VersionID,
			&comment.PagePath, &comment.ElementPath, &comment.Content,
			&comment.Status, &comment.CreatedAt, &comment.SubmittedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan comment: %w", err)
		}
		comments = append(comments, comment)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating comments: %w", err)
	}

	return comments, nil
}

// GetVersionComments retrieves all comments for a specific version
func (s *CommentService) GetVersionComments(ctx context.Context, versionID string) ([]models.Comment, error) {
	query := `
		SELECT id, app_id, user_id, version_id, page_path, element_path, content, status, created_at, submitted_at
		FROM comments
		WHERE version_id = $1
		ORDER BY created_at ASC
	`

	rows, err := s.DB.Query(ctx, query, versionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get version comments: %w", err)
	}
	defer rows.Close()

	var comments []models.Comment
	for rows.Next() {
		var comment models.Comment
		err := rows.Scan(
			&comment.ID, &comment.AppID, &comment.UserID, &comment.VersionID,
			&comment.PagePath, &comment.ElementPath, &comment.Content,
			&comment.Status, &comment.CreatedAt, &comment.SubmittedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan comment: %w", err)
		}
		comments = append(comments, comment)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating comments: %w", err)
	}

	return comments, nil
}

// SubmitComments submits draft comments by binding them to a version
func (s *CommentService) SubmitComments(ctx context.Context, commentIDs []string, versionID string) error {
	now := time.Now()
	query := `
		UPDATE comments
		SET version_id = $1, status = 'submitted', submitted_at = $2
		WHERE id = ANY($3) AND status = 'draft'
	`

	rowsAffected, err := s.DB.Exec(ctx, query, versionID, now, commentIDs)
	if err != nil {
		return fmt.Errorf("failed to submit comments: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no draft comments found to submit")
	}

	return nil
}

// UpdateComment updates the content of a draft comment
func (s *CommentService) UpdateComment(ctx context.Context, commentID, userID, newContent string) (*models.Comment, error) {
	query := `
		UPDATE comments
		SET content = $1
		WHERE id = $2 AND user_id = $3 AND status = 'draft'
		RETURNING id, app_id, user_id, version_id, page_path, element_path, content, status, created_at, submitted_at
	`

	var comment models.Comment
	err := s.DB.QueryRow(ctx, query, newContent, commentID, userID).Scan(
		&comment.ID, &comment.AppID, &comment.UserID, &comment.VersionID,
		&comment.PagePath, &comment.ElementPath, &comment.Content,
		&comment.Status, &comment.CreatedAt, &comment.SubmittedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to update comment: %w", err)
	}

	return &comment, nil
}

// DeleteComment deletes a draft comment
func (s *CommentService) DeleteComment(ctx context.Context, commentID, userID string) error {
	query := `DELETE FROM comments WHERE id = $1 AND user_id = $2 AND status = 'draft'`
	rowsAffected, err := s.DB.Exec(ctx, query, commentID, userID)
	if err != nil {
		return fmt.Errorf("failed to delete comment: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("comment not found or not a draft")
	}

	return nil
}
