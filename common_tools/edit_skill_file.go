package common_tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:generate ../../gen_schema -func=Edit_Skill_File -file=edit_skill_file.go -out=../schemas/cached_schemas

// Custom skills directory (persisted in volume)
const customSkillsDir = "data/custom_skills"

// Edit_Skill_File performs a find-and-replace in a skill markdown file.
// Replaces the first occurrence of old_text with new_text in the specified file.
// Reads from custom or default skills, but always writes to custom skills directory.
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

	// Read from either custom or default skills
	content, err := readSkillContent(name)
	if err != nil {
		return "", err
	}

	original := string(content)

	if !strings.Contains(original, old_text) {
		return "", fmt.Errorf("old_text not found in %q", name)
	}

	updated := strings.Replace(original, old_text, new_text, 1)

	// Always write to custom skills directory (persisted)
	if err := writeToCustomSkills(name, []byte(updated)); err != nil {
		return "", err
	}

	return fmt.Sprintf("Successfully edited %s (saved to custom skills)", name), nil
}

// readSkillContent reads skill content, checking custom skills first then default
func readSkillContent(name string) ([]byte, error) {
	searchDirs := []string{
		customSkillsDir,                    // Custom/persisted skills (priority)
		filepath.Join("prompts", "skills"), // Default skills from repo
	}

	for _, dir := range searchDirs {
		skillPath := filepath.Join(dir, name)
		content, err := os.ReadFile(skillPath)
		if err == nil {
			return content, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read skill file %q: %v", name, err)
		}
	}

	return nil, fmt.Errorf("skill file %q not found", name)
}

// writeToCustomSkills writes content to the custom skills directory
func writeToCustomSkills(name string, content []byte) error {
	// Ensure custom skills directory exists
	if err := os.MkdirAll(customSkillsDir, 0755); err != nil {
		return fmt.Errorf("failed to create custom skills directory: %v", err)
	}

	skillPath := filepath.Join(customSkillsDir, name)
	if err := os.WriteFile(skillPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write skill file %q: %v", name, err)
	}

	return nil
}
