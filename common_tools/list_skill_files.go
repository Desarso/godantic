package common_tools

import (
	"os"
	"path/filepath"
	"strings"
)

//go:generate ../../gen_schema -func=List_Skill_Files -file=list_skill_files.go -out=../schemas/cached_schemas

// Skill directories - default skills from repo + custom skills persisted in volume
var skillsDirs = []string{
	filepath.Join("prompts", "skills"),     // Default skills from repo (updated on redeploy)
	filepath.Join("data", "custom_skills"), // Agent-created skills (persisted volume)
}

// List_Skill_Files lists all available skill markdown files from both default and custom directories.
// Returns a newline-separated list of filenames (prefixed with [custom] for custom skills).
func List_Skill_Files() (string, error) {
	var files []string
	seen := make(map[string]bool)

	for i, skillsDir := range skillsDirs {
		isCustom := i == 1 // Second directory is custom skills

		entries, err := os.ReadDir(skillsDir)
		if err != nil {
			// Skip if directory doesn't exist (custom_skills may not exist yet)
			if os.IsNotExist(err) {
				continue
			}
			continue // Skip other errors too, don't fail completely
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasSuffix(strings.ToLower(name), ".md") {
				// Avoid duplicates (custom skills can override default)
				if !seen[name] {
					seen[name] = true
					if isCustom {
						files = append(files, name+" [custom]")
					} else {
						files = append(files, name)
					}
				}
			}
		}
	}

	if len(files) == 0 {
		return "(No skill files found)", nil
	}

	return strings.Join(files, "\n"), nil
}
