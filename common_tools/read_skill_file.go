package common_tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:generate ../../gen_schema -func=Read_Skill_File -file=read_skill_file.go -out=../schemas/cached_schemas

// Read_Skill_File reads and returns the contents of a skill markdown file by name.
// The name should be the filename (e.g. "browser_navigate.md").
// Checks custom skills directory first (persisted), then falls back to default skills.
func Read_Skill_File(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("skill file name cannot be empty")
	}

	// Ensure .md extension
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		name = name + ".md"
	}

	// Sanitize: prevent directory traversal
	name = filepath.Base(name)

	// Use the same directories as List_Skill_Files (includes client config dir)
	searchDirs := GetSkillsDirs()

	for _, dir := range searchDirs {
		skillPath := filepath.Join(dir, name)
		content, err := os.ReadFile(skillPath)
		if err == nil {
			if strings.TrimSpace(string(content)) == "" {
				return "(Skill file is empty)", nil
			}
			return string(content), nil
		}
		// Continue to next directory if file not found
		if !os.IsNotExist(err) {
			// Real error (not just missing file)
			return "", fmt.Errorf("failed to read skill file %q: %v", name, err)
		}
	}

	return "", fmt.Errorf("skill file %q not found", name)
}
