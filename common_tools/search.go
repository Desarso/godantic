package common_tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

//go:generate ../../gen_schema -func=Search -file=search.go -out=../schemas/cached_schemas

// Structs for Perplexity API request and response
type PerplexityMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type PerplexityRequest struct {
	Model            string              `json:"model"`
	Messages         []PerplexityMessage `json:"messages"`
	MaxTokens        int                 `json:"max_tokens,omitempty"`
	Temperature      float64             `json:"temperature,omitempty"`
	TopP             float64             `json:"top_p,omitempty"`
	Stream           bool                `json:"stream"`
	PresencePenalty  float64             `json:"presence_penalty,omitempty"`
	FrequencyPenalty float64             `json:"frequency_penalty,omitempty"`
}

type PerplexityResponseChoice struct {
	Message PerplexityMessage `json:"message"`
}

type PerplexityResponse struct {
	Choices []PerplexityResponseChoice `json:"choices"`
}

// Search is a tool to search the web using Perplexity's API
func Search(query string) (string, error) {
	// Validate query is not empty
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("search query cannot be empty")
	}

	apiKey := os.Getenv("PERPLEXITY_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("PERPLEXITY_API_KEY environment variable not set")
	}

	apiURL := "https://api.perplexity.ai/chat/completions"

	requestBody := PerplexityRequest{
		Model: "sonar",
		Messages: []PerplexityMessage{
			{Role: "system", Content: "Be precise and concise. Provide factual information from the web search results."},
			{Role: "user", Content: query},
		},
		MaxTokens:        256,
		Temperature:      0.2,
		TopP:             0.9,
		Stream:           false,
		FrequencyPenalty: 1.0,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("error marshalling request body: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request to Perplexity API: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Perplexity API request failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	var perplexityResponse PerplexityResponse
	err = json.Unmarshal(responseBody, &perplexityResponse)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling Perplexity API response: %w. Raw response: %s", err, string(responseBody))
	}

	if len(perplexityResponse.Choices) > 0 && perplexityResponse.Choices[0].Message.Content != "" {
		return perplexityResponse.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("no content found in Perplexity API response. Raw response: %s", string(responseBody))
}
