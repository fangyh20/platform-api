package worker

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/redis/go-redis/v9"
	"github.com/rapidbuildapp/rapidbuild/config"
	"github.com/rapidbuildapp/rapidbuild/internal/models"
	"github.com/rapidbuildapp/rapidbuild/internal/services"
)

type Builder struct {
	Config         *config.Config
	AppService     *services.AppService
	VersionService *services.VersionService
	VercelService  *services.VercelService
	S3Client       *s3.Client
	RedisClient    *redis.Client
}

func NewBuilder(cfg *config.Config, appService *services.AppService, versionService *services.VersionService, vercelService *services.VercelService, s3Client *s3.Client, redisClient *redis.Client) *Builder {
	return &Builder{
		Config:         cfg,
		AppService:     appService,
		VersionService: versionService,
		VercelService:  vercelService,
		S3Client:       s3Client,
		RedisClient:    redisClient,
	}
}

// findClaudePath attempts to locate the Claude CLI executable
func findClaudePath() string {
	// Check environment variable first
	if path := os.Getenv("CLAUDE_CLI_PATH"); path != "" {
		return path
	}

	// Try common installation paths
	commonPaths := []string{
		"/home/ubuntu/.local/bin/claude",
		"/usr/local/bin/claude",
		"/home/ubuntu/.nvm/versions/node/v22.16.0/bin/claude",
		"/usr/bin/claude",
	}

	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Return "claude" as fallback (relies on PATH)
	return "claude"
}

// BuildApp orchestrates the entire build process
func (b *Builder) BuildApp(ctx context.Context, versionID, appID, requirements string, comments []models.Comment, ownerEmail string) error {
	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			errMsg := fmt.Sprintf("Build panic: %v", r)
			log.Printf("[BuildApp] PANIC for version %s: %s\n", versionID, errMsg)
			b.VersionService.UpdateVersion(ctx, versionID, map[string]interface{}{
				"status":        "failed",
				"error_message": &errMsg,
			})
		}
	}()

	log.Printf("[BuildApp] Starting build for version %s, app %s\n", versionID, appID)

	// Update status to building immediately
	_, err := b.VersionService.UpdateVersion(ctx, versionID, map[string]interface{}{
		"status": "building",
	})
	if err != nil {
		log.Printf("[BuildApp] Warning: Failed to update status to building: %v\n", err)
	}

	// Wait 2 seconds to allow SSE clients to subscribe before sending first message
	// This prevents missing initial progress messages due to race condition
	time.Sleep(2 * time.Second)

	b.sendProgress(versionID, "building", "Starting build process...")

	// Create workspace using appID for easier troubleshooting
	workspaceDir := filepath.Join(b.Config.WorkspaceDir, appID)
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return b.handleError(ctx, versionID, "Failed to create workspace", err)
	}
	defer b.cleanup(workspaceDir)

	// Download previous version from S3 if exists, otherwise use starter code
	b.sendProgress(versionID, "building", "Setting up workspace...")
	if err := b.setupWorkspace(ctx, workspaceDir, appID); err != nil {
		return b.handleError(ctx, versionID, "Failed to setup workspace", err)
	}

	// Link Vercel project before Claude runs
	b.sendProgress(versionID, "building", "Linking Vercel project...")
	vercelProjectID, err := b.linkVercel(ctx, workspaceDir, versionID)
	if err != nil {
		return b.handleError(ctx, versionID, "Failed to link Vercel project", err)
	}

	// Store Vercel project ID in apps table
	if vercelProjectID != "" {
		updateQuery := `UPDATE apps SET vercel_project_id = $1, updated_at = $2 WHERE id = $3`
		_, err = b.VersionService.DB.Exec(ctx, updateQuery, vercelProjectID, time.Now(), appID)
		if err != nil {
			log.Printf("[Vercel] Warning: Failed to store Vercel project ID: %v\n", err)
		} else {
			log.Printf("[Vercel] Stored project ID %s for app %s\n", vercelProjectID, appID)
		}
	}

	// Prepare prompt for Claude
	prompt := b.buildPrompt(appID, requirements, comments)

	// Run Claude CLI
	b.sendProgress(versionID, "building", "Running AI code generation...")
	if err := b.runClaude(ctx, workspaceDir, prompt, versionID); err != nil {
		return b.handleError(ctx, versionID, "AI code generation failed", err)
	}

	// Build/fix retry loop (max 3 attempts)
	var buildErr error
	for attempt := 1; attempt <= 3; attempt++ {
		// Send progress update
		if attempt == 1 {
			b.sendProgress(versionID, "building", "Building with Vercel...")
		} else {
			b.sendProgress(versionID, "building", fmt.Sprintf("Retrying build (attempt %d/3)...", attempt))
		}

		// Run Vercel build
		buildErr = b.buildForVercel(ctx, workspaceDir, versionID, attempt)

		if buildErr == nil {
			// Build successful!
			log.Printf("[BuildApp] Build successful for version %s\n", versionID)
			break
		}

		// Build failed
		log.Printf("[BuildApp] Build failed (attempt %d/3): %v\n", attempt, buildErr)

		// If this was the last attempt, give up
		if attempt >= 3 {
			return b.handleError(ctx, versionID, "Build failed after 3 attempts", buildErr)
		}

		// Ask Claude to fix the errors
		b.sendProgress(versionID, "building", fmt.Sprintf("Build failed (attempt %d/3), Claude is fixing errors...", attempt))

		if err := b.fixBuildErrors(ctx, workspaceDir, versionID, buildErr.Error(), attempt); err != nil {
			return b.handleError(ctx, versionID, "Claude failed to fix build errors", err)
		}

		// Loop will retry the build
	}

	// Create app database and collections if schemas exist
	schemasDir := filepath.Join(workspaceDir, "schemas")
	if _, err := os.Stat(schemasDir); err == nil {
		b.sendProgress(versionID, "building", "Setting up database schema...")

		// Fetch app details to pass to app-manager (for logo, name, etc.)
		app, err := b.AppService.GetApp(ctx, appID, "")
		if err != nil {
			log.Printf("[BuildApp] Warning: Failed to fetch app details: %v\n", err)
			app = nil
		}

		if err := b.setupDatabase(ctx, schemasDir, appID, ownerEmail, app); err != nil {
			// Log warning but don't fail the build - database setup is optional
			log.Printf("[BuildApp] Warning: Failed to setup database for app %s: %v\n", appID, err)
		}
	}

	// Package core code
	b.sendProgress(versionID, "building", "Packaging code...")
	tarPath, err := b.packageCode(workspaceDir)
	if err != nil {
		return b.handleError(ctx, versionID, "Failed to package code", err)
	}

	// Upload to S3
	b.sendProgress(versionID, "building", "Uploading to S3...")
	s3Path, err := b.uploadToS3(ctx, tarPath, appID, versionID)
	if err != nil {
		return b.handleError(ctx, versionID, "Failed to upload to S3", err)
	}

	// Update version with S3 path
	_, err = b.VersionService.UpdateVersion(ctx, versionID, map[string]interface{}{
		"s3_code_path": s3Path,
	})
	if err != nil {
		return b.handleError(ctx, versionID, "Failed to update S3 path", err)
	}

	// Deploy to Vercel (workspace is pre-built by Claude)
	b.sendProgress(versionID, "building", "Deploying to Vercel...")
	vercelURL, vercelDeployID, err := b.deployToVercel(ctx, workspaceDir, appID, versionID)
	if err != nil {
		return b.handleError(ctx, versionID, "Failed to deploy to Vercel", err)
	}

	// Disable Vercel deployment protection to make it publicly accessible
	if b.VercelService != nil {
		projectID, err := b.getVercelProjectID(workspaceDir)
		if err != nil {
			log.Printf("[Vercel] Warning: Could not read project ID to disable protection: %v\n", err)
		} else {
			log.Printf("[Vercel] Disabling deployment protection for project %s\n", projectID)
			if err := b.VercelService.DisableDeploymentProtection(projectID); err != nil {
				// Log but don't fail the build - this is not critical
				log.Printf("[Vercel] Warning: Failed to disable deployment protection: %v\n", err)
			} else {
				log.Printf("[Vercel] ✅ Deployment protection disabled\n")
			}
		}
	}

	// Update version with Vercel URL
	_, err = b.VersionService.UpdateVersion(ctx, versionID, map[string]interface{}{
		"vercel_url":       vercelURL,
		"vercel_deploy_id": vercelDeployID,
	})
	if err != nil {
		return b.handleError(ctx, versionID, "Failed to update Vercel URL", err)
	}

	b.sendProgress(versionID, "completed", "Build completed successfully!")

	// Mark version as completed
	_, err = b.VersionService.UpdateVersion(ctx, versionID, map[string]interface{}{
		"status": "completed",
	})
	if err != nil {
		log.Printf("[BuildApp] ERROR updating completion status for version %s: %v\n", versionID, err)
		return b.handleError(ctx, versionID, "Failed to mark as completed", err)
	}

	// Update app status to active
	_, err = b.AppService.UpdateApp(ctx, appID, "", map[string]interface{}{
		"status": "active",
	})
	if err != nil {
		log.Printf("[BuildApp] Warning: Failed to update app status for app %s: %v\n", appID, err)
		// Don't fail the build if app status update fails
	}

	log.Printf("[BuildApp] ✅ Build completed successfully for version %s\n", versionID)
	return nil
}

func (b *Builder) setupWorkspace(ctx context.Context, workspaceDir, appID string) error {
	// Try to get the latest version's code from S3
	versions, err := b.VersionService.ListVersions(ctx, appID)
	if err != nil || len(versions) == 0 {
		// No previous version, copy starter code
		return b.copyStarterCode(workspaceDir)
	}

	// Find the latest completed version
	var latestVersion *models.Version
	for i := len(versions) - 1; i >= 0; i-- {
		if versions[i].Status == "completed" && versions[i].S3CodePath != nil && *versions[i].S3CodePath != "" {
			latestVersion = &versions[i]
			break
		}
	}

	if latestVersion == nil {
		return b.copyStarterCode(workspaceDir)
	}

	// Download from S3 and extract
	return b.downloadFromS3(ctx, *latestVersion.S3CodePath, workspaceDir)
}

func (b *Builder) copyStarterCode(workspaceDir string) error {
	// Use rsync to exclude heavy directories like node_modules, .vercel, .agent-history
	cmd := exec.Command("rsync", "-av",
		"--exclude=node_modules",
		"--exclude=.vercel",
		"--exclude=.agent-history",
		"--exclude=dist",
		"--exclude=.git",
		"--exclude=.next",
		b.Config.StarterCodeDir+"/",
		workspaceDir+"/",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to copy starter code: %w, output: %s", err, string(output))
	}
	return nil
}

func (b *Builder) buildPrompt(appID, requirements string, comments []models.Comment) string {
	var sb strings.Builder

	// Add app ID for configuration
	sb.WriteString("## App Configuration\n")
	sb.WriteString(fmt.Sprintf("App ID: %s\n", appID))
	sb.WriteString("IMPORTANT: Configure the RapidBuildProvider with this appId in src/App.jsx:\n")
	sb.WriteString(fmt.Sprintf("<RapidBuildProvider appId=\"%s\">\n\n", appID))

	if requirements != "" {
		sb.WriteString("## Requirements\n")
		sb.WriteString(requirements)
		sb.WriteString("\n\n")
	}

	if len(comments) > 0 {
		sb.WriteString("## User Comments\n")
		for _, comment := range comments {
			sb.WriteString(fmt.Sprintf("Page: %s\n", comment.PagePath))
			sb.WriteString(fmt.Sprintf("Element: %s\n", comment.ElementPath))
			sb.WriteString(fmt.Sprintf("Comment: %s\n\n", comment.Content))
		}
	}

	return sb.String()
}

func (b *Builder) runClaude(ctx context.Context, workspaceDir, prompt, versionID string) error {
	// Create context with timeout (6 hours for build)
	claudeCtx, cancel := context.WithTimeout(ctx, 360*time.Minute)
	defer cancel()

	// Get Claude CLI path
	claudePath := findClaudePath()

	// Build command with proper shell execution
	// Using bash -c to handle complex prompts
	cmd := exec.CommandContext(claudeCtx, "bash", "-c", fmt.Sprintf(
		"cd %s && %s -p --dangerously-skip-permissions %q",
		workspaceDir,
		claudePath,
		prompt,
	))

	// Set environment variables for PATH
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CLAUDE_CLI_PATH=%s", claudePath),
		"PATH=/home/ubuntu/.local/bin:/home/ubuntu/.nvm/versions/node/v22.16.0/bin:/usr/bin:/usr/local/bin:/sbin:/bin",
	)

	// Capture output separately
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute command
	err := cmd.Run()

	// Combine output for logging
	combinedOutput := stdout.String()
	if stderr.Len() > 0 {
		combinedOutput += "\n--- STDERR ---\n" + stderr.String()
	}

	// Update build log in database
	b.VersionService.UpdateVersion(ctx, versionID, map[string]interface{}{
		"build_log": combinedOutput,
	})

	if err != nil {
		// Check if context was cancelled
		if claudeCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("Claude execution timed out after 6 hours")
		}

		// Extract meaningful error message
		errorMsg := stderr.String()
		if errorMsg == "" {
			errorMsg = err.Error()
		}

		return fmt.Errorf("Claude execution failed: %s", strings.TrimSpace(errorMsg))
	}

	return nil
}

// buildForVercel runs vercel build to create the prebuilt output
func (b *Builder) buildForVercel(ctx context.Context, workspaceDir, versionID string, attempt int) error {
	// Create context with timeout (10 minutes for build)
	buildCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	log.Printf("[Vercel Build] Building project for version %s (attempt %d/3)\n", versionID, attempt)

	cmd := exec.CommandContext(buildCtx, "bash", "-c", fmt.Sprintf(
		"cd %s && vercel build --target=preview -y",
		workspaceDir,
	))

	cmd.Env = append(os.Environ(),
		"PATH=/home/ubuntu/.nvm/versions/node/v22.16.0/bin:/usr/bin:/usr/local/bin:/sbin:/bin",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Combine output for logging
	combinedOutput := stdout.String()
	if stderr.Len() > 0 {
		combinedOutput += "\n--- BUILD ERRORS ---\n" + stderr.String()
	}

	if err != nil {
		if buildCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("Vercel build timed out after 10 minutes")
		}

		// Return detailed error with full output
		errorMsg := strings.TrimSpace(combinedOutput)
		if errorMsg == "" {
			errorMsg = err.Error()
		}
		return fmt.Errorf("%s", errorMsg)
	}

	log.Printf("[Vercel Build] Build successful for version %s\n", versionID)
	return nil
}

// fixBuildErrors runs Claude to fix build errors
func (b *Builder) fixBuildErrors(ctx context.Context, workspaceDir, versionID string, buildError string, attempt int) error {
	log.Printf("[Claude Fix] Asking Claude to fix build errors (attempt %d/3)\n", attempt)

	// Create context with timeout (6 hours for fix, same as initial build)
	claudeCtx, cancel := context.WithTimeout(ctx, 360*time.Minute)
	defer cancel()

	// Get Claude CLI path
	claudePath := findClaudePath()

	// Build error fix prompt
	fixPrompt := fmt.Sprintf(`BUILD FAILED (Attempt %d/3):

%s

Please analyze the errors above and fix them. Focus on:
- Syntax errors
- Type errors
- Import/export issues
- Missing dependencies
- Build configuration issues

Fix the issues directly in the code.`, attempt, buildError)

	// Build command
	cmd := exec.CommandContext(claudeCtx, "bash", "-c", fmt.Sprintf(
		"cd %s && %s -c -p --dangerously-skip-permissions %q",
		workspaceDir,
		claudePath,
		fixPrompt,
	))

	// Set environment variables for PATH
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CLAUDE_CLI_PATH=%s", claudePath),
		"PATH=/home/ubuntu/.local/bin:/home/ubuntu/.nvm/versions/node/v22.16.0/bin:/usr/bin:/usr/local/bin:/sbin:/bin",
	)

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute command
	err := cmd.Run()

	// Combine output for logging
	combinedOutput := stdout.String()
	if stderr.Len() > 0 {
		combinedOutput += "\n--- STDERR ---\n" + stderr.String()
	}

	// Append fix attempt to build log
	b.VersionService.UpdateVersion(ctx, versionID, map[string]interface{}{
		"build_log": combinedOutput,
	})

	if err != nil {
		if claudeCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("Claude fix timed out after 6 hours")
		}

		errorMsg := stderr.String()
		if errorMsg == "" {
			errorMsg = err.Error()
		}

		return fmt.Errorf("Claude failed to fix errors: %s", strings.TrimSpace(errorMsg))
	}

	log.Printf("[Claude Fix] Claude completed fix attempt %d\n", attempt)
	return nil
}

func (b *Builder) packageCode(workspaceDir string) (string, error) {
	tarPath := workspaceDir + ".tar.gz"

	file, err := os.Create(tarPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	gzw := gzip.NewWriter(file)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	// Directories to exclude from packaging
	excludeDirs := map[string]bool{
		"node_modules":   true,
		".vercel":        true,
		".agent-history": true,
		"dist":           true,
		".git":           true,
		".next":          true,
	}

	// Walk the workspace and add files to tar
	return tarPath, filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the workspace dir itself
		if path == workspaceDir {
			return nil
		}

		// Get relative path for checking
		relPath, err := filepath.Rel(workspaceDir, path)
		if err != nil {
			return err
		}

		// Skip excluded directories
		parts := strings.Split(relPath, string(filepath.Separator))
		if len(parts) > 0 && excludeDirs[parts[0]] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		// Set relative path (already calculated above)
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// If it's a file, write its contents
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(tw, file)
			return err
		}

		return nil
	})
}

func (b *Builder) uploadToS3(ctx context.Context, tarPath, appID, versionID string) (string, error) {
	file, err := os.Open(tarPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	key := fmt.Sprintf("apps/%s/versions/%s/code.tar.gz", appID, versionID)

	_, err = b.S3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(b.Config.S3Bucket),
		Key:    aws.String(key),
		Body:   file,
	})

	return key, err
}

func (b *Builder) downloadFromS3(ctx context.Context, s3Path, workspaceDir string) error {
	result, err := b.S3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.Config.S3Bucket),
		Key:    aws.String(s3Path),
	})
	if err != nil {
		return err
	}
	defer result.Body.Close()

	// Extract tar.gz
	gzr, err := gzip.NewReader(result.Body)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(workspaceDir, header.Name)

		if header.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
		} else {
			file, err := os.Create(target)
			if err != nil {
				return err
			}
			io.Copy(file, tr)
			file.Close()
		}
	}

	return nil
}

func (b *Builder) cleanup(workspaceDir string) {
	os.RemoveAll(workspaceDir)
	os.Remove(workspaceDir + ".tar.gz")
}

// getVercelProjectID reads the project ID from .vercel/project.json
func (b *Builder) getVercelProjectID(workspaceDir string) (string, error) {
	projectFile := filepath.Join(workspaceDir, ".vercel", "project.json")
	data, err := os.ReadFile(projectFile)
	if err != nil {
		return "", fmt.Errorf("failed to read project.json: %w", err)
	}

	var projectData struct {
		ProjectID string `json:"projectId"`
	}
	if err := json.Unmarshal(data, &projectData); err != nil {
		return "", fmt.Errorf("failed to parse project.json: %w", err)
	}

	return projectData.ProjectID, nil
}

// linkVercel links the workspace to a Vercel project and returns the project ID
func (b *Builder) linkVercel(ctx context.Context, workspaceDir, versionID string) (string, error) {
	// Create context with timeout (2 minutes for link)
	linkCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	log.Printf("[Vercel] Linking project for version %s\n", versionID)

	cmd := exec.CommandContext(linkCtx, "bash", "-c", fmt.Sprintf(
		"cd %s && vercel link -y",
		workspaceDir,
	))

	cmd.Env = append(os.Environ(),
		"PATH=/home/ubuntu/.nvm/versions/node/v22.16.0/bin:/usr/bin:/usr/local/bin:/sbin:/bin",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if linkCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("Vercel link timed out after 2 minutes")
		}
		errorMsg := stderr.String()
		if errorMsg == "" {
			errorMsg = err.Error()
		}
		return "", fmt.Errorf("Vercel link failed: %s", strings.TrimSpace(errorMsg))
	}

	log.Printf("[Vercel] Link output: %s\n", stdout.String())

	// Read the project ID from .vercel/project.json
	projectID, err := b.getVercelProjectID(workspaceDir)
	if err != nil {
		log.Printf("[Vercel] Warning: Could not read project ID: %v\n", err)
		return "", nil
	}

	log.Printf("[Vercel] Project ID: %s\n", projectID)
	return projectID, nil
}

// deployToVercel deploys the pre-built workspace to Vercel
func (b *Builder) deployToVercel(ctx context.Context, workspaceDir, appID, versionID string) (string, string, error) {
	// Create context with timeout (10 minutes for deployment)
	deployCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	// Set environment variables for PATH
	envVars := append(os.Environ(),
		"PATH=/home/ubuntu/.nvm/versions/node/v22.16.0/bin:/usr/bin:/usr/local/bin:/sbin:/bin",
	)

	// Deploy to Vercel with --prebuilt flag (workspace is already built by Claude)
	log.Printf("[Vercel] Deploying version %s\n", versionID)
	cmd := exec.CommandContext(deployCtx, "bash", "-c", fmt.Sprintf(
		"cd %s && vercel --yes --prebuilt --target=preview",
		workspaceDir,
	))
	cmd.Env = envVars

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute deployment
	err := cmd.Run()

	if err != nil {
		// Check if context was cancelled
		if deployCtx.Err() == context.DeadlineExceeded {
			return "", "", fmt.Errorf("Vercel deployment timed out after 10 minutes")
		}

		// Extract error message
		errorMsg := stderr.String()
		if errorMsg == "" {
			errorMsg = err.Error()
		}
		return "", "", fmt.Errorf("Vercel deployment failed: %s", strings.TrimSpace(errorMsg))
	}

	// Parse deployment URL from output
	// Vercel typically outputs the URL in the format: https://project-name-xxx.vercel.app
	deploymentURL := ""
	outputLines := strings.Split(stdout.String(), "\n")
	for _, line := range outputLines {
		if strings.Contains(line, "https://") && strings.Contains(line, "vercel.app") {
			// Extract URL from the line
			parts := strings.Fields(line)
			for _, part := range parts {
				if strings.HasPrefix(part, "https://") && strings.Contains(part, "vercel.app") {
					deploymentURL = strings.TrimSpace(part)
					break
				}
			}
			if deploymentURL != "" {
				break
			}
		}
	}

	// Fallback to generating URL if parsing failed
	if deploymentURL == "" {
		folderName := filepath.Base(workspaceDir)
		deploymentURL = fmt.Sprintf("https://%s.vercel.app", folderName)
		log.Printf("[Vercel] Could not parse URL from output, using fallback: %s\n", deploymentURL)
	}

	log.Printf("[Vercel] Deployment successful: %s\n", deploymentURL)

	// Get deployment ID from Vercel API using the URL
	deploymentID, err := b.VercelService.GetDeploymentIDByURL(deploymentURL)
	if err != nil {
		log.Printf("[Vercel] Warning: Could not fetch deployment ID from API: %v, using versionID as fallback\n", err)
		deploymentID = versionID
	} else {
		log.Printf("[Vercel] Retrieved deployment ID from API: %s\n", deploymentID)
	}

	return deploymentURL, deploymentID, nil
}

func (b *Builder) sendProgress(versionID, status, message string) {
	// Check if Redis is configured
	if b.RedisClient == nil {
		log.Printf("[Redis] Warning: RedisClient is nil, cannot send progress for version %s\n", versionID)
		return
	}

	progress := models.BuildProgress{
		VersionID: versionID,
		Status:    status,
		Message:   message,
		Timestamp: time.Now(),
	}

	// Publish to Redis channel for this version
	data, err := json.Marshal(progress)
	if err != nil {
		log.Printf("[Redis] Failed to marshal progress: %v\n", err)
		return
	}

	channel := fmt.Sprintf("build:progress:%s", versionID)
	err = b.RedisClient.Publish(context.Background(), channel, data).Err()
	if err != nil {
		log.Printf("[Redis] Failed to publish progress: %v\n", err)
	}
}

func (b *Builder) handleError(ctx context.Context, versionID, message string, err error) error {
	fullMsg := fmt.Sprintf("%s: %v", message, err)
	log.Printf("[BuildApp] ERROR for version %s: %s\n", versionID, fullMsg)
	b.sendProgress(versionID, "failed", fullMsg)

	errMsg := err.Error()
	_, updateErr := b.VersionService.UpdateVersion(ctx, versionID, map[string]interface{}{
		"status":        "failed",
		"error_message": &errMsg,
	})
	if updateErr != nil {
		log.Printf("[BuildApp] Failed to update version with error: %v\n", updateErr)
	}

	// Get the app ID from the version
	version, getErr := b.VersionService.GetVersion(ctx, versionID)
	if getErr == nil {
		// Update app status to error
		_, appErr := b.AppService.UpdateApp(ctx, version.AppID, "", map[string]interface{}{
			"status": "error",
		})
		if appErr != nil {
			log.Printf("[BuildApp] Warning: Failed to update app status: %v\n", appErr)
		}
	}

	return fmt.Errorf(fullMsg)
}

// setupDatabase updates database schemas using app-manager CLI
// Note: App already exists in MongoDB (created in CreateApp), this only updates schemas
func (b *Builder) setupDatabase(ctx context.Context, schemasDir, appID, ownerEmail string, app *models.App) error {
	log.Printf("[Database] Updating schemas for app %s with schemas from %s\n", appID, schemasDir)

	// Create context with timeout (2 minutes for database setup)
	dbCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Use schemas command to update only schemas (app already exists from CreateApp)
	args := []string{"schemas", appID, "--dir", schemasDir}

	// Run app-manager schemas command
	// This only updates collection schemas, app already exists
	cmd := exec.CommandContext(dbCtx, "app-manager", args...)

	// Set environment variables (include pnpm path where app-manager is installed)
	cmd.Env = append(os.Environ(),
		"PATH=/home/ubuntu/.local/share/pnpm:/usr/local/bin:/usr/bin:/bin",
	)

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute command
	err := cmd.Run()

	// Log output
	if stdout.Len() > 0 {
		log.Printf("[Database] Output: %s\n", stdout.String())
	}

	if err != nil {
		if dbCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("database setup timed out after 2 minutes")
		}

		errorMsg := stderr.String()
		if errorMsg == "" {
			errorMsg = err.Error()
		}
		return fmt.Errorf("app-manager failed: %s", strings.TrimSpace(errorMsg))
	}

	log.Printf("[Database] ✅ Database setup completed for app %s\n", appID)
	return nil
}
