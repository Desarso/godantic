package common_tools

import (
	"testing"
)

func TestWebSearchToolDeclaration(t *testing.T) {
	tool := WebSearchTool()
	if tool.Name != "web_search" {
		t.Errorf("expected name 'web_search', got %q", tool.Name)
	}
	if tool.Description == "" {
		t.Error("description should not be empty")
	}
	if tool.Callable == nil {
		t.Error("Callable should not be nil")
	}
	if tool.Parameters.Type != "object" {
		t.Errorf("expected object type, got %q", tool.Parameters.Type)
	}
	if _, ok := tool.Parameters.Properties["query"]; !ok {
		t.Error("expected 'query' property")
	}
	if len(tool.Parameters.Required) != 1 || tool.Parameters.Required[0] != "query" {
		t.Errorf("expected required=['query'], got %v", tool.Parameters.Required)
	}
}

func TestWebFetchToolDeclaration(t *testing.T) {
	tool := WebFetchTool()
	if tool.Name != "web_fetch" {
		t.Errorf("expected name 'web_fetch', got %q", tool.Name)
	}
	if tool.Callable == nil {
		t.Error("Callable should not be nil")
	}
	if _, ok := tool.Parameters.Properties["url"]; !ok {
		t.Error("expected 'url' property")
	}
	if _, ok := tool.Parameters.Properties["extractMode"]; !ok {
		t.Error("expected 'extractMode' property")
	}
	if _, ok := tool.Parameters.Properties["maxChars"]; !ok {
		t.Error("expected 'maxChars' property")
	}
}

func TestDefaultTools(t *testing.T) {
	tools := DefaultTools()
	if len(tools) != 2 {
		t.Errorf("expected 2 default tools, got %d", len(tools))
	}

	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if !names["web_search"] {
		t.Error("expected web_search tool")
	}
	if !names["web_fetch"] {
		t.Error("expected web_fetch tool")
	}
}
