package common_tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

//go:generate ../../gen_schema -func=Brave_Search -file=brave_search.go -out=../schemas/cached_schemas

// Brave_Search is a tool to search the web using Brave Search API
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

	// Save the result to file
	err = os.WriteFile("brave_search_result.txt", []byte(resultString), 0644)
	if err != nil {
		fmt.Printf("Error writing brave search result to file: %v\n", err)
	}

	return resultString, nil
}

// stripStrongTags removes specific known HTML tags from strings
func stripStrongTags(s string) string {
	s = strings.ReplaceAll(s, "<strong>", "")
	s = strings.ReplaceAll(s, "</strong>", "")
	return s
}

// FormatResultsAsText converts the simplified search result struct into a readable text format,
// stripping known HTML tags from titles and descriptions.
func FormatResultsAsText(searchResult SimplifiedResultData) string {
	var builder strings.Builder

	// Add the query
	builder.WriteString(fmt.Sprintf("Search Query: %s\n\n", searchResult.Query.Original))

	// Format Web Results
	builder.WriteString("Web Search Results:\n\n")
	if len(searchResult.Web.Results) == 0 {
		builder.WriteString("  No web results found.\n")
	} else {
		for i, webResult := range searchResult.Web.Results {
			cleanTitle := stripStrongTags(webResult.Title)
			cleanDescription := stripStrongTags(webResult.Description)
			builder.WriteString(fmt.Sprintf("%d. Title: %s\n", i+1, cleanTitle))
			builder.WriteString(fmt.Sprintf("   URL: %s\n", webResult.URL))
			builder.WriteString(fmt.Sprintf("   Description: %s\n", cleanDescription))

			// Extract source from URL
			parsedURL, err := url.Parse(webResult.URL)
			source := "Unknown"
			if err == nil {
				source = strings.TrimPrefix(parsedURL.Hostname(), "www.")
			}
			builder.WriteString(fmt.Sprintf("   Source: %s\n\n", source))
		}
	}

	// Format News Results
	builder.WriteString("\nNews Results:\n\n")
	if len(searchResult.News.Results) == 0 {
		builder.WriteString("  No news results found.\n")
	} else {
		for i, newsResult := range searchResult.News.Results {
			cleanTitle := stripStrongTags(newsResult.Title)
			cleanDescription := stripStrongTags(newsResult.Description)
			builder.WriteString(fmt.Sprintf("%d. Title: %s\n", i+1, cleanTitle))
			builder.WriteString(fmt.Sprintf("   URL: %s\n", newsResult.URL))
			builder.WriteString(fmt.Sprintf("   Description: %s\n", cleanDescription))

			// Extract source from URL
			parsedURL, err := url.Parse(newsResult.URL)
			source := "Unknown"
			if err == nil {
				source = strings.TrimPrefix(parsedURL.Hostname(), "www.")
			}
			builder.WriteString(fmt.Sprintf("   Source: %s\n", source))
			builder.WriteString("\n")
		}
	}

	return builder.String()
}
