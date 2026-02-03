package common_tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:generate ../../gen_schema -func=List_Skill_Files -file=list_skill_files.go -out=../schemas/cached_schemas

// List_Skill_Files lists all available skill markdown files in the prompts/skills directory.
// Returns a newline-separated list of filenames.
func List_Skill_Files() (string, error) {
	skillsDir := filepath.Join("prompts", "skills")

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return "", fmt.Errorf("failed to read skills directory: %v", err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(strings.ToLower(name), ".md") {
			files = append(files, name)
		}
	}

	if len(files) == 0 {
		return "(No skill files found)", nil
	}

	return strings.Join(files, "\n"), nil
}
