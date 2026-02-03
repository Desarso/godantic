package common_tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:generate ../../gen_schema -func=Create_Skill_File -file=create_skill_file.go -out=../schemas/cached_schemas

// Create_Skill_File creates a new skill file in the custom skills directory.
// This is for agent-created skills that persist across deploys.
// The skill name must be unique and will be saved as a .md file.
func Create_Skill_File(name string, content string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("skill file name cannot be empty")
	}
	if content == "" {
		return "", fmt.Errorf("skill content cannot be empty")
	}

	// Ensure .md extension
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		name = name + ".md"
	}

	// Sanitize: prevent directory traversal
	name = filepath.Base(name)

	// Ensure custom skills directory exists
	if err := os.MkdirAll(CustomSkillsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create custom skills directory: %v", err)
	}

	skillPath := filepath.Join(CustomSkillsDir, name)

	// Check if file already exists
	if _, err := os.Stat(skillPath); err == nil {
		return "", fmt.Errorf("skill file %q already exists, use Edit_Skill_File to modify it", name)
	}

	// Write the skill file
	if err := os.WriteFile(skillPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write skill file %q: %v", name, err)
	}

	return fmt.Sprintf("Successfully created skill file: %s", name), nil
}
