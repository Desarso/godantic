package common_tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:generate ../../gen_schema -func=List_Skill_Files -file=skill_files.go -out=../schemas/cached_schemas
//go:generate ../../gen_schema -func=Read_Skill_File -file=skill_files.go -out=../schemas/cached_schemas
//go:generate ../../gen_schema -func=Edit_Skill_File -file=skill_files.go -out=../schemas/cached_schemas

const skillFilesDir = "system_prompts/skills/shared"

type skillFileEditRequest struct {
	File    string `json:"file"`
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

// List_Skill_Files lists markdown files under system_prompts/skills/shared.
// The filter argument is optional and matched as a case-insensitive substring.
func List_Skill_Files(filter string) (string, error) {
	entries, err := os.ReadDir(skillFilesDir)
	if err != nil {
		return "", fmt.Errorf("failed to read skill directory: %w", err)
	}

	filterValue := strings.ToLower(strings.TrimSpace(filter))
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		if filterValue != "" && !strings.Contains(strings.ToLower(name), filterValue) {
			continue
		}
		files = append(files, name)
	}

	sort.Strings(files)

	payload := map[string]interface{}{
		"files": files,
	}
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to serialize skill file list: %w", err)
	}

	return string(jsonBytes), nil
}

// Read_Skill_File reads a single markdown file from system_prompts/skills/shared by name.
func Read_Skill_File(file string) (string, error) {
	fileName, err := normalizeSkillFileName(file)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(filepath.Join(skillFilesDir, fileName))
	if err != nil {
		return "", fmt.Errorf("failed to read skill file: %w", err)
	}

	return string(content), nil
}

// Edit_Skill_File performs a find-and-replace on a skill file.
// The request argument must be JSON: {"file":"name.md","find":"...","replace":"..."}.
func Edit_Skill_File(request string) (string, error) {
	if strings.TrimSpace(request) == "" {
		return "", fmt.Errorf("request is required")
	}

	var payload skillFileEditRequest
	if err := json.Unmarshal([]byte(request), &payload); err != nil {
		return "", fmt.Errorf("invalid JSON request (expected {\"file\":\"name.md\",\"content\":\"...\"}): %w", err)
	}

	fileName, err := normalizeSkillFileName(payload.File)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(skillFilesDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to ensure skill directory: %w", err)
	}

	if strings.TrimSpace(payload.Find) == "" {
		return "", fmt.Errorf("find string is required")
	}

	filePath := filepath.Join(skillFilesDir, fileName)
	originalBytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read skill file: %w", err)
	}

	original := string(originalBytes)
	replaced, count := replaceAllWithCount(original, payload.Find, payload.Replace)
	if err := os.WriteFile(filePath, []byte(replaced), 0o644); err != nil {
		return "", fmt.Errorf("failed to write skill file: %w", err)
	}

	result := map[string]interface{}{
		"file":         fileName,
		"replacements": count,
		"bytes":        len(replaced),
	}
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to serialize edit result: %w", err)
	}

	return string(jsonBytes), nil
}

func normalizeSkillFileName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("file name is required")
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return "", fmt.Errorf("file name must not include path separators")
	}
	if filepath.Base(trimmed) != trimmed {
		return "", fmt.Errorf("file name must be a base name only")
	}
	if !strings.HasSuffix(strings.ToLower(trimmed), ".md") {
		trimmed += ".md"
	}
	return trimmed, nil
}

func replaceAllWithCount(input, find, replace string) (string, int) {
	if find == "" {
		return input, 0
	}
	parts := strings.Split(input, find)
	if len(parts) == 1 {
		return input, 0
	}
	return strings.Join(parts, replace), len(parts) - 1
}
