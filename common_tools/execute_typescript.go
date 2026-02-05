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
// - 60 second execution timeout
// - No direct file system access (use skills API for skill files)
// - No process manipulation
// - Input validation in TypeScript executor
func Execute_TypeScript(code string) (string, error) {
	return Execute_TypeScriptWithTracing(code, nil)
}

// Execute_TypeScriptWithTracing executes TypeScript code and streams trace events
// If traceEmitter is nil, traces are silently discarded (backward compatible)
// If frontendHandler is nil, frontend actions from TypeScript will fail
func Execute_TypeScriptWithTracing(code string, traceEmitter TraceEmitter, frontendHandler ...FrontendActionHandler) (string, error) {
	var feHandler FrontendActionHandler
	if len(frontendHandler) > 0 {
		feHandler = frontendHandler[0]
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get the path to the TypeScript executor
	executorPath := "helpers/typescript_runtime/executor.ts"

	// Execute with Bun, passing code as argument
	cmd := exec.CommandContext(ctx, bunPath, executorPath, code)

	// Set up environment variables
	cmd.Env = os.Environ()

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
	var stderrPipe io.ReadCloser

	stderrPipe, err = cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start TypeScript executor: %v", err)
	}

	// Process stderr for traces and frontend action requests
	go processStderrWithFrontendActions(stderrPipe, traceEmitter, feHandler, stdinPipe, &stderr)

	// Wait for command to finish
	err = cmd.Wait()

	// Check for timeout
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("execution timeout: code took longer than 60 seconds")
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
				// Include any partial output that was captured
				errMsg := result.Error
				if result.Output != "" {
					errMsg = fmt.Sprintf("%s\n\nPartial output:\n%s", result.Error, result.Output)
				}
				return "", fmt.Errorf("%s", errMsg)
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

// processStderrWithFrontendActions reads stderr line by line, extracts trace events
// and frontend action requests, sends responses back via stdin
func processStderrWithFrontendActions(pipe io.ReadCloser, traceEmitter TraceEmitter, feHandler FrontendActionHandler, stdinPipe io.WriteCloser, nonTraceOutput *bytes.Buffer) {
	defer pipe.Close()
	if stdinPipe != nil {
		defer stdinPipe.Close()
	}

	scanner := bufio.NewScanner(pipe)
	const tracePrefix = "__TRACE__"
	const frontendActionRequestPrefix = "__FRONTEND_ACTION_REQUEST__"

	for scanner.Scan() {
		line := scanner.Text()

		// Check if this is a trace event
		if strings.HasPrefix(line, tracePrefix) {
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
