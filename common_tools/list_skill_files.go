package common_tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:generate ../../gen_schema -func=List_Skill_Files -file=list_skill_files.go -out=../schemas/cached_schemas

// GetSkillsDirs returns the directories to search for skill files.
// This can be overridden by the application to use config-based paths.
var GetSkillsDirs = func() []string {
	return []string{
		filepath.Join("prompts", "skills"),     // Default skills from repo
		filepath.Join("data", "custom_skills"), // Agent-created skills (persisted volume)
	}
}

// List_Skill_Files lists all available skill markdown files from both default and custom directories.
// Returns a newline-separated list of filenames. Custom skills that override defaults are marked [custom].
// Custom-only skills (not overriding anything) are marked [custom-only].
func List_Skill_Files() (string, error) {
	var searchedDirs []string
	var errors []string

	// Track files from default dirs and custom dir separately
	defaultFiles := make(map[string]bool)
	customFiles := make(map[string]bool)

	skillsDirs := GetSkillsDirs()

	// First pass: collect all files, tracking source
	for _, skillsDir := range skillsDirs {
		isCustomDir := strings.Contains(skillsDir, "custom_skills")
		searchedDirs = append(searchedDirs, skillsDir)

		entries, err := os.ReadDir(skillsDir)
		if err != nil {
			if os.IsNotExist(err) {
				errors = append(errors, fmt.Sprintf("%s: does not exist", skillsDir))
			} else {
				errors = append(errors, fmt.Sprintf("%s: %v", skillsDir, err))
			}
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasSuffix(strings.ToLower(name), ".md") {
				if isCustomDir {
					customFiles[name] = true
				} else {
					defaultFiles[name] = true
				}
			}
		}
	}

	// Build result list with proper labels
	var files []string
	seen := make(map[string]bool)

	// Add custom files first (they have priority)
	for name := range customFiles {
		seen[name] = true
		if defaultFiles[name] {
			// Custom file overrides a default
			files = append(files, name+" [custom override]")
		} else {
			// Custom-only file
			files = append(files, name+" [custom]")
		}
	}

	// Add default files that aren't overridden
	for name := range defaultFiles {
		if !seen[name] {
			files = append(files, name)
		}
	}

	if len(files) == 0 {
		// Return debug info when no files found
		return fmt.Sprintf("(No skill files found)\nSearched: %s\nErrors: %s",
			strings.Join(searchedDirs, ", "),
			strings.Join(errors, "; ")), nil
	}

	return strings.Join(files, "\n"), nil
}
