package common_tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:generate ../../gen_schema -func=Edit_Skill_File -file=edit_skill_file.go -out=../schemas/cached_schemas

// Edit_Skill_File performs a find-and-replace in a skill markdown file.
// Replaces the first occurrence of old_text with new_text in the specified file.
func Edit_Skill_File(name string, old_text string, new_text string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("skill file name cannot be empty")
	}
	if old_text == "" {
		return "", fmt.Errorf("old_text cannot be empty")
	}

	// Ensure .md extension
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		name = name + ".md"
	}

	// Sanitize: prevent directory traversal
	name = filepath.Base(name)

	skillPath := filepath.Join("prompts", "skills", name)

	content, err := os.ReadFile(skillPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("skill file %q not found", name)
		}
		return "", fmt.Errorf("failed to read skill file %q: %v", name, err)
	}

	original := string(content)

	if !strings.Contains(original, old_text) {
		return "", fmt.Errorf("old_text not found in %q", name)
	}

	updated := strings.Replace(original, old_text, new_text, 1)

	if err := os.WriteFile(skillPath, []byte(updated), 0644); err != nil {
		return "", fmt.Errorf("failed to write skill file %q: %v", name, err)
	}

	return fmt.Sprintf("Successfully edited %s", name), nil
}
