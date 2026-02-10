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

// ReadFileTool returns a FunctionDeclaration for reading files.
func ReadFileTool() models.FunctionDeclaration {
	return models.FunctionDeclaration{
		Name:        "read_file",
		Description: "Read the contents of a file. Supports offset (1-indexed line number) and limit for large files.",
		Parameters: models.Parameters{
			Type: "object",
			Properties: map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to read",
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "Line number to start reading from (1-indexed)",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of lines to read",
				},
			},
			Required: []string{"file_path"},
		},
		Callable: ReadFile,
	}
}

// WriteFileTool returns a FunctionDeclaration for writing files.
func WriteFileTool() models.FunctionDeclaration {
	return models.FunctionDeclaration{
		Name:        "write_file",
		Description: "Write content to a file. Creates the file and parent directories if they don't exist.",
		Parameters: models.Parameters{
			Type: "object",
			Properties: map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to write",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Content to write to the file",
				},
			},
			Required: []string{"file_path", "content"},
		},
		Callable: WriteFile,
	}
}

// EditFileTool returns a FunctionDeclaration for editing files with find/replace.
func EditFileTool() models.FunctionDeclaration {
	return models.FunctionDeclaration{
		Name:        "edit_file",
		Description: "Edit a file by replacing exact text. The old_text must match exactly once in the file.",
		Parameters: models.Parameters{
			Type: "object",
			Properties: map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to edit",
				},
				"old_text": map[string]interface{}{
					"type":        "string",
					"description": "Exact text to find and replace",
				},
				"new_text": map[string]interface{}{
					"type":        "string",
					"description": "New text to replace the old text with",
				},
			},
			Required: []string{"file_path", "old_text", "new_text"},
		},
		Callable: EditFile,
	}
}

// ListDirectoryTool returns a FunctionDeclaration for listing directory contents.
func ListDirectoryTool() models.FunctionDeclaration {
	return models.FunctionDeclaration{
		Name:        "list_directory",
		Description: "List files and directories in a path.",
		Parameters: models.Parameters{
			Type: "object",
			Properties: map[string]interface{}{
				"dir_path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the directory to list (default: current directory)",
				},
			},
			Required: []string{},
		},
		Callable: ListDirectory,
	}
}

// ShellExecTool returns a FunctionDeclaration for shell command execution.
func ShellExecTool() models.FunctionDeclaration {
	return models.FunctionDeclaration{
		Name:        "shell_exec",
		Description: "Execute a shell command. Supports timeout, working directory, and environment variables. Secret values are automatically scrubbed from output.",
		Parameters: models.Parameters{
			Type: "object",
			Properties: map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Shell command to execute",
				},
				"workdir": map[string]interface{}{
					"type":        "string",
					"description": "Working directory for the command",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": "Timeout in seconds (default: 30)",
				},
				"env": map[string]interface{}{
					"type":        "string",
					"description": "Comma-separated environment variables (KEY=VAL,KEY2=VAL2)",
				},
			},
			Required: []string{"command"},
		},
		Callable: ShellExec,
	}
}

// DefaultTools returns the standard set of tools for FastClaw.
func DefaultTools() []models.FunctionDeclaration {
	return []models.FunctionDeclaration{
		WebSearchTool(),
		WebFetchTool(),
		ReadFileTool(),
		WriteFileTool(),
		EditFileTool(),
		ListDirectoryTool(),
		ShellExecTool(),
		ImageAnalysisTool(),
	}
}
