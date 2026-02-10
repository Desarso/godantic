package common_tools

import (
	"github.com/Desarso/godantic/models"
)

// WebSearchTool returns a FunctionDeclaration for the Brave Search tool.
func WebSearchTool() models.FunctionDeclaration {
	return models.FunctionDeclaration{
		Name:        "web_search",
		Description: "Search the web using Brave Search API. Returns titles, URLs, and snippets.",
		Parameters: models.Parameters{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query string",
				},
			},
			Required: []string{"query"},
		},
		Callable: Brave_Search,
	}
}

// WebFetchTool returns a FunctionDeclaration for the URL fetch tool.
func WebFetchTool() models.FunctionDeclaration {
	return models.FunctionDeclaration{
		Name:        "web_fetch",
		Description: "Fetch and extract readable content from a URL. Converts HTML to markdown or plain text.",
		Parameters: models.Parameters{
			Type: "object",
			Properties: map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "HTTP or HTTPS URL to fetch",
				},
				"extractMode": map[string]interface{}{
					"type":        "string",
					"description": "Extraction mode: 'markdown' or 'text'. Default: markdown",
					"enum":        []string{"markdown", "text"},
				},
				"maxChars": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum characters to return (0 = no limit)",
				},
			},
			Required: []string{"url"},
		},
		Callable: Web_Fetch,
	}
}

// DefaultTools returns the standard set of web tools for FastClaw.
func DefaultTools() []models.FunctionDeclaration {
	return []models.FunctionDeclaration{
		WebSearchTool(),
		WebFetchTool(),
	}
}
