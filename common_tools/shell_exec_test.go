package common_tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestShellExec(t *testing.T) {
	origExec := shellExecutor
	origConfig := shellConfig
	defer func() {
		shellExecutor = origExec
		shellConfig = origConfig
	}()

	shellExecutor = func(ctx context.Context, command, workdir string, env []string, timeout time.Duration) (*ShellExecResult, error) {
		return &ShellExecResult{
			ExitCode: 0,
			Stdout:   "hello output",
		}, nil
	}
	shellConfig = &ShellExecConfig{DefaultTimeout: 30}

	result, err := ShellExec("echo hello", "", 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "hello output") {
		t.Errorf("expected output, got %q", result)
	}
}

func TestShellExecEmptyCommand(t *testing.T) {
	_, err := ShellExec("", "", 0, "")
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestShellExecWithWorkdirAndTimeout(t *testing.T) {
	origExec := shellExecutor
	defer func() { shellExecutor = origExec }()

	var gotWorkdir string
	var gotTimeout time.Duration
	shellExecutor = func(ctx context.Context, command, workdir string, env []string, timeout time.Duration) (*ShellExecResult, error) {
		gotWorkdir = workdir
		gotTimeout = timeout
		return &ShellExecResult{Stdout: "ok"}, nil
	}

	_, err := ShellExec("ls", "/tmp", 60, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotWorkdir != "/tmp" {
		t.Errorf("expected /tmp, got %s", gotWorkdir)
	}
	if gotTimeout != 60*time.Second {
		t.Errorf("expected 60s, got %v", gotTimeout)
	}
}

func TestShellExecEnvParsing(t *testing.T) {
	origExec := shellExecutor
	defer func() { shellExecutor = origExec }()

	var gotEnv []string
	shellExecutor = func(ctx context.Context, command, workdir string, env []string, timeout time.Duration) (*ShellExecResult, error) {
		gotEnv = env
		return &ShellExecResult{Stdout: "ok"}, nil
	}

	_, err := ShellExec("cmd", "", 10, "FOO=bar, BAZ=qux")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotEnv) != 2 || gotEnv[0] != "FOO=bar" || gotEnv[1] != "BAZ=qux" {
		t.Errorf("unexpected env: %v", gotEnv)
	}
}

func TestShellExecSecretScrubbing(t *testing.T) {
	origExec := shellExecutor
	origConfig := shellConfig
	defer func() {
		shellExecutor = origExec
		shellConfig = origConfig
	}()

	shellExecutor = func(ctx context.Context, command, workdir string, env []string, timeout time.Duration) (*ShellExecResult, error) {
		return &ShellExecResult{
			Stdout: "key is sk-abc123secret",
		}, nil
	}
	shellConfig = &ShellExecConfig{
		DefaultTimeout: 30,
		Secrets: map[string]string{
			"API_KEY": "sk-abc123secret",
		},
	}

	result, err := ShellExec("echo key", "", 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "sk-abc123secret") {
		t.Error("secret was not scrubbed")
	}
	if !strings.Contains(result, "[REDACTED:API_KEY]") {
		t.Errorf("expected redaction marker, got %q", result)
	}
}

func TestShellExecError(t *testing.T) {
	origExec := shellExecutor
	defer func() { shellExecutor = origExec }()

	shellExecutor = func(ctx context.Context, command, workdir string, env []string, timeout time.Duration) (*ShellExecResult, error) {
		return nil, fmt.Errorf("boom")
	}

	_, err := ShellExec("bad", "", 10, "")
	if err == nil {
		t.Error("expected error")
	}
}

func TestShellExecNonZeroExit(t *testing.T) {
	origExec := shellExecutor
	defer func() { shellExecutor = origExec }()

	shellExecutor = func(ctx context.Context, command, workdir string, env []string, timeout time.Duration) (*ShellExecResult, error) {
		return &ShellExecResult{
			ExitCode: 1,
			Stderr:   "not found",
			Error:    "exit status 1",
		}, nil
	}

	result, err := ShellExec("false", "", 10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "exit code: 1") {
		t.Errorf("expected exit code in output: %q", result)
	}
}

func TestFormatExecResult(t *testing.T) {
	r := &ShellExecResult{
		ExitCode:  0,
		Stdout:    "out",
		Stderr:    "err",
		Truncated: true,
	}
	result := formatExecResult(r)
	if !strings.Contains(result, "out") || !strings.Contains(result, "STDERR: err") || !strings.Contains(result, "truncated") {
		t.Errorf("unexpected format: %q", result)
	}
}

func TestSetShellExecutor(t *testing.T) {
	origExec := shellExecutor
	defer func() { shellExecutor = origExec }()

	called := false
	SetShellExecutor(func(ctx context.Context, command, workdir string, env []string, timeout time.Duration) (*ShellExecResult, error) {
		called = true
		return &ShellExecResult{Stdout: "custom"}, nil
	})

	result, err := ShellExec("test", "", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("custom executor not called")
	}
	if !strings.Contains(result, "custom") {
		t.Error("expected custom output")
	}
}
