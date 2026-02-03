package common_tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:generate ../../gen_schema -func=Delete_Skill_File -file=delete_skill_file.go -out=../schemas/cached_schemas

// Delete_Skill_File deletes a skill file from the custom skills directory.
// IMPORTANT: This can ONLY delete skills from the custom skills directory (data/custom_skills/).
// It CANNOT delete default/system skills - those are protected.
func Delete_Skill_File(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("skill file name cannot be empty")
	}

	// Ensure .md extension
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		name = name + ".md"
	}

	// Sanitize: prevent directory traversal
	name = filepath.Base(name)

	// Only allow deletion from custom skills directory
	skillPath := filepath.Join(CustomSkillsDir, name)

	// Check if file exists in custom skills directory
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		return "", fmt.Errorf("skill file %q not found in custom skills directory (only agent-created skills can be deleted)", name)
	}

	// Delete the file
	if err := os.Remove(skillPath); err != nil {
		return "", fmt.Errorf("failed to delete skill file %q: %v", name, err)
	}

	return fmt.Sprintf("Successfully deleted custom skill file: %s", name), nil
}
