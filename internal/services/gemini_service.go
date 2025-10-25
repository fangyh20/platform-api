package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type GeminiService struct {
	APIKey string
}

type AppConfig struct {
	AppName      string   `json:"appName"`
	DisplayName  string   `json:"displayName"`
	RequiresAuth bool     `json:"requiresAuth"`
	AllowSignup  bool     `json:"allowSignup"`
	Category     string   `json:"category"`
	Keywords     []string `json:"keywords"`
	ColorScheme  string   `json:"colorScheme"`
}

type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func NewGeminiService(apiKey string) *GeminiService {
	return &GeminiService{
		APIKey: apiKey,
	}
}

// ExtractAppConfig uses Gemini Flash to extract app configuration from description
func (s *GeminiService) ExtractAppConfig(description string) (*AppConfig, error) {
	prompt := fmt.Sprintf(`Analyze this app description and extract configuration as JSON:

Description: "%s"

Respond with ONLY valid JSON (no markdown, no explanations):
{
  "appName": "BrandName (creative brand name, no spaces)",
  "displayName": "Display Name (branded name with proper formatting)",
  "requiresAuth": true/false,
  "allowSignup": true/false,
  "category": "productivity|social|ecommerce|content|dashboard|other",
  "keywords": ["keyword1", "keyword2", "keyword3"],
  "colorScheme": "blue|green|purple|orange|red|teal|indigo"
}

Rules:
- appName: Create a CREATIVE, MEMORABLE brand name (like "Notion", "Asana", "Trello", "Stripe", "Zapier")
  * Be creative and unique - think of it as naming a startup
  * Can be a single word, portmanteau, or invented term
  * Should be catchy, modern, and memorable
  * Must be alphanumeric only (no spaces, hyphens, or special chars except numbers)
  * Examples: "Taskly", "Flowbase", "Nexora", "Velocity", "Lumina", "Cirrus"
- displayName: The branded display name (can include proper capitalization and spacing if needed)
- requiresAuth: true if users need login/accounts, false for public sites
- allowSignup: false only if "internal", "team", "invite", or "admin" mentioned
- category: Best fit category
- keywords: 3-5 relevant keywords
- colorScheme: Pick color matching app vibe`, description)

	// Prepare request to Gemini API
	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Call Gemini Flash API
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=%s", s.APIKey)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to call Gemini API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Gemini API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var geminiResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("failed to decode Gemini response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from Gemini")
	}

	// Extract JSON from response
	responseText := geminiResp.Candidates[0].Content.Parts[0].Text

	// Clean response (remove markdown code blocks if present)
	responseText = strings.TrimSpace(responseText)
	responseText = strings.TrimPrefix(responseText, "```json")
	responseText = strings.TrimPrefix(responseText, "```")
	responseText = strings.TrimSuffix(responseText, "```")
	responseText = strings.TrimSpace(responseText)

	// Parse extracted config
	var config AppConfig
	if err := json.Unmarshal([]byte(responseText), &config); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w (response: %s)", err, responseText)
	}

	// Validate config
	if config.AppName == "" {
		config.AppName = "MyApp"
	}
	if config.DisplayName == "" {
		config.DisplayName = config.AppName
	}
	if config.Category == "" {
		config.Category = "other"
	}
	if config.ColorScheme == "" {
		config.ColorScheme = "blue"
	}
	if len(config.Keywords) == 0 {
		config.Keywords = []string{"app"}
	}

	return &config, nil
}
