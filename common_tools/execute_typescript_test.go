package common_tools

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExecutorProtocolErrorIncludesExitAndBothStreams(t *testing.T) {
	err := executorProtocolError(errors.New("exit status 1"), "not JSON", "WARN ignored pnpm setting\nactual startup failure")
	for _, want := range []string{"exit status 1", "malformed JSON", "not JSON", "may include warnings", "actual startup failure"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q: %v", want, err)
		}
	}
	if got := executorProtocolError(nil, strings.Repeat("x", 9000), "").Error(); len(got) > 8500 || !strings.Contains(got, "truncated") {
		t.Fatal("diagnostics were not bounded")
	}
}

func TestExecutorStreamsProgressSeparatelyFromDiagnostics(t *testing.T) {
	var stderr, progress bytes.Buffer
	processStderrWithFrontendActions(io.NopCloser(strings.NewReader("__EXECUTION_OUTPUT__\"completed user 1\"\nwarning\n__EXECUTION_OUTPUT__\"completed user 2\"\n")), nil, nil, nil, &stderr, &progress)
	if progress.String() != "completed user 1\ncompleted user 2\n" {
		t.Fatalf("progress: %q", progress.String())
	}
	if stderr.String() != "warning\n" {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestExecutorProgressIsBounded(t *testing.T) {
	var stderr, progress bytes.Buffer
	line := "__EXECUTION_OUTPUT__\"" + strings.Repeat("x", 50000) + "\"\n"
	processStderrWithFrontendActions(io.NopCloser(strings.NewReader(line+line)), nil, nil, nil, &stderr, &progress)
	if progress.Len() > 50001 || progress.Len() == 0 {
		t.Fatalf("progress length: %d", progress.Len())
	}
}

func TestExecuteTypeScriptProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake runner uses a POSIX shell")
	}
	for _, tc := range []struct {
		name, stdout, stderr, exit, want string
		succeeds                         bool
	}{
		{"warning with valid result", `{"success":true,"output":"done"}`, "pnpm warning", "0", "done", true},
		{"startup failure", "not JSON", "pnpm warning: ignored setting; actual startup failure", "1", "exit status 1", false},
		{"missing protocol", `{}`, "warning", "0", "missing or malformed JSON", false},
		{"abnormal exit with success JSON", `{"success":true,"output":"done"}`, "startup failure", "1", "exit status 1", false},
		{"script failure with progress", `{"success":false,"error":"Line 2: broken","output":"user 1 complete"}`, "warning", "1", "Partial output:\nuser 1 complete", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cli := filepath.Join(dir, "helpers", "typescript_runtime", "node_modules", "tsx", "dist")
			if err := os.MkdirAll(cli, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cli, "cli.mjs"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\nprintf '%s' '" + tc.stdout + "'\nprintf '%s' '" + tc.stderr + "' >&2\nexit " + tc.exit + "\n"
			if err := os.WriteFile(filepath.Join(dir, "node"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir)
			t.Setenv("PATH", dir)
			output, err := Execute_TypeScript("console.log('test');")
			if tc.succeeds {
				if err != nil || output != tc.want {
					t.Fatalf("output=%q err=%v", output, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q; output=%q err=%v", tc.want, output, err)
			}
		})
	}
}
