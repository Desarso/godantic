package common_tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

//go:generate ../../gen_schema -func=Execute_TypeScript -file=execute_typescript.go -out=../schemas/cached_schemas

// TypeScriptExecutionResult represents the result from the TypeScript executor
type TypeScriptExecutionResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error"`
}

// findBun attempts to locate the Bun executable
func findBun() (string, error) {
	// Try common installation paths
	bunPaths := []string{
		"bun",                                // In PATH
		os.ExpandEnv("$HOME/.bun/bin/bun"),   // Default Bun installation
		"/usr/local/bin/bun",                 // Homebrew installation
		os.ExpandEnv("$HOME/.local/bin/bun"), // Local installation
		"/opt/homebrew/bin/bun",              // M1 Mac Homebrew
	}

	for _, path := range bunPaths {
		if _, err := exec.LookPath(path); err == nil {
			return path, nil
		}
		// Also try direct file check for expanded paths
		if strings.Contains(path, "/") {
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("bun executable not found. Please install Bun: https://bun.sh/")
}

// Execute_TypeScript executes TypeScript code in a sandboxed environment using Bun
// The code is validated and executed by a separate TypeScript file with built-in safety checks
// Built-in libraries: web (HTTP requests), tavily (search), math (mathjs library), graph (Microsoft Graph API), skills (manage skill files)
// Skills API: skills.list(), skills.read(name), skills.create(name, content), skills.edit(name, old, new), skills.remove(name)
// Safety rules:
// - 30 second execution timeout
// - No direct file system access (use skills API for skill files)
// - No process manipulation
// - Input validation in TypeScript executor
func Execute_TypeScript(code string) (string, error) {
	// Basic validation
	if code == "" {
		return "", fmt.Errorf("TypeScript code cannot be empty")
	}

	// Find Bun executable
	bunPath, err := findBun()
	if err != nil {
		return "", err
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get the path to the TypeScript executor
	executorPath := "helpers/typescript_runtime/executor.ts"

	// Execute with Bun, passing code as argument
	cmd := exec.CommandContext(ctx, bunPath, executorPath, code)

	// Set up environment variables
	cmd.Env = os.Environ()

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	err = cmd.Run()

	// Check for timeout
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("execution timeout: code took longer than 30 seconds")
	}

	// Parse the JSON response
	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	// Always try to parse stdout first, even if command exited with error
	// The executor outputs JSON to stdout regardless of success/failure
	if stdoutStr != "" {
		var result TypeScriptExecutionResult
		if jsonErr := json.Unmarshal([]byte(stdoutStr), &result); jsonErr == nil {
			// Successfully parsed JSON from stdout
			if !result.Success {
				// Execution failed, return the error message from JSON
				return "", fmt.Errorf("%s", result.Error)
			}
			// Execution succeeded
			output := result.Output
			if output == "" {
				output = "(No output)"
			}
			return output, nil
		}
	}

	// If stdout parsing failed or stdout is empty, check stderr
	if stderrStr != "" {
		// Try to parse stderr as JSON
		var result TypeScriptExecutionResult
		if jsonErr := json.Unmarshal([]byte(stderrStr), &result); jsonErr == nil {
			if !result.Success {
				return "", fmt.Errorf("%s", result.Error)
			}
		}
		return "", fmt.Errorf("execution error: %s", stderrStr)
	}

	// If both stdout and stderr are empty or unparseable, return generic error
	if err != nil {
		return "", fmt.Errorf("execution failed: %v", err)
	}

	// Fallback: return raw stdout if it exists but wasn't JSON
	if stdoutStr != "" {
		return stdoutStr, nil
	}

	return "", fmt.Errorf("execution failed: no output received")
}
