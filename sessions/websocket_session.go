package sessions

import (
	"encoding/json"

	"github.com/Desarso/godantic/models"
	"github.com/google/uuid"
)

// functionCallInfo holds information about a function call
type functionCallInfo struct {
	Name       string
	Args       map[string]interface{}
	ID         string
	ArgsJSON   string
	TextInPart *string
}

// RunInteraction handles the complete agent interaction loop
func (as *AgentSession) RunInteraction(req models.Model_Request) error {
	currentReq := req

	for {
		// Fetch latest history
		if err := as.fetchHistory(); err != nil {
			return as.sendError("Failed to fetch history", false)
		}

		// Save user message or tool results if present
		if err := as.saveIncomingMessage(currentReq); err != nil {
			as.Logger.Printf("Error saving incoming message: %v", err)
		}

		// Run agent stream - now we can pass history directly since types match
		resChan, errChan := as.Agent.Run_Stream(currentReq, as.History)

		// Process stream and accumulate parts
		accumulatedParts, err := as.processStream(resChan, errChan)
		if err != nil {
			return err
		}

		// Process accumulated parts for tools and text
		toolResults, executed, err := as.processAccumulatedParts(accumulatedParts)
		if err != nil {
			return err
		}

		if !executed {
			// No tools executed, interaction complete
			break
		}

		// Prepare for next iteration with tool results
		currentReq = models.Model_Request{
			User_Message: nil,
			Tool_Results: &toolResults,
		}
	}

	return as.Writer.WriteDone()
}

// fetchHistory retrieves the latest conversation history
func (as *AgentSession) fetchHistory() error {
	history, err := as.Store.FetchHistory(as.SessionID)
	if err != nil {
		as.Logger.Printf("Error fetching history: %v", err)
		return &AgentError{Message: "Failed to fetch history", Fatal: false}
	}
	as.History = history
	return nil
}

// saveIncomingMessage saves user messages or tool results to the database
func (as *AgentSession) saveIncomingMessage(req models.Model_Request) error {
	if req.User_Message != nil {
		return as.saveUserMessage(req.User_Message)
	} else if req.Tool_Results != nil {
		return as.saveToolResults(*req.Tool_Results)
	}
	return nil
}

// saveUserMessage saves a user message to the database
func (as *AgentSession) saveUserMessage(userMsg *models.User_Message) error {
	userPartsToSave := make([]models.User_Part, 0)
	userText := ""

	if userMsg.Content.Parts != nil {
		for _, part := range userMsg.Content.Parts {
			userPartsToSave = append(userPartsToSave, part)
			if part.Text != "" {
				userText += part.Text
			}
		}
	}

	// Legacy support: if no parts but text exists, create text part
	if len(userPartsToSave) == 0 && userText != "" {
		as.Logger.Printf("Warning: User message had text but no parts structure; creating text part.")
		userPartsToSave = append(userPartsToSave, models.User_Part{Text: userText})
	}

	return as.Store.SaveMessage(as.SessionID, "user", "user_message", userPartsToSave, "")
}

// saveToolResults saves tool results to the database
func (as *AgentSession) saveToolResults(toolResults []models.Tool_Result) error {
	toolResponseParts := make([]models.User_Part, 0, len(toolResults))

	for _, toolResult := range toolResults {
		var resultMap map[string]interface{}
		if err := json.Unmarshal([]byte(toolResult.Tool_Output), &resultMap); err != nil {
			as.Logger.Printf("Failed to unmarshal tool output for DB saving (%s), storing raw: %v", toolResult.Tool_Name, err)
			resultMap = map[string]interface{}{"raw_output": toolResult.Tool_Output}
		}

		part := models.User_Part{
			FunctionResponse: &models.FunctionResponse{
				Name:     toolResult.Tool_Name,
				Response: resultMap,
			},
		}
		toolResponseParts = append(toolResponseParts, part)
	}

	if len(toolResponseParts) > 0 {
		return as.Store.SaveMessage(as.SessionID, "user", "function_response", toolResponseParts, "")
	}
	return nil
}

// processStream handles the agent stream processing
func (as *AgentSession) processStream(resChan <-chan models.Model_Response, errChan <-chan error) ([]models.Model_Part, error) {
	var accumulated []models.Model_Part

	for {
		select {
		case chunk, ok := <-resChan:
			if !ok {
				as.Logger.Printf("Stream finished normally")
				return accumulated, nil
			}
			accumulated = append(accumulated, chunk.Parts...)
			if err := as.Writer.WriteResponse(chunk); err != nil {
				as.Logger.Printf("Error writing stream chunk: %v", err)
				return nil, &AgentError{Message: "Error writing stream chunk", Fatal: true}
			}

		case streamErr, ok := <-errChan:
			if ok && streamErr != nil {
				as.Logger.Printf("Stream error: %v", streamErr)
				as.Writer.WriteError("Agent stream error: " + streamErr.Error())
				return nil, &AgentError{Message: "Agent stream error", Fatal: false}
			}
			if !ok {
				errChan = nil
			}
		}

		if resChan == nil && errChan == nil {
			as.Logger.Printf("Both agent stream channels closed unexpectedly")
			return accumulated, nil
		}
	}
}

// processAccumulatedParts processes accumulated parts for function calls and text
func (as *AgentSession) processAccumulatedParts(parts []models.Model_Part) ([]models.Tool_Result, bool, error) {
	if len(parts) == 0 {
		return nil, false, nil
	}

	toolResults := []models.Tool_Result{}
	executedAny := false
	finalText := ""

	// Extract function calls and text
	functionCalls := as.extractFunctionCalls(parts, &finalText)

	if len(functionCalls) > 0 {
		// Process function calls
		modelPartsToSave := make([]models.Model_Part, 0, len(functionCalls))

		for _, fc := range functionCalls {
			// Create model part for saving
			part := models.Model_Part{
				FunctionCall: &models.FunctionCall{
					ID:   fc.ID,
					Name: fc.Name,
					Args: fc.Args,
				},
				Text: fc.TextInPart,
			}
			modelPartsToSave = append(modelPartsToSave, part)

			// Check approval and execute if auto-approved
			if approved, err := as.checkAndExecuteTool(fc); err != nil {
				as.Logger.Printf("Error checking tool approval for %s (ID: %s): %v", fc.Name, fc.ID, err)
				continue
			} else if approved {
				toolResult, err := as.executeTool(fc)
				if err != nil {
					as.Logger.Printf("Error executing tool %s (ID: %s): %v", fc.Name, fc.ID, err)
				}

				// Send tool result to client
				if err := as.sendToolResult(fc, toolResult); err != nil {
					as.Logger.Printf("Error sending tool result: %v", err)
				}

				// Add to results for next iteration
				toolResults = append(toolResults, models.Tool_Result{
					Tool_Name:   fc.Name,
					Tool_Output: toolResult,
				})
				executedAny = true
			}
		}

		// Save function calls to database
		if len(modelPartsToSave) > 0 {
			if err := as.Store.SaveMessage(as.SessionID, "model", "function_call", modelPartsToSave, ""); err != nil {
				as.Logger.Printf("Error saving function call message: %v", err)
			}
		}

	} else if finalText != "" {
		// Save text-only response
		textPart := models.Model_Part{Text: &finalText}
		if err := as.Store.SaveMessage(as.SessionID, "model", "model_message", []models.Model_Part{textPart}, ""); err != nil {
			as.Logger.Printf("Error saving text message: %v", err)
		}
	}

	return toolResults, executedAny, nil
}

// extractFunctionCalls extracts unique function calls from parts
func (as *AgentSession) extractFunctionCalls(parts []models.Model_Part, finalText *string) []functionCallInfo {
	seenFC := make(map[string]bool)
	functionCalls := []functionCallInfo{}

	for _, part := range parts {
		// Accumulate text
		if part.Text != nil {
			*finalText += *part.Text
		}

		// Process function calls
		if part.FunctionCall != nil {
			argsBytes, _ := json.Marshal(part.FunctionCall.Args)
			argsJSON := string(argsBytes)
			key := part.FunctionCall.Name + "|" + argsJSON

			if !seenFC[key] {
				seenFC[key] = true
				id := part.FunctionCall.ID
				if id == "" {
					id = uuid.New().String()
				}

				functionCalls = append(functionCalls, functionCallInfo{
					Name:       part.FunctionCall.Name,
					Args:       part.FunctionCall.Args,
					ID:         id,
					ArgsJSON:   argsJSON,
					TextInPart: part.Text,
				})
			}
		}
	}

	return functionCalls
}

// checkAndExecuteTool checks if a tool should be auto-approved
func (as *AgentSession) checkAndExecuteTool(fc functionCallInfo) (bool, error) {
	return as.Agent.ApproveTool(fc.Name, fc.Args)
}

// executeTool executes a tool and returns the result
func (as *AgentSession) executeTool(fc functionCallInfo) (string, error) {
	return as.Agent.ExecuteTool(fc.Name, fc.Args, as.SessionID)
}

// sendToolResult sends a tool result to the WebSocket client
func (as *AgentSession) sendToolResult(fc functionCallInfo, toolResultJSON string) error {
	var resultData map[string]interface{}
	if err := json.Unmarshal([]byte(toolResultJSON), &resultData); err != nil {
		as.Logger.Printf("Failed to unmarshal tool result JSON for structured sending (tool: %s, ID: %s): %v. Sending raw JSON string.", fc.Name, fc.ID, err)
		resultData = nil
	}

	toolMsg := WebSocketToolResultMessage{
		Type:         "tool_result",
		FunctionName: fc.Name,
		FunctionID:   fc.ID,
		Result:       resultData,
		ResultJSON:   toolResultJSON,
	}

	return as.Writer.WriteResponse(toolMsg)
}

// sendError sends an error message and returns an AgentError
func (as *AgentSession) sendError(message string, fatal bool) error {
	as.Logger.Printf("Error: %s (fatal: %v)", message, fatal)
	as.Writer.WriteError(message)
	return &AgentError{Message: message, Fatal: fatal}
}
