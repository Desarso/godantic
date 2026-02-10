package common_tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadFile reads the contents of a file. Supports offset (1-indexed line) and limit.
func ReadFile(filePath string, offset int, limit int) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("file_path is required")
	}

	data, err := readFileFunc(filePath)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", filePath, err)
	}

	content := string(data)

	// Apply offset/limit if specified (line-based, 1-indexed)
	if offset > 0 || limit > 0 {
		lines := strings.Split(content, "\n")
		start := 0
		if offset > 0 {
			start = offset - 1
		}
		if start > len(lines) {
			return "", nil
		}
		end := len(lines)
		if limit > 0 && start+limit < end {
			end = start + limit
		}
		content = strings.Join(lines[start:end], "\n")
	}

	// Truncate at 50KB
	const maxBytes = 50 * 1024
	if len(content) > maxBytes {
		content = content[:maxBytes] + "\n...(truncated at 50KB)"
	}

	return content, nil
}

// WriteFile writes content to a file, creating parent directories as needed.
func WriteFile(filePath string, content string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("file_path is required")
	}

	dir := filepath.Dir(filePath)
	if err := mkdirAllFunc(dir, 0755); err != nil {
		return "", fmt.Errorf("creating directory %s: %w", dir, err)
	}

	if err := writeFileFunc(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("writing %s: %w", filePath, err)
	}

	return fmt.Sprintf("Wrote %d bytes to %s", len(content), filePath), nil
}

// EditFile performs a find/replace edit on a file. oldText must match exactly.
func EditFile(filePath string, oldText string, newText string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("file_path is required")
	}
	if oldText == "" {
		return "", fmt.Errorf("old_text is required")
	}

	data, err := readFileFunc(filePath)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", filePath, err)
	}

	content := string(data)
	count := strings.Count(content, oldText)
	if count == 0 {
		return "", fmt.Errorf("old_text not found in %s", filePath)
	}
	if count > 1 {
		return "", fmt.Errorf("old_text found %d times in %s; must match exactly once", count, filePath)
	}

	newContent := strings.Replace(content, oldText, newText, 1)
	if err := writeFileFunc(filePath, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("writing %s: %w", filePath, err)
	}

	return fmt.Sprintf("Edited %s: replaced 1 occurrence", filePath), nil
}

// ListDirectory lists files and directories in a path.
func ListDirectory(dirPath string) (string, error) {
	if dirPath == "" {
		dirPath = "."
	}

	entries, err := readDirFunc(dirPath)
	if err != nil {
		return "", fmt.Errorf("listing %s: %w", dirPath, err)
	}

	var lines []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		lines = append(lines, name)
	}

	if len(lines) == 0 {
		return "(empty directory)", nil
	}
	return strings.Join(lines, "\n"), nil
}

// Mockable file system functions
var (
	readFileFunc  = os.ReadFile
	writeFileFunc = os.WriteFile
	mkdirAllFunc  = os.MkdirAll
	readDirFunc   = os.ReadDir
)
