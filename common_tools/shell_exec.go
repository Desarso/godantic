package common_tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ShellExecResult holds the result of a shell command execution.
type ShellExecResult struct {
	ExitCode  int    `json:"exit_code"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Error     string `json:"error,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// ShellExecConfig holds configuration for shell execution.
type ShellExecConfig struct {
	// Secrets maps secret names to values for output scrubbing.
	Secrets map[string]string
	// DefaultWorkDir is used when no workdir is specified.
	DefaultWorkDir string
	// DefaultTimeout in seconds.
	DefaultTimeout int
}

// ShellExecutor is the function signature for executing shell commands.
// It takes a context, command string, workdir, env vars, and timeout.
type ShellExecutor func(ctx context.Context, command, workdir string, env []string, timeout time.Duration) (*ShellExecResult, error)

// defaultShellExecutor is the package-level executor (mockable for tests).
var shellExecutor ShellExecutor = defaultExecutor

func defaultExecutor(ctx context.Context, command, workdir string, env []string, timeout time.Duration) (*ShellExecResult, error) {
	// This would typically wrap capsule.Runner. For now, return an error
	// indicating it needs to be wired up.
	return nil, fmt.Errorf("shell executor not configured; wire up capsule.Runner")
}

// SetShellExecutor allows wiring in the capsule runner at init time.
func SetShellExecutor(exec ShellExecutor) {
	shellExecutor = exec
}

var shellConfig = &ShellExecConfig{
	DefaultTimeout: 30,
}

// SetShellExecConfig sets the config for secret scrubbing etc.
func SetShellExecConfig(cfg *ShellExecConfig) {
	shellConfig = cfg
}

// ShellExec executes a shell command with optional working directory, timeout, and env vars.
func ShellExec(command string, workdir string, timeout int, env string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	actualTimeout := shellConfig.DefaultTimeout
	if timeout > 0 {
		actualTimeout = timeout
	}
	if actualTimeout <= 0 {
		actualTimeout = 30
	}

	actualWorkdir := workdir
	if actualWorkdir == "" {
		actualWorkdir = shellConfig.DefaultWorkDir
	}

	// Parse env string into slice: "KEY=VAL,KEY2=VAL2"
	var envSlice []string
	if env != "" {
		for _, pair := range strings.Split(env, ",") {
			pair = strings.TrimSpace(pair)
			if pair != "" {
				envSlice = append(envSlice, pair)
			}
		}
	}

	ctx := context.Background()
	dur := time.Duration(actualTimeout) * time.Second

	result, err := shellExecutor(ctx, command, actualWorkdir, envSlice, dur)
	if err != nil {
		return "", fmt.Errorf("exec failed: %w", err)
	}

	// Scrub secrets from output
	output := formatExecResult(result)
	if shellConfig != nil && len(shellConfig.Secrets) > 0 {
		for name, value := range shellConfig.Secrets {
			if value != "" {
				output = strings.ReplaceAll(output, value, fmt.Sprintf("[REDACTED:%s]", name))
			}
		}
	}

	return output, nil
}

func formatExecResult(r *ShellExecResult) string {
	var sb strings.Builder
	if r.Stdout != "" {
		sb.WriteString(r.Stdout)
	}
	if r.Stderr != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("STDERR: ")
		sb.WriteString(r.Stderr)
	}
	if r.Error != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("ERROR: ")
		sb.WriteString(r.Error)
	}
	if r.ExitCode != 0 {
		sb.WriteString(fmt.Sprintf("\n(exit code: %d)", r.ExitCode))
	}
	if r.Truncated {
		sb.WriteString("\n(output truncated)")
	}
	return sb.String()
}
