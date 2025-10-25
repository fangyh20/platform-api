package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rapidbuildapp/rapidbuild/config"
)

type VercelService struct {
	Config *Config
	Client *http.Client
}

type Config struct {
	Token string
}

func NewVercelService(cfg *config.Config) *VercelService {
	return &VercelService{
		Config: &Config{
			Token: cfg.VercelToken,
		},
		Client: &http.Client{Timeout: 30 * time.Second},
	}
}

type VercelDeployment struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	State string `json:"state"`
}

type VercelDeploymentRequest struct {
	Name    string            `json:"name"`
	Files   []VercelFile      `json:"files"`
	Target  string            `json:"target,omitempty"`
	GitMeta map[string]string `json:"gitMetadata,omitempty"`
}

type VercelFile struct {
	File string `json:"file"`
	Data string `json:"data"` // base64 encoded
}

// Deploy creates a new Vercel deployment
func (s *VercelService) Deploy(projectName, workspacePath string) (*VercelDeployment, error) {
	// In a real implementation, you would:
	// 1. Zip the workspace
	// 2. Upload files to Vercel
	// 3. Create deployment

	// For now, this is a simplified version
	url := "https://api.vercel.com/v13/deployments"

	reqBody := VercelDeploymentRequest{
		Name:   projectName,
		Target: "preview",
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+s.Config.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("vercel deployment failed: %s", string(body))
	}

	var deployment VercelDeployment
	if err := json.Unmarshal(body, &deployment); err != nil {
		return nil, err
	}

	return &deployment, nil
}

// PromoteDeployment promotes a deployment to production
// Uses Vercel API v10: POST /v10/projects/{projectId}/promote/{deploymentId}
// This points all production domains for the project to the given deployment
func (s *VercelService) PromoteDeployment(projectID, deploymentID string) error {
	url := fmt.Sprintf("https://api.vercel.com/v10/projects/%s/promote/%s", projectID, deploymentID)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+s.Config.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Accept 201, 202 as success
	// Also accept 409 if the deployment is already in production (not an error)
	if resp.StatusCode != 201 && resp.StatusCode != 202 && resp.StatusCode != 409 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vercel promotion failed (status %d): %s", resp.StatusCode, string(body))
	}

	// If 409, check if it's "already production" which is success
	if resp.StatusCode == 409 {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if strings.Contains(bodyStr, "already the current production deployment") {
			// This is fine - deployment is already promoted
			return nil
		}
		// Other 409 errors should still be returned as errors
		return fmt.Errorf("vercel promotion conflict (status 409): %s", bodyStr)
	}

	return nil
}

// GetDeploymentIDByURL fetches deployment details by URL and returns the deployment ID
func (s *VercelService) GetDeploymentIDByURL(deploymentURL string) (string, error) {
	// Extract hostname from URL (remove https:// and path)
	hostname := strings.TrimPrefix(deploymentURL, "https://")
	hostname = strings.TrimPrefix(hostname, "http://")
	hostname = strings.Split(hostname, "/")[0]

	url := fmt.Sprintf("https://api.vercel.com/v13/deployments/%s", hostname)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+s.Config.Token)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("failed to get deployment by URL (status %d): %s", resp.StatusCode, string(body))
	}

	var deployment struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &deployment); err != nil {
		return "", fmt.Errorf("failed to parse deployment response: %w", err)
	}

	if deployment.ID == "" {
		return "", fmt.Errorf("deployment ID not found in response")
	}

	return deployment.ID, nil
}

// GetDeploymentStatus gets the status of a deployment
func (s *VercelService) GetDeploymentStatus(deploymentID string) (*VercelDeployment, error) {
	url := fmt.Sprintf("https://api.vercel.com/v13/deployments/%s", deploymentID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+s.Config.Token)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("failed to get deployment status: %s", string(body))
	}

	var deployment VercelDeployment
	if err := json.Unmarshal(body, &deployment); err != nil {
		return nil, err
	}

	return &deployment, nil
}

// DisableDeploymentProtection disables SSO/password protection for a project
func (s *VercelService) DisableDeploymentProtection(projectID string) error {
	url := fmt.Sprintf("https://api.vercel.com/v9/projects/%s", projectID)

	reqBody := map[string]interface{}{
		"ssoProtection":      nil,
		"passwordProtection": nil,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+s.Config.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to disable protection: %s", string(body))
	}

	return nil
}
