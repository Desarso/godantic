package sessions

import (
	"encoding/json"
	"fmt"
)

// ExecuteToolWithContext executes a tool with WebSocket context for frontend tools
func (as *AgentSession) ExecuteToolWithContext(functionName string, functionCallArgs map[string]interface{}) (string, error) {
	// Check if this is a frontend tool that needs WebSocket access
	isFrontendTool := functionName == "Browser_Prompt" || functionName == "Browser_Confirm" || functionName == "Browser_Alert"

	if isFrontendTool {
		// For frontend tools, inject WebSocket writer and response waiter
		return as.executeFrontendTool(functionName, functionCallArgs)
	}

	// For regular tools, use the standard agent ExecuteTool
	return as.Agent.ExecuteTool(functionName, functionCallArgs, as.SessionID)
}

// executeFrontendTool executes a frontend tool with WebSocket access
func (as *AgentSession) executeFrontendTool(functionName string, functionCallArgs map[string]interface{}) (string, error) {
	// Extract the argument
	var stringArg string
	if len(functionCallArgs) != 1 {
		return "", fmt.Errorf("frontend tool '%s' expects 1 argument", functionName)
	}

	for _, val := range functionCallArgs {
		var ok bool
		stringArg, ok = val.(string)
		if !ok {
			return "", fmt.Errorf("frontend tool '%s' expects string argument", functionName)
		}
		break
	}

	switch functionName {
	case "Browser_Prompt":
		return as.executeBrowserPrompt(stringArg)
	case "Browser_Alert":
		return as.executeBrowserAlert(stringArg)
	default:
		return "", fmt.Errorf("unknown frontend tool: %s", functionName)
	}
}

// executeBrowserAlert executes the Browser_Alert tool with WebSocket support
//
// Note: unlike Browser_Prompt, this does not collect user input, but we still wait
// for a frontend ack so the tool call cleanly synchronizes with the browser UI.
func (as *AgentSession) executeBrowserAlert(message string) (string, error) {
	if message == "" {
		message = "Alert from your AI assistant!"
	}

	alertMessage := map[string]interface{}{
		"type":   "frontend_tool_prompt",
		"tool":   "Browser_Alert",
		"action": "browser_alert",
		"data": map[string]interface{}{
			"message": message,
		},
	}

	if err := as.Writer.WriteResponse(alertMessage); err != nil {
		return "", fmt.Errorf("failed to send alert to frontend: %w", err)
	}

	as.Logger.Printf("[Browser_Alert] Sent alert to frontend, waiting for ack...")

	ack, ok := as.ResponseWaiter.WaitForResponse()
	if !ok {
		return "", fmt.Errorf("failed to receive alert ack")
	}

	as.Logger.Printf("[Browser_Alert] Received ack: %s", ack)

	// Return a result that does NOT trigger the older "tool_result -> browser_alert" frontend handler.
	result := map[string]interface{}{
		"alert_shown":   true,
		"message_shown": message,
		"ack":           ack,
		"success":       true,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	return string(resultJSON), nil
}

// executeBrowserPrompt executes the Browser_Prompt tool with WebSocket support
func (as *AgentSession) executeBrowserPrompt(message string) (string, error) {
	if message == "" {
		message = "Please enter your response:"
	}

	// Send the prompt to the frontend via WebSocket
	promptMessage := map[string]interface{}{
		"type":   "frontend_tool_prompt",
		"tool":   "Browser_Prompt",
		"action": "browser_prompt",
		"data": map[string]interface{}{
			"message":       message,
			"default_value": "",
		},
	}

	if err := as.Writer.WriteResponse(promptMessage); err != nil {
		return "", fmt.Errorf("failed to send prompt to frontend: %w", err)
	}

	as.Logger.Printf("[Browser_Prompt] Sent prompt to frontend, waiting for response...")

	// Wait for the user's response
	response, ok := as.ResponseWaiter.WaitForResponse()
	if !ok {
		return "", fmt.Errorf("failed to receive user response")
	}

	as.Logger.Printf("[Browser_Prompt] Received user response: %s", response)

	// Return the response in the expected format
	result := map[string]interface{}{
		"user_response": response,
		"prompt_shown":  message,
		"success":       true,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	return string(resultJSON), nil
}
