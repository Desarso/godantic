package common_tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

//go:generate ../../gen_schema -func=GetWeather -file=common.go -out=../schemas/cached_schemas

// A tool to get the weather in a specific location
func GetWeather(location string) (string, error) {
	return "The weather in " + location + " is sunny", nil
}

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
	// Add other fields if needed, e.g., search filters, etc.
}

type PerplexityResponseChoice struct {
	Message PerplexityMessage `json:"message"`
}

type PerplexityResponse struct {
	Choices []PerplexityResponseChoice `json:"choices"`
	// Include other fields from the response if needed, e.g., usage stats
}

//go:generate ../../gen_schema -func=Search -file=common.go -out=../schemas/cached_schemas

// A tool to search the web using Perplexity's API
func Search(query string) (string, error) {
	apiKey := os.Getenv("PERPLEXITY_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("PERPLEXITY_API_KEY environment variable not set")
	}

	apiURL := "https://api.perplexity.ai/chat/completions"

	requestBody := PerplexityRequest{
		Model: "sonar", // Using the updated online model
		Messages: []PerplexityMessage{
			{Role: "system", Content: "Be precise and concise. Provide factual information from the web search results."},
			{Role: "user", Content: query},
		},
		MaxTokens:        256, // Adjusted max tokens
		Temperature:      0.2,
		TopP:             0.9,
		Stream:           false,
		FrequencyPenalty: 1.0,
		// Add other parameters like search filters if necessary
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
	req.Header.Set("Accept", "application/json") // Added Accept header

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
		// Try to log the raw response body for debugging if unmarshalling fails
		return "", fmt.Errorf("error unmarshalling Perplexity API response: %w. Raw response: %s", err, string(responseBody))
	}

	if len(perplexityResponse.Choices) > 0 && perplexityResponse.Choices[0].Message.Content != "" {
		return perplexityResponse.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("no content found in Perplexity API response. Raw response: %s", string(responseBody))
}

//go:generate ../../gen_schema -func=Brave_Search -file=common.go -out=../schemas/cached_schemas

// A tool to search the web using Brave Search API
func Brave_Search(query string) (string, error) {
	apiKey := os.Getenv("BRAVE_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("BRAVE_API_KEY environment variable not set")
	}

	apiURL := "https://api.search.brave.com/res/v1/web/search"

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}

	// Set query parameters
	q := req.URL.Query()
	q.Add("q", query)
	req.URL.RawQuery = q.Encode()

	// Set headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request to Brave Search API: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Brave Search API request failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	var result SimplifiedResultData
	err = json.Unmarshal(responseBody, &result)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling Brave Search API response: %w", err)
	}

	resultString := FormatResultsAsText(result)

	// Marshal the result back into JSON for saving

	// Save the JSON data to file
	err = os.WriteFile("brave_search_result.txt", []byte(resultString), 0644)
	if err != nil {
		// Handle file writing error
		fmt.Printf("Error writing brave search result to file: %v\n", err)
		// return "", fmt.Errorf("error writing brave search result to file: %w", err)
	}

	// Return the raw JSON response as a string
	// You might want to parse this JSON later depending on your needs
	return resultString, nil
}

// Helper function to remove specific known HTML tags (keeps only strong for now)
func stripStrongTags(s string) string {
	s = strings.ReplaceAll(s, "<strong>", "")
	s = strings.ReplaceAll(s, "</strong>", "")
	// Add more replacements here if other simple tags appear
	// s = strings.ReplaceAll(s, "<em>", "")
	// s = strings.ReplaceAll(s, "</em>", "")
	// s = strings.ReplaceAll(s, "<b>", "")
	// s = strings.ReplaceAll(s, "</b>", "")
	return s
}

// FormatResultsAsText converts the simplified search result struct into a readable text format,
// stripping known HTML tags from titles and descriptions.
func FormatResultsAsText(searchResult SimplifiedResultData) string {
	var builder strings.Builder

	// Add the query
	builder.WriteString(fmt.Sprintf("Search Query: %s\n\n", searchResult.Query.Original))

	// --- Format Web Results ---
	builder.WriteString("Web Search Results:\n\n")
	if len(searchResult.Web.Results) == 0 {
		builder.WriteString("  No web results found.\n")
	} else {
		for i, webResult := range searchResult.Web.Results {
			// --- Strip tags from Title and Description ---
			cleanTitle := stripStrongTags(webResult.Title)
			cleanDescription := stripStrongTags(webResult.Description) // Strip description too
			// ---------------------------------------------
			builder.WriteString(fmt.Sprintf("%d. Title: %s\n", i+1, cleanTitle)) // Use cleanTitle
			builder.WriteString(fmt.Sprintf("   URL: %s\n", webResult.URL))
			builder.WriteString(fmt.Sprintf("   Description: %s\n", cleanDescription)) // Use cleanDescription

			// Extract source from URL
			parsedURL, err := url.Parse(webResult.URL)
			source := "Unknown"
			if err == nil {
				source = strings.TrimPrefix(parsedURL.Hostname(), "www.") // Remove www. if present
			}
			builder.WriteString(fmt.Sprintf("   Source: %s\n\n", source))

		}
	}

	// --- Format News Results ---
	builder.WriteString("\nNews Results:\n\n")
	if len(searchResult.News.Results) == 0 {
		builder.WriteString("  No news results found.\n")
	} else {
		for i, newsResult := range searchResult.News.Results {
			// --- Strip tags from Title and Description ---
			cleanTitle := stripStrongTags(newsResult.Title)
			cleanDescription := stripStrongTags(newsResult.Description) // Strip description too
			// ---------------------------------------------
			builder.WriteString(fmt.Sprintf("%d. Title: %s\n", i+1, cleanTitle)) // Use cleanTitle
			builder.WriteString(fmt.Sprintf("   URL: %s\n", newsResult.URL))
			builder.WriteString(fmt.Sprintf("   Description: %s\n", cleanDescription)) // Use cleanDescription

			// Extract source from URL
			parsedURL, err := url.Parse(newsResult.URL)
			source := "Unknown"
			if err == nil {
				source = strings.TrimPrefix(parsedURL.Hostname(), "www.") // Remove www. if present
			}
			builder.WriteString(fmt.Sprintf("   Source: %s\n", source))

			builder.WriteString("\n") // Add newline for spacing
		}
	}

	return builder.String()
}
