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
	if len(tools) != 7 {
		t.Errorf("expected 7 default tools, got %d", len(tools))
	}

	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	expected := []string{"web_search", "web_fetch", "read_file", "write_file", "edit_file", "list_directory", "shell_exec"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected %s tool", name)
		}
	}
}

func TestReadFileTool(t *testing.T) {
	tool := ReadFileTool()
	if tool.Name != "read_file" || tool.Callable == nil {
		t.Error("invalid read_file tool")
	}
}

func TestWriteFileTool(t *testing.T) {
	tool := WriteFileTool()
	if tool.Name != "write_file" || tool.Callable == nil {
		t.Error("invalid write_file tool")
	}
}

func TestEditFileTool(t *testing.T) {
	tool := EditFileTool()
	if tool.Name != "edit_file" || tool.Callable == nil {
		t.Error("invalid edit_file tool")
	}
}

func TestListDirectoryTool(t *testing.T) {
	tool := ListDirectoryTool()
	if tool.Name != "list_directory" || tool.Callable == nil {
		t.Error("invalid list_directory tool")
	}
}

func TestShellExecTool(t *testing.T) {
	tool := ShellExecTool()
	if tool.Name != "shell_exec" || tool.Callable == nil {
		t.Error("invalid shell_exec tool")
	}
}
