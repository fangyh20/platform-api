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

// AddDomainToProject adds a custom domain to a Vercel project
// Uses Vercel API v10: POST /v10/projects/{projectId}/domains
func (s *VercelService) AddDomainToProject(projectID, domainName string) error {
	url := fmt.Sprintf("https://api.vercel.com/v10/projects/%s/domains", projectID)

	reqBody := map[string]interface{}{
		"name": domainName,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
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

	body, _ := io.ReadAll(resp.Body)

	// Success
	if resp.StatusCode == 200 {
		return nil
	}

	// Domain already exists - treat as success
	if resp.StatusCode == 400 {
		bodyStr := string(body)
		if strings.Contains(bodyStr, "already exists") || strings.Contains(bodyStr, "domain_already_exists") {
			return nil
		}
	}

	// Other errors
	return fmt.Errorf("failed to add domain to project (status %d): %s", resp.StatusCode, string(body))
}

// PromoteDeployment promotes a deployment to production by updating the project's production domain
// Uses Vercel API v9: PATCH /v9/projects/{projectId}/domains/{domain}
// This assigns the production domain to the specific deployment
func (s *VercelService) PromoteDeployment(projectID, deploymentID, productionDomain string) error {
	// Update the production domain to point to this deployment
	url := fmt.Sprintf("https://api.vercel.com/v9/projects/%s/domains/%s", projectID, productionDomain)

	reqBody := map[string]interface{}{
		"gitBranch": nil,
		"redirect":  nil,
		"target":    deploymentID,
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

	body, _ := io.ReadAll(resp.Body)

	// Accept 200 as success
	if resp.StatusCode == 200 {
		return nil
	}

	// Other errors
	return fmt.Errorf("vercel domain update failed (status %d): %s", resp.StatusCode, string(body))
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
