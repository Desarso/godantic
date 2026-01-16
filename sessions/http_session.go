package sessions

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Desarso/godantic/models"
)

// RunSingleInteraction handles a complete request-response cycle (legacy method)
func (s *HTTPSession) RunSingleInteraction(userMessage models.User_Message) (models.Model_Response, error) {
	// Save user message
	if err := s.saveUserMessage(userMessage); err != nil {
		s.Logger.Printf("Error saving user message: %v", err)
	}

	// Get history and run agent
	history, err := s.Store.FetchHistory(s.ConversationID)
	if err != nil {
		return models.Model_Response{}, fmt.Errorf("failed to fetch history: %w", err)
	}

	req := models.Model_Request{User_Message: &userMessage}
	response, err := s.Agent.Run(req, history)
	if err != nil {
		return models.Model_Response{}, fmt.Errorf("agent error: %w", err)
	}

	// Save model response and handle auto-approved tools
	if err := s.processAndSaveResponse(response); err != nil {
		s.Logger.Printf("Error processing response: %v", err)
	}

	return response, nil
}

// RunStreamInteraction handles streaming interactions (legacy method)
func (s *HTTPSession) RunStreamInteraction(userMessage models.User_Message) (<-chan models.Model_Response, <-chan error) {
	respChan := make(chan models.Model_Response)
	errChan := make(chan error, 1)

	go func() {
		defer close(respChan)
		defer close(errChan)

		// Save user message
		if err := s.saveUserMessage(userMessage); err != nil {
			s.Logger.Printf("Error saving user message: %v", err)
		}

		// Get history and run agent stream
		history, err := s.Store.FetchHistory(s.ConversationID)
		if err != nil {
			errChan <- fmt.Errorf("failed to fetch history: %w", err)
			return
		}

		req := models.Model_Request{User_Message: &userMessage}
		agentRespChan, agentErrChan := s.Agent.Run_Stream(req, history)

		var accumulatedParts []models.Model_Part

		// Forward stream responses and accumulate parts
		for {
			select {
			case response, ok := <-agentRespChan:
				if !ok {
					// Stream finished, save accumulated response
					if len(accumulatedParts) > 0 {
						finalResponse := models.Model_Response{Parts: accumulatedParts}
						if err := s.processAndSaveResponse(finalResponse); err != nil {
							s.Logger.Printf("Error saving final response: %v", err)
						}
					}
					return
				}
				accumulatedParts = append(accumulatedParts, response.Parts...)
				respChan <- response

			case err, ok := <-agentErrChan:
				if ok && err != nil {
					errChan <- err
					return
				}
				if !ok {
					agentErrChan = nil
				}
			}

			if agentRespChan == nil && agentErrChan == nil {
				// Both channels closed, save accumulated response
				if len(accumulatedParts) > 0 {
					finalResponse := models.Model_Response{Parts: accumulatedParts}
					if err := s.processAndSaveResponse(finalResponse); err != nil {
						s.Logger.Printf("Error saving final response: %v", err)
					}
				}
				return
			}
		}
	}()

	return respChan, errChan
}

// RunSingleInteractionWithRequest handles a complete request-response cycle with Model_Request format
func (s *HTTPSession) RunSingleInteractionWithRequest(request models.Model_Request) (models.Model_Response, error) {
	// Validate request has either user message or tool results
	if request.User_Message == nil && request.Tool_Results == nil {
		return models.Model_Response{}, fmt.Errorf("request must contain either user message or tool results")
	}

	currentReq := request
	var finalResponse models.Model_Response
	iteration := 0

	for {
		iteration++
		s.Logger.Printf("=== HTTP Iteration %d ===", iteration)

		// Save user message if present (only on first iteration)
		if currentReq.User_Message != nil {
			s.Logger.Printf("Processing user message with %d parts", len(currentReq.User_Message.Content.Parts))
			if err := s.saveUserMessage(*currentReq.User_Message); err != nil {
				s.Logger.Printf("Error saving user message: %v", err)
			}
		}

		// Save tool results if present - Tool results should be saved when fed back as next request
		if currentReq.Tool_Results != nil {
			s.Logger.Printf("Processing tool results: %d tools", len(*currentReq.Tool_Results))
			for _, tr := range *currentReq.Tool_Results {
				s.Logger.Printf("  Tool: %s, Output length: %d", tr.Tool_Name, len(tr.Tool_Output))
			}
			// TEMPORARILY COMMENTED OUT to test if tool results are saved elsewhere:
			// if err := s.saveToolResults(*currentReq.Tool_Results); err != nil {
			// 	s.Logger.Printf("Error saving tool results: %v", err)
			// }
		}

		// Get history and run agent
		history, err := s.Store.FetchHistory(s.ConversationID)
		if err != nil {
			return models.Model_Response{}, fmt.Errorf("failed to fetch history: %w", err)
		}
		s.Logger.Printf("Retrieved %d messages from history", len(history))

		s.Logger.Printf("Calling agent.Run...")
		response, err := s.Agent.Run(currentReq, history)
		if err != nil {
			s.Logger.Printf("Agent error: %v", err)
			return models.Model_Response{}, fmt.Errorf("agent error: %w", err)
		}
		s.Logger.Printf("Agent returned %d parts", len(response.Parts))
		for i, part := range response.Parts {
			if part.FunctionCall != nil {
				s.Logger.Printf("  Part %d: FunctionCall - %s", i, part.FunctionCall.Name)
			} else if part.Text != nil {
				s.Logger.Printf("  Part %d: Text - '%s'", i, *part.Text)
			} else {
				s.Logger.Printf("  Part %d: Empty/Other", i)
			}
		}

		// Process response for tool execution and extract text
		toolResults, executed, finalText, err := s.processResponseForToolsAndText(response)
		if err != nil {
			return models.Model_Response{}, fmt.Errorf("error processing tools: %w", err)
		}
		s.Logger.Printf("Tool processing: executed=%v, results=%d, finalText='%s'", executed, len(toolResults), finalText)

		if !executed {
			// No tools executed, this is the final response
			s.Logger.Printf("No tools executed - final response")
			if finalText != "" {
				// Create a text response
				s.Logger.Printf("Creating text response with: '%s'", finalText)
				textPart := models.Model_Part{Text: &finalText}
				finalResponse = models.Model_Response{Parts: []models.Model_Part{textPart}}

				// Save the final text response
				if err := s.Store.SaveMessage(s.ConversationID, "model", "model_message", []models.Model_Part{textPart}, ""); err != nil {
					s.Logger.Printf("Error saving final text message: %v", err)
				}
			} else {
				// No text and no tools - return the original response
				s.Logger.Printf("No final text, returning original response with %d parts", len(response.Parts))
				finalResponse = response
			}
			break
		}

		// Prepare for next iteration with tool results
		s.Logger.Printf("Preparing next iteration with %d tool results", len(toolResults))
		currentReq = models.Model_Request{
			User_Message: nil,
			Tool_Results: &toolResults,
		}
	}

	s.Logger.Printf("Final response has %d parts", len(finalResponse.Parts))
	return finalResponse, nil
}

// RunStreamInteractionWithRequest handles streaming interactions with Model_Request format
func (s *HTTPSession) RunStreamInteractionWithRequest(request models.Model_Request) (<-chan models.Model_Response, <-chan error) {
	respChan := make(chan models.Model_Response)
	errChan := make(chan error, 1)

	go func() {
		defer close(respChan)
		defer close(errChan)

		// Validate request has either user message or tool results
		if request.User_Message == nil && request.Tool_Results == nil {
			errChan <- fmt.Errorf("request must contain either user message or tool results")
			return
		}

		currentReq := request
		var allParts []models.Model_Part

		for {
			// Save user message if present (only on first iteration)
			if currentReq.User_Message != nil {
				if err := s.saveUserMessage(*currentReq.User_Message); err != nil {
					s.Logger.Printf("Error saving user message: %v", err)
				}
			}

			// Save tool results if present
			if currentReq.Tool_Results != nil {
				if err := s.saveToolResults(*currentReq.Tool_Results); err != nil {
					s.Logger.Printf("Error saving tool results: %v", err)
				}
			}

			// Get history and run agent stream
			history, err := s.Store.FetchHistory(s.ConversationID)
			if err != nil {
				errChan <- fmt.Errorf("failed to fetch history: %w", err)
				return
			}

			agentRespChan, agentErrChan := s.Agent.Run_Stream(currentReq, history)

			var iterationParts []models.Model_Part

			// Forward stream responses and accumulate parts for this iteration
			for {
				select {
				case response, ok := <-agentRespChan:
					if !ok {
						// Stream finished for this iteration
						goto processIteration
					}
					iterationParts = append(iterationParts, response.Parts...)
					allParts = append(allParts, response.Parts...)
					respChan <- response

				case err, ok := <-agentErrChan:
					if ok && err != nil {
						errChan <- err
						return
					}
					if !ok {
						agentErrChan = nil
					}
				}

				if agentRespChan == nil && agentErrChan == nil {
					// Both channels closed
					goto processIteration
				}
			}

		processIteration:
			// Process this iteration's parts for tool execution
			if len(iterationParts) > 0 {
				iterationResponse := models.Model_Response{Parts: iterationParts}
				toolResults, executed, err := s.processResponseForTools(iterationResponse)
				if err != nil {
					errChan <- fmt.Errorf("error processing tools: %w", err)
					return
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
			} else {
				// No parts in this iteration, break
				break
			}
		}
	}()

	return respChan, errChan
}

// RunSSEInteraction handles complete SSE streaming interaction with context cancellation (legacy method)
func (s *HTTPSession) RunSSEInteraction(userMessage models.User_Message, writer SSEWriter, ctx context.Context) error {
	// Run streaming interaction
	respChan, errChan := s.RunStreamInteraction(userMessage)

	for {
		select {
		case response, ok := <-respChan:
			if !ok {
				s.Logger.Printf("SSE stream finished.")
				return nil
			}

			jsonData, err := json.Marshal(response)
			if err != nil {
				s.Logger.Printf("Error marshalling response: %v", err)
				continue
			}

			if err := writer.WriteSSE(string(jsonData)); err != nil {
				s.Logger.Printf("Error writing to SSE stream: %v", err)
				return err
			}
			writer.Flush()

		case err, ok := <-errChan:
			if ok && err != nil {
				s.Logger.Printf("SSE stream error: %v", err)
				if writeErr := writer.WriteSSEError(err); writeErr != nil {
					s.Logger.Printf("Error writing SSE error: %v", writeErr)
				}
				writer.Flush()
				return err
			}
			if !ok {
				errChan = nil
			}

		case <-ctx.Done():
			s.Logger.Printf("SSE client disconnected")
			return ctx.Err()
		}

		if respChan == nil && errChan == nil {
			s.Logger.Printf("Both SSE channels closed.")
			return nil
		}
	}
}

// RunSSEInteractionWithRequest handles complete SSE streaming interaction with Model_Request format
func (s *HTTPSession) RunSSEInteractionWithRequest(request models.Model_Request, writer SSEWriter, ctx context.Context) error {
	// Run streaming interaction with Model_Request using the updated method
	respChan, errChan := s.RunStreamInteractionWithRequest(request)

	for {
		select {
		case response, ok := <-respChan:
			if !ok {
				s.Logger.Printf("SSE stream finished.")
				return nil
			}

			jsonData, err := json.Marshal(response)
			if err != nil {
				s.Logger.Printf("Error marshalling response: %v", err)
				continue
			}

			if err := writer.WriteSSE(string(jsonData)); err != nil {
				s.Logger.Printf("Error writing to SSE stream: %v", err)
				return err
			}
			writer.Flush()

		case err, ok := <-errChan:
			if ok && err != nil {
				s.Logger.Printf("SSE stream error: %v", err)
				if writeErr := writer.WriteSSEError(err); writeErr != nil {
					s.Logger.Printf("Error writing SSE error: %v", writeErr)
				}
				writer.Flush()
				return err
			}
			if !ok {
				errChan = nil
			}

		case <-ctx.Done():
			s.Logger.Printf("SSE client disconnected")
			return ctx.Err()
		}

		if respChan == nil && errChan == nil {
			s.Logger.Printf("Both SSE channels closed.")
			return nil
		}
	}
}

// saveUserMessage saves user message to store
func (s *HTTPSession) saveUserMessage(userMessage models.User_Message) error {
	userPartsToSave := make([]models.User_Part, 0)
	userText := ""

	if userMessage.Content.Parts != nil {
		for _, part := range userMessage.Content.Parts {
			userPartsToSave = append(userPartsToSave, part)
			if part.Text != "" {
				userText += part.Text
			}
		}
	}

	// Legacy support: if no parts but text exists, create text part
	if len(userPartsToSave) == 0 && userText != "" {
		userPartsToSave = append(userPartsToSave, models.User_Part{Text: userText})
	}

	return s.Store.SaveMessage(s.ConversationID, "user", "user_message", userPartsToSave, "")
}

// saveToolResults saves tool results to the database for HTTP sessions
func (s *HTTPSession) saveToolResults(toolResults []models.Tool_Result) error {
	toolResponseParts := make([]models.User_Part, 0, len(toolResults))

	for _, toolResult := range toolResults {
		var resultMap map[string]interface{}
		if err := json.Unmarshal([]byte(toolResult.Tool_Output), &resultMap); err != nil {
			s.Logger.Printf("Failed to unmarshal tool output for DB saving (%s), storing raw: %v", toolResult.Tool_Name, err)
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
		return s.Store.SaveMessage(s.ConversationID, "user", "function_response", toolResponseParts, "")
	}
	return nil
}

// processAndSaveResponse processes and saves model response, handling auto-approved tools
func (s *HTTPSession) processAndSaveResponse(response models.Model_Response) error {
	if len(response.Parts) == 0 {
		return nil
	}

	// Determine message type and check for function calls
	msgType := "model_message"
	var functionID string
	var firstFunctionName string
	var firstFunctionArgs map[string]interface{}
	foundFunctionCall := false

	for i, part := range response.Parts {
		if part.FunctionCall != nil {
			msgType = "function_call"
			if !foundFunctionCall {
				foundFunctionCall = true
				functionID = fmt.Sprintf("func_%s_%d", part.FunctionCall.Name, i)
				firstFunctionName = part.FunctionCall.Name
				firstFunctionArgs = part.FunctionCall.Args
			}
		}
	}

	// Save the model response
	if err := s.Store.SaveMessage(s.ConversationID, "model", msgType, response.Parts, functionID); err != nil {
		return fmt.Errorf("failed to save model response: %w", err)
	}

	// Handle auto-approved function calls
	if foundFunctionCall {
		if autoApproved, err := s.Agent.ApproveTool(firstFunctionName, firstFunctionArgs); err != nil {
			s.Logger.Printf("Error checking tool approval: %v", err)
		} else if autoApproved {
			s.Logger.Printf("Tool %s is auto-approved. Executing...", firstFunctionName)

			toolResult, err := s.Agent.ExecuteTool(firstFunctionName, firstFunctionArgs, s.ConversationID)
			if err != nil {
				s.Logger.Printf("Tool execution error: %v", err)
			}

			// Save tool result
			var resultMap map[string]interface{}
			if err := json.Unmarshal([]byte(toolResult), &resultMap); err != nil {
				resultMap = map[string]interface{}{"raw_output": toolResult}
			}

			toolResponsePart := models.User_Part{
				FunctionResponse: &models.FunctionResponse{
					Name:     firstFunctionName,
					Response: resultMap,
				},
			}

			if err := s.Store.SaveMessage(s.ConversationID, "user", "function_response", []models.User_Part{toolResponsePart}, functionID); err != nil {
				return fmt.Errorf("failed to save tool result: %w", err)
			}
		}
	}

	return nil
}

// GetChatHistory retrieves and converts chat history to API response format
func (s *HTTPSession) GetChatHistory() ([]models.ChatMessageResponse, error) {
	// Get history from store
	dbHistory, err := s.Store.FetchHistory(s.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch history: %w", err)
	}

	// Convert to API response format
	apiHistory := make([]models.ChatMessageResponse, 0, len(dbHistory))
	for _, msg := range dbHistory {
		apiMsg := models.ChatMessageResponse{
			ID:             msg.ID,
			CreatedAt:      msg.CreatedAt,
			UpdatedAt:      msg.UpdatedAt,
			ConversationID: msg.ConversationID,
			Sequence:       msg.Sequence,
			Role:           msg.Role,
			Type:           msg.Type,
			FunctionID:     msg.FunctionID,
			Text:           "",
			Parts:          nil,
		}

		// Unmarshal PartsJSON and extract text content
		if msg.PartsJSON != "" && msg.PartsJSON != "{}" && msg.PartsJSON != "null" {
			var unmarshalledParts interface{}
			if err := json.Unmarshal([]byte(msg.PartsJSON), &unmarshalledParts); err != nil {
				s.Logger.Printf("Error unmarshalling PartsJSON for msg ID %d: %v", msg.ID, err)
			} else {
				apiMsg.Parts = unmarshalledParts

				// Extract text for user/model messages
				if msg.Type == "user_message" {
					var userParts []models.User_Part
					if err := json.Unmarshal([]byte(msg.PartsJSON), &userParts); err == nil {
						for _, p := range userParts {
							if p.Text != "" {
								apiMsg.Text += p.Text
							}
						}
					}
				} else if msg.Type == "model_message" {
					var modelParts []models.Model_Part
					if err := json.Unmarshal([]byte(msg.PartsJSON), &modelParts); err == nil {
						for _, p := range modelParts {
							if p.Text != nil && *p.Text != "" {
								apiMsg.Text += *p.Text
							}
						}
					}
				}
			}
		}

		apiHistory = append(apiHistory, apiMsg)
	}

	return apiHistory, nil
}

// processResponseForTools processes model response for tool execution and returns tool results
func (s *HTTPSession) processResponseForTools(response models.Model_Response) ([]models.Tool_Result, bool, error) {
	if len(response.Parts) == 0 {
		return nil, false, nil
	}

	toolResults := []models.Tool_Result{}
	executedAny := false

	// Determine message type and extract function calls
	msgType := "model_message"
	var functionID string
	functionCalls := []struct {
		Name string
		Args map[string]interface{}
		ID   string
	}{}

	// Extract all function calls from parts
	for i, part := range response.Parts {
		if part.FunctionCall != nil {
			msgType = "function_call"
			if functionID == "" {
				functionID = fmt.Sprintf("func_%s_%d", part.FunctionCall.Name, i)
			}

			id := part.FunctionCall.ID
			if id == "" {
				id = fmt.Sprintf("func_%s_%d", part.FunctionCall.Name, i)
			}

			functionCalls = append(functionCalls, struct {
				Name string
				Args map[string]interface{}
				ID   string
			}{
				Name: part.FunctionCall.Name,
				Args: part.FunctionCall.Args,
				ID:   id,
			})
		}
	}

	// Save the model response first
	if err := s.Store.SaveMessage(s.ConversationID, "model", msgType, response.Parts, functionID); err != nil {
		return nil, false, fmt.Errorf("failed to save model response: %w", err)
	}

	// Process each function call
	for _, fc := range functionCalls {
		// Check approval and execute if auto-approved
		if autoApproved, err := s.Agent.ApproveTool(fc.Name, fc.Args); err != nil {
			s.Logger.Printf("Error checking tool approval for %s: %v", fc.Name, err)
			continue
		} else if autoApproved {
			s.Logger.Printf("Tool %s is auto-approved. Executing...", fc.Name)

			toolResult, err := s.Agent.ExecuteTool(fc.Name, fc.Args, s.ConversationID)
			if err != nil {
				s.Logger.Printf("Tool execution error for %s: %v", fc.Name, err)
				continue
			}

			// Save tool result to database
			var resultMap map[string]interface{}
			if err := json.Unmarshal([]byte(toolResult), &resultMap); err != nil {
				resultMap = map[string]interface{}{"raw_output": toolResult}
			}

		toolResponsePart := models.User_Part{
			FunctionResponse: &models.FunctionResponse{
				ID:       fc.ID,
				Name:     fc.Name,
				Response: resultMap,
			},
		}

		if err := s.Store.SaveMessage(s.ConversationID, "user", "function_response", []models.User_Part{toolResponsePart}, fc.ID); err != nil {
			s.Logger.Printf("Failed to save tool result for %s: %v", fc.Name, err)
		}

		// Add to results for next iteration
		toolResults = append(toolResults, models.Tool_Result{
			Tool_ID:     fc.ID,
			Tool_Name:   fc.Name,
			Tool_Output: toolResult,
		})
			executedAny = true
		}
	}

	return toolResults, executedAny, nil
}

// processResponseForToolsAndText processes model response for tool execution and returns tool results and final text
func (s *HTTPSession) processResponseForToolsAndText(response models.Model_Response) ([]models.Tool_Result, bool, string, error) {
	if len(response.Parts) == 0 {
		return nil, false, "", nil
	}

	toolResults := []models.Tool_Result{}
	executedAny := false
	finalText := ""

	// Determine message type and extract function calls
	msgType := "model_message"
	var functionID string
	functionCalls := []struct {
		Name string
		Args map[string]interface{}
		ID   string
	}{}

	// Extract all function calls from parts
	for i, part := range response.Parts {
		if part.FunctionCall != nil {
			msgType = "function_call"
			if functionID == "" {
				functionID = fmt.Sprintf("func_%s_%d", part.FunctionCall.Name, i)
			}

			id := part.FunctionCall.ID
			if id == "" {
				id = fmt.Sprintf("func_%s_%d", part.FunctionCall.Name, i)
			}

			functionCalls = append(functionCalls, struct {
				Name string
				Args map[string]interface{}
				ID   string
			}{
				Name: part.FunctionCall.Name,
				Args: part.FunctionCall.Args,
				ID:   id,
			})
		}
	}

	// Save the model response first
	if err := s.Store.SaveMessage(s.ConversationID, "model", msgType, response.Parts, functionID); err != nil {
		return nil, false, "", fmt.Errorf("failed to save model response: %w", err)
	}

	// Process each function call
	for _, fc := range functionCalls {
		// Check approval and execute if auto-approved
		if autoApproved, err := s.Agent.ApproveTool(fc.Name, fc.Args); err != nil {
			s.Logger.Printf("Error checking tool approval for %s: %v", fc.Name, err)
			continue
		} else if autoApproved {
			s.Logger.Printf("Tool %s is auto-approved. Executing...", fc.Name)

		toolResult, err := s.Agent.ExecuteTool(fc.Name, fc.Args, s.ConversationID)
		if err != nil {
			s.Logger.Printf("Tool execution error for %s: %v", fc.Name, err)
			continue
		}

		// Add to results for next iteration
		toolResults = append(toolResults, models.Tool_Result{
			Tool_ID:     fc.ID,
			Tool_Name:   fc.Name,
			Tool_Output: toolResult,
		})
			executedAny = true
		}
	}

	// Extract final text from parts
	for _, part := range response.Parts {
		if part.Text != nil {
			finalText += *part.Text
		}
	}

	return toolResults, executedAny, finalText, nil
}
