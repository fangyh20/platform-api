package services

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/rapidbuildapp/rapidbuild/config"
	"github.com/rapidbuildapp/rapidbuild/internal/db"
	"github.com/rapidbuildapp/rapidbuild/internal/models"
)

type UploadService struct {
	DB       *db.PostgresClient
	S3Client *s3.Client
	Config   *config.Config
}

func NewUploadService(dbClient *db.PostgresClient, s3Client *s3.Client, cfg *config.Config) *UploadService {
	return &UploadService{
		DB:       dbClient,
		S3Client: s3Client,
		Config:   cfg,
	}
}

// UploadRequirementFile uploads a requirement file to S3 and stores metadata
func (s *UploadService) UploadRequirementFile(
	ctx context.Context,
	appID, versionID string,
	fileHeader *multipart.FileHeader,
) (*models.RequirementFile, error) {
	// Open the file
	file, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Generate unique file name
	ext := filepath.Ext(fileHeader.Filename)
	fileName := uuid.New().String() + ext

	// Determine file type
	fileType := "text"
	if isImageFile(ext) {
		fileType = "image"
	}

	// Upload to S3
	s3Path := fmt.Sprintf("apps/%s/versions/%s/requirements/%s", appID, versionID, fileName)

	_, err = s.S3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.Config.S3Bucket),
		Key:    aws.String(s3Path),
		Body:   file,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upload to S3: %w", err)
	}

	// Create database record
	reqFile := models.RequirementFile{
		ID:        uuid.New().String(),
		AppID:     appID,
		VersionID: versionID,
		FileName:  fileHeader.Filename,
		FileType:  fileType,
		S3Path:    s3Path,
		CreatedAt: time.Now(),
	}

	query := `
		INSERT INTO requirement_files (id, app_id, version_id, file_name, file_type, s3_path, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err = s.DB.Exec(ctx, query, reqFile.ID, reqFile.AppID, reqFile.VersionID, reqFile.FileName, reqFile.FileType, reqFile.S3Path, reqFile.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to save file metadata: %w", err)
	}

	return &reqFile, nil
}

// DownloadFile downloads a file from S3
func (s *UploadService) DownloadFile(ctx context.Context, s3Path string) (io.ReadCloser, error) {
	result, err := s.S3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.Config.S3Bucket),
		Key:    aws.String(s3Path),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to download from S3: %w", err)
	}

	return result.Body, nil
}

func isImageFile(ext string) bool {
	imageExts := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".webp": true,
		".svg":  true,
	}
	return imageExts[ext]
}
