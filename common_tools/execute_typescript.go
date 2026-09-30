package common_tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// TraceEvent represents an execution trace from the TypeScript runtime
type TraceEvent struct {
	TraceID    string                 `json:"trace_id"`
	ParentID   string                 `json:"parent_id,omitempty"`
	Tool       string                 `json:"tool"`
	Operation  string                 `json:"operation"`
	Status     string                 `json:"status"` // start, progress, end, error
	Label      string                 `json:"label"`
	Details    map[string]interface{} `json:"details,omitempty"`
	Timestamp  int64                  `json:"timestamp"`
	DurationMS int64                  `json:"duration_ms,omitempty"`
}

// TraceEmitter is an interface for emitting trace events
type TraceEmitter interface {
	EmitTrace(trace TraceEvent) error
}

// FrontendAction represents an action to be executed in the frontend browser
type FrontendAction struct {
	Action    string                 `json:"action"`
	Data      map[string]interface{} `json:"data"`
	Timestamp int64                  `json:"timestamp"`
}

// FrontendActionEmitter is an interface for emitting frontend actions via WebSocket
type FrontendActionEmitter interface {
	EmitFrontendAction(action FrontendAction) error
}

// FrontendActionHandler handles frontend actions with round-trip (waits for response)
type FrontendActionHandler interface {
	// HandleFrontendAction sends action to frontend and waits for response
	HandleFrontendAction(action FrontendAction) (response string, err error)
}

type typescriptRunner struct {
	command string
	args    []string
	dir     string
}

func findTypeScriptRunner() (typescriptRunner, error) {
	runtimeDir := ""
	for _, candidate := range []string{"helpers/typescript_runtime", "../helpers/typescript_runtime"} {
		if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
			runtimeDir = candidate
			break
		}
	}
	if runtimeDir == "" {
		return typescriptRunner{}, fmt.Errorf("TypeScript runtime directory not found")
	}

	// Invoke the installed CLI directly to avoid package-manager startup noise.
	cli := filepath.Join(runtimeDir, "node_modules", "tsx", "dist", "cli.mjs")
	if _, err := os.Stat(cli); err != nil {
		return typescriptRunner{}, fmt.Errorf("TypeScript runner not installed; run pnpm install in helpers/typescript_runtime: %w", err)
	}
	cli, err := filepath.Abs(cli)
	if err != nil {
		return typescriptRunner{}, err
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return typescriptRunner{}, fmt.Errorf("Node.js executable not found: %w", err)
	}
	return typescriptRunner{command: node, args: []string{cli}, dir: runtimeDir}, nil
}

// Execute_TypeScript executes a self-contained TypeScript snippet using Node.js/tsx.
// Configured API namespaces are available through tools and their own names; use console.log for output.
// Variables DO NOT persist between calls. Declare or fetch all inputs in this call, or load saved workspace data.
// Maximum code length: 10,000 UTF-16 code units. Fetch inventories instead of embedding large arrays; store bulk data in workspace files.
// Hard timeout: 60 seconds. Use background automations for bulk scans, provisioning polls, and long jobs, with bounded concurrency and durable checkpoints.
// A timeout may occur after mutations succeeded. Inspect progress and verify existing state before retrying writes.
// Await async entry points and API calls. Top-level assistant tools such as Confirm_With_User must be invoked separately, outside this snippet.
// TypeScript syntax is compiled before execution. Imports/exports are not supported inside snippets; use the configured API namespaces.
// Direct filesystem access and process manipulation are prohibited; use the workspace/skills APIs.
func Execute_TypeScript(code string) (string, error) {
	return Execute_TypeScriptWithTracing(code, nil)
}

// Execute_TypeScriptWithTracing executes TypeScript code and streams trace events
// If traceEmitter is nil, traces are silently discarded (backward compatible)
// If frontendHandler is nil, frontend actions from TypeScript will fail
func Execute_TypeScriptWithTracing(code string, traceEmitter TraceEmitter, frontendHandler ...FrontendActionHandler) (string, error) {
	return Execute_TypeScriptWithTracingAndEnv(code, traceEmitter, nil, frontendHandler...)
}

// Execute_TypeScriptWithTracingAndEnv executes TypeScript code with extra per-request environment variables.
func Execute_TypeScriptWithTracingAndEnv(code string, traceEmitter TraceEmitter, extraEnv map[string]string, frontendHandler ...FrontendActionHandler) (string, error) {
	var feHandler FrontendActionHandler
	if len(frontendHandler) > 0 {
		feHandler = frontendHandler[0]
	}
	// Basic validation
	if code == "" {
		return "", fmt.Errorf("TypeScript code cannot be empty")
	}

	// Find TypeScript runner
	runner, err := findTypeScriptRunner()
	if err != nil {
		return "", err
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get the path to the TypeScript executor, relative to the runtime package.
	executorPath := "executor.ts"

	// Execute with pnpm/tsx, passing code as argument
	args := append(append([]string{}, runner.args...), executorPath, code)
	cmd := exec.CommandContext(ctx, runner.command, args...)
	cmd.Dir = runner.dir

	// Set up environment variables
	cmd.Env = os.Environ()
	for key, value := range extraEnv {
		if key != "" && value != "" {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	// Capture stdout
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	// Set up stdin pipe for sending responses back to TypeScript
	var stdinPipe io.WriteCloser
	if feHandler != nil {
		stdinPipe, err = cmd.StdinPipe()
		if err != nil {
			return "", fmt.Errorf("failed to create stdin pipe: %v", err)
		}
	}

	// For stderr, we need to parse trace events and frontend action requests
	var stderr bytes.Buffer
	var progress bytes.Buffer
	var stderrPipe io.ReadCloser

	stderrPipe, err = cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start TypeScript executor: %v", err)
	}

	// Process stderr for traces and frontend action requests.
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		processStderrWithFrontendActions(stderrPipe, traceEmitter, feHandler, stdinPipe, &stderr, &progress)
	}()

	// Wait for command to finish
	err = cmd.Wait()
	<-stderrDone

	// Check for timeout
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("execution timeout: code took longer than 60 seconds. Use background automations with bounded concurrency and durable checkpoints for bulk work or provisioning polls. Mutations may already have succeeded; verify existing state before retrying.\nPartial console output:\n%s", progress.String())
	}

	// Parse the JSON response
	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	// Always try to parse stdout first, even if command exited with error
	// The executor outputs JSON to stdout regardless of success/failure
	if stdoutStr != "" {
		var result TypeScriptExecutionResult
		var fields map[string]json.RawMessage
		if jsonErr := json.Unmarshal([]byte(stdoutStr), &result); jsonErr == nil && json.Unmarshal([]byte(stdoutStr), &fields) == nil && fields["success"] != nil && string(fields["success"]) != "null" {
			// Successfully parsed JSON from stdout
			if !result.Success {
				// Execution failed, return the error message from JSON
				// Include any partial output that was captured
				errMsg := result.Error
				if result.Output != "" {
					errMsg = fmt.Sprintf("%s\n\nPartial output:\n%s", result.Error, result.Output)
				}
				return "", fmt.Errorf("%s", errMsg)
			}
			// Execution succeeded, but an abnormal child exit remains a failure.
			if err != nil {
				return "", executorProtocolError(err, stdoutStr, stderrStr)
			}
			output := result.Output
			if output == "" {
				output = "(No output)"
			}
			return output, nil
		}
	}

	return "", executorProtocolError(err, stdoutStr, stderrStr)
}

// Keep warnings distinct from the process failure and never accept raw stdout
// as a successful result when the executor protocol is absent.
func executorProtocolError(exitErr error, stdout, stderr string) error {
	status := "exit status 0"
	if exitErr != nil {
		status = exitErr.Error()
	}
	bounded := func(s string) string {
		const limit = 8000
		if len(s) > limit {
			return s[:limit] + "\n... (truncated)"
		}
		if s == "" {
			return "(empty)"
		}
		return s
	}
	return fmt.Errorf("TypeScript executor returned missing or malformed JSON (%s). Check runtime installation/startup diagnostics.\nstdout:\n%s\nstderr (may include warnings):\n%s", status, bounded(stdout), bounded(stderr))
}

// processStderrWithFrontendActions reads stderr line by line, extracts trace events
// and frontend action requests, sends responses back via stdin
func processStderrWithFrontendActions(pipe io.ReadCloser, traceEmitter TraceEmitter, feHandler FrontendActionHandler, stdinPipe io.WriteCloser, nonTraceOutput *bytes.Buffer, progress ...*bytes.Buffer) {
	defer pipe.Close()
	if stdinPipe != nil {
		defer stdinPipe.Close()
	}

	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	const tracePrefix = "__TRACE__"
	const frontendActionRequestPrefix = "__FRONTEND_ACTION_REQUEST__"

	for scanner.Scan() {
		line := scanner.Text()

		// Check if this is a trace event
		if strings.HasPrefix(line, "__EXECUTION_OUTPUT__") {
			var output string
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "__EXECUTION_OUTPUT__")), &output) == nil && len(progress) > 0 && progress[0].Len() < 50000 {
				remaining := 50000 - progress[0].Len()
				if len(output) > remaining {
					output = output[:remaining]
				}
				progress[0].WriteString(output + "\n")
			}
		} else if strings.HasPrefix(line, tracePrefix) {
			// Parse and emit the trace
			jsonStr := strings.TrimPrefix(line, tracePrefix)
			var trace TraceEvent
			if err := json.Unmarshal([]byte(jsonStr), &trace); err == nil {
				if traceEmitter != nil {
					_ = traceEmitter.EmitTrace(trace)
				}
			}
		} else if strings.HasPrefix(line, frontendActionRequestPrefix) {
			// Parse frontend action request
			jsonStr := strings.TrimPrefix(line, frontendActionRequestPrefix)
			var action FrontendAction
			if err := json.Unmarshal([]byte(jsonStr), &action); err == nil {
				// Handle the action and get response
				var response string
				var actionErr error
				if feHandler != nil {
					response, actionErr = feHandler.HandleFrontendAction(action)
				} else {
					actionErr = fmt.Errorf("frontend action handler not available")
				}

				// Send response back to TypeScript via stdin
				if stdinPipe != nil {
					resp := map[string]interface{}{
						"success": actionErr == nil,
					}
					if actionErr != nil {
						resp["error"] = actionErr.Error()
					} else {
						resp["response"] = response
					}
					respJSON, _ := json.Marshal(resp)
					stdinPipe.Write([]byte(fmt.Sprintf("__FRONTEND_ACTION_RESPONSE__%s\n", string(respJSON))))
				}
			}
		} else {
			// Not a trace or action request, collect as regular stderr output
			nonTraceOutput.WriteString(line)
			nonTraceOutput.WriteString("\n")
		}
	}
}
