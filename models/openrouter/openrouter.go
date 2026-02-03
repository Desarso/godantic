package openrouter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	models "github.com/Desarso/godantic/models"
	"github.com/Desarso/godantic/stores"
	"github.com/joho/godotenv"
)

const (
	OpenRouterBaseURL = "https://openrouter.ai/api/v1/chat/completions"
	DefaultModel      = "openai/gpt-4o-mini"
)

var (
	logFile *os.File
)

func init() {
	// Load .env file if it exists (not present in production)
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}
	var err error
	logFile, err = os.OpenFile("debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalln("Failed to open log file")
	}
}

// OpenRouter_Model implements the Model interface for OpenRouter API
// Also supports any OpenAI-compatible API endpoint
type OpenRouter_Model struct {
	Model        string // Model identifier (e.g., "openai/gpt-4o", "anthropic/claude-3-opus")
	Temperature  *float64
	MaxTokens    *int
	SiteURL      string // Optional: Your site URL for OpenRouter rankings
	SiteName     string // Optional: Your site name for OpenRouter rankings
	SystemPrompt string // Optional: System prompt for the AI
	BaseURL      string // Optional: Custom API base URL (defaults to OpenRouter)
	APIKeyEnv    string // Optional: Environment variable name for API key (defaults to OPENROUTER_API_KEY)
}

// Model_Request implements the Model interface
func (o *OpenRouter_Model) Model_Request(request models.Model_Request, tools []models.FunctionDeclaration, conversationHistory []stores.Message) (models.Model_Response, error) {
	if request.User_Message == nil && request.Tool_Results == nil {
		return models.Model_Response{}, fmt.Errorf("request must contain either user message or tool results")
	}

	var msg models.User_Message
	if request.User_Message != nil {
		msg = *request.User_Message
	} else {
		msg = models.User_Message{}
	}

	modelToUse := o.Model
	if modelToUse == "" {
		modelToUse = DefaultModel
	}

	openRouterResponse, err := o.makeRequest(modelToUse, msg, tools, request.Tool_Results, conversationHistory)
	if err != nil {
		return models.Model_Response{}, err
	}

	return o.openRouterResponseToModelResponse(openRouterResponse)
}

// Stream_Model_Request implements the Model interface for streaming
func (o *OpenRouter_Model) Stream_Model_Request(request models.Model_Request, tools []models.FunctionDeclaration, conversationHistory []stores.Message) (<-chan models.Model_Response, <-chan error) {
	if request.User_Message == nil && request.Tool_Results == nil {
		errChan := make(chan error, 1)
		respChan := make(chan models.Model_Response)
		errChan <- fmt.Errorf("request must contain either user message or tool results")
		close(errChan)
		close(respChan)
		return respChan, errChan
	}

	var msg models.User_Message
	if request.User_Message != nil {
		msg = *request.User_Message
	} else {
		msg = models.User_Message{}
	}

	modelToUse := o.Model
	if modelToUse == "" {
		modelToUse = DefaultModel
	}

	return o.makeStreamRequest(modelToUse, msg, tools, request.Tool_Results, conversationHistory)
}

// openRouterResponseToModelResponse converts OpenRouter response to the standard Model_Response
func (o *OpenRouter_Model) openRouterResponseToModelResponse(response OpenRouterResponse) (models.Model_Response, error) {
	modelResponse := models.Model_Response{}

	for _, choice := range response.Choices {
		// Handle text content
		if choice.Message.Content != nil {
			switch content := choice.Message.Content.(type) {
			case string:
				if content != "" {
					text := content
					modelResponse.Parts = append(modelResponse.Parts, models.Model_Part{
						Text: &text,
					})
				}
			}
		}

		// Handle tool calls
		for _, toolCall := range choice.Message.ToolCalls {
			if toolCall.Type == "function" {
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
					log.Printf("Warning: Failed to unmarshal tool call arguments: %v", err)
					args = map[string]interface{}{}
				}

				modelResponse.Parts = append(modelResponse.Parts, models.Model_Part{
					FunctionCall: &models.FunctionCall{
						ID:   toolCall.ID,
						Name: toolCall.Function.Name,
						Args: args,
					},
				})
			}
		}
	}

	return modelResponse, nil
}

// makeRequest sends a non-streaming request to OpenRouter
func (o *OpenRouter_Model) makeRequest(model string, message models.User_Message, tools []models.FunctionDeclaration, toolResults *[]models.Tool_Result, conversationHistory []stores.Message) (OpenRouterResponse, error) {
	requestBody, err := o.createOpenRouterRequest(model, message, tools, toolResults, conversationHistory, false)
	if err != nil {
		return OpenRouterResponse{}, fmt.Errorf("failed to create OpenRouter request: %w", err)
	}

	jsonBytes, err := json.Marshal(requestBody)
	if err != nil {
		return OpenRouterResponse{}, fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Debug: save request body
	if err := os.WriteFile("openrouter_request_body.json", jsonBytes, 0644); err != nil {
		log.Printf("Warning: failed to write request body to file: %v", err)
	}

	// Use custom base URL if provided, otherwise use OpenRouter
	baseURL := o.BaseURL
	if baseURL == "" {
		baseURL = OpenRouterBaseURL
	}

	req, err := http.NewRequest("POST", baseURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return OpenRouterResponse{}, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	o.setHeaders(req)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return OpenRouterResponse{}, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OpenRouterResponse{}, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil {
			return OpenRouterResponse{}, fmt.Errorf("OpenRouter API error: %s (type: %s)", errResp.Error.Message, errResp.Error.Type)
		}
		return OpenRouterResponse{}, fmt.Errorf("OpenRouter API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var response OpenRouterResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return OpenRouterResponse{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return response, nil
}

// makeStreamRequest sends a streaming request to OpenRouter
func (o *OpenRouter_Model) makeStreamRequest(model string, message models.User_Message, tools []models.FunctionDeclaration, toolResults *[]models.Tool_Result, conversationHistory []stores.Message) (<-chan models.Model_Response, <-chan error) {
	respChan := make(chan models.Model_Response)
	errChan := make(chan error, 1)

	go func() {
		defer close(respChan)
		defer close(errChan)

		requestBody, err := o.createOpenRouterRequest(model, message, tools, toolResults, conversationHistory, true)
		if err != nil {
			errChan <- fmt.Errorf("failed to create OpenRouter request: %w", err)
			return
		}

		jsonBytes, err := json.Marshal(requestBody)
		if err != nil {
			errChan <- fmt.Errorf("failed to marshal request body: %w", err)
			return
		}

		// Request body logging disabled - enable for debugging if needed
		// log.Printf("OpenRouter Stream Request Body:\n%s", string(jsonBytes))

		// Use custom base URL if provided, otherwise use OpenRouter
		baseURL := o.BaseURL
		if baseURL == "" {
			baseURL = OpenRouterBaseURL
		}

		req, err := http.NewRequest("POST", baseURL, bytes.NewReader(jsonBytes))
		if err != nil {
			errChan <- fmt.Errorf("failed to create HTTP request: %w", err)
			return
		}

		o.setHeaders(req)

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			errChan <- fmt.Errorf("HTTP request failed: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			var errResp ErrorResponse
			if err := json.Unmarshal(body, &errResp); err == nil {
				errChan <- fmt.Errorf("OpenRouter API error: %s (type: %s)", errResp.Error.Message, errResp.Error.Type)
			} else {
				errChan <- fmt.Errorf("OpenRouter API error: status %d, body: %s", resp.StatusCode, string(body))
			}
			return
		}

		// Track accumulated tool calls across stream chunks
		toolCallAccumulator := make(map[int]*ToolCall)

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					// Process any accumulated tool calls before finishing
					if len(toolCallAccumulator) > 0 {
						modelResp := models.Model_Response{}
						for _, tc := range toolCallAccumulator {
							var args map[string]interface{}
							if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
								log.Printf("Warning: Failed to unmarshal final tool call arguments: %v", err)
								args = map[string]interface{}{}
							}
							modelResp.Parts = append(modelResp.Parts, models.Model_Part{
								FunctionCall: &models.FunctionCall{
									ID:   tc.ID,
									Name: tc.Function.Name,
									Args: args,
								},
							})
						}
						respChan <- modelResp
					}
					return
				}
				errChan <- fmt.Errorf("error reading stream: %w", err)
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Handle SSE format
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				// Process any accumulated tool calls
				if len(toolCallAccumulator) > 0 {
					modelResp := models.Model_Response{}
					for _, tc := range toolCallAccumulator {
						var args map[string]interface{}
						if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
							log.Printf("Warning: Failed to unmarshal final tool call arguments: %v", err)
							args = map[string]interface{}{}
						}
						modelResp.Parts = append(modelResp.Parts, models.Model_Part{
							FunctionCall: &models.FunctionCall{
								ID:   tc.ID,
								Name: tc.Function.Name,
								Args: args,
							},
						})
					}
					respChan <- modelResp
				}
				return
			}

			var streamResp StreamResponse
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				log.Printf("Warning: Failed to unmarshal stream chunk: %v, data: %s", err, data)
				continue
			}

			for _, choice := range streamResp.Choices {
				if choice.Delta == nil {
					continue
				}

				modelResp := models.Model_Response{}

				// Handle text delta
				if choice.Delta.Content != nil {
					switch content := choice.Delta.Content.(type) {
					case string:
						if content != "" {
							text := content
							modelResp.Parts = append(modelResp.Parts, models.Model_Part{
								Text: &text,
							})
						}
					}
				}

				// Handle tool call deltas (accumulate)
				for _, toolCall := range choice.Delta.ToolCalls {
					idx := choice.Index
					if existing, ok := toolCallAccumulator[idx]; ok {
						// Append to existing tool call arguments
						existing.Function.Arguments += toolCall.Function.Arguments
					} else {
						// New tool call
						toolCallAccumulator[idx] = &ToolCall{
							ID:   toolCall.ID,
							Type: toolCall.Type,
							Function: ToolCallFunction{
								Name:      toolCall.Function.Name,
								Arguments: toolCall.Function.Arguments,
							},
						}
					}
				}

				// Send text parts immediately
				if len(modelResp.Parts) > 0 {
					respChan <- modelResp
				}
			}
		}
	}()

	return respChan, errChan
}

// setHeaders sets the required headers for OpenRouter API requests
func (o *OpenRouter_Model) setHeaders(req *http.Request) {
	// Use custom API key environment variable if provided, otherwise use OPENROUTER_API_KEY
	apiKeyEnv := o.APIKeyEnv
	if apiKeyEnv == "" {
		apiKeyEnv = "OPENROUTER_API_KEY"
	}
	apiKey := os.Getenv(apiKeyEnv)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Optional headers for OpenRouter
	if o.SiteURL != "" {
		req.Header.Set("HTTP-Referer", o.SiteURL)
	}
	if o.SiteName != "" {
		req.Header.Set("X-Title", o.SiteName)
	}
}

// createOpenRouterRequest builds the request body for OpenRouter API
func (o *OpenRouter_Model) createOpenRouterRequest(model string, message models.User_Message, tools []models.FunctionDeclaration, toolResults *[]models.Tool_Result, conversationHistory []stores.Message, stream bool) (OpenRouterRequest, error) {
	messages := []Message{}

	// Add system prompt as first message if provided
	if o.SystemPrompt != "" {
		messages = append(messages, Message{
			Role:    "system",
			Content: o.SystemPrompt,
		})
	}

	// 1. Process conversation history
	for _, histMsg := range conversationHistory {
		msg, err := o.convertHistoryMessage(histMsg)
		if err != nil {
			log.Printf("Warning: Failed to convert history message %d: %v", histMsg.ID, err)
			continue
		}
		if msg != nil {
			messages = append(messages, *msg)
		}
	}

	// 2. Handle tool results for the current turn
	if toolResults != nil && len(*toolResults) > 0 {
		for _, tr := range *toolResults {
			// Tool results in OpenAI format require the tool_call_id
			toolCallID := tr.Tool_ID
			messages = append(messages, Message{
				Role:       "tool",
				Content:    tr.Tool_Output,
				ToolCallID: &toolCallID,
			})
		}
	} else {
		// 3. Process current user message only if no tool results
		userMsg, err := o.convertUserMessage(message)
		if err != nil {
			return OpenRouterRequest{}, fmt.Errorf("failed to convert user message: %w", err)
		}
		if userMsg != nil {
			messages = append(messages, *userMsg)
		}
	}

	if len(messages) == 0 {
		return OpenRouterRequest{}, fmt.Errorf("cannot create OpenRouter request with no messages")
	}

	// Build request
	request := OpenRouterRequest{
		Model:    model,
		Messages: messages,
		Stream:   stream,
	}

	// Add tools if provided
	if len(tools) > 0 {
		request.Tools = ConvertToOpenRouterTools(tools)
		request.ToolChoice = "auto"
	}

	// Add optional parameters
	if o.Temperature != nil {
		request.Temperature = o.Temperature
	}
	if o.MaxTokens != nil {
		request.MaxTokens = o.MaxTokens
	}

	return request, nil
}

// convertHistoryMessage converts a stored message to OpenRouter Message format
func (o *OpenRouter_Model) convertHistoryMessage(histMsg stores.Message) (*Message, error) {
	if histMsg.PartsJSON == "" || histMsg.PartsJSON == "{}" || histMsg.PartsJSON == "null" {
		return nil, nil
	}

	role := histMsg.Role

	if role == "user" {
		var userParts []models.User_Part
		if err := json.Unmarshal([]byte(histMsg.PartsJSON), &userParts); err != nil {
			return nil, fmt.Errorf("failed to unmarshal user parts: %w", err)
		}

		// Check if this is a function response
		for _, part := range userParts {
			if part.FunctionResponse != nil {
				toolCallID := part.FunctionResponse.ID
				responseBytes, _ := json.Marshal(part.FunctionResponse.Response)
				return &Message{
					Role:       "tool",
					Content:    string(responseBytes),
					ToolCallID: &toolCallID,
				}, nil
			}
		}

		// Regular user message
		content := o.buildContentFromUserParts(userParts)
		if content == nil {
			return nil, nil
		}
		return &Message{
			Role:    "user",
			Content: content,
		}, nil

	} else if role == "model" {
		var modelParts []models.Model_Part
		if err := json.Unmarshal([]byte(histMsg.PartsJSON), &modelParts); err != nil {
			return nil, fmt.Errorf("failed to unmarshal model parts: %w", err)
		}

		msg := &Message{
			Role: "assistant",
		}

		var textContent strings.Builder
		var toolCalls []ToolCall

		for _, part := range modelParts {
			if part.Text != nil && *part.Text != "" {
				textContent.WriteString(*part.Text)
			}
			if part.FunctionCall != nil {
				argsBytes, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, ToolCall{
					ID:   part.FunctionCall.ID,
					Type: "function",
					Function: ToolCallFunction{
						Name:      part.FunctionCall.Name,
						Arguments: string(argsBytes),
					},
				})
			}
		}

		if textContent.Len() > 0 {
			msg.Content = textContent.String()
		}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}

		if msg.Content == nil && len(msg.ToolCalls) == 0 {
			return nil, nil
		}

		return msg, nil
	}

	return nil, fmt.Errorf("unknown role: %s", role)
}

// convertUserMessage converts a User_Message to OpenRouter Message format
func (o *OpenRouter_Model) convertUserMessage(message models.User_Message) (*Message, error) {
	if len(message.Content.Parts) == 0 {
		return nil, nil
	}

	content := o.buildContentFromUserParts(message.Content.Parts)
	if content == nil {
		return nil, nil
	}

	return &Message{
		Role:    "user",
		Content: content,
	}, nil
}

// buildContentFromUserParts builds message content from user parts
// Returns string for text-only, []ContentPart for multimodal
func (o *OpenRouter_Model) buildContentFromUserParts(parts []models.User_Part) interface{} {
	var textParts []string
	var contentParts []ContentPart
	hasMultimodal := false

	for _, part := range parts {
		if part.Text != "" {
			textParts = append(textParts, part.Text)
			contentParts = append(contentParts, ContentPart{
				Type: "text",
				Text: part.Text,
			})
		}

		// Handle inline data (base64 images)
		if part.InlineData != nil {
			hasMultimodal = true
			dataURL := fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, part.InlineData.Data)
			contentParts = append(contentParts, ContentPart{
				Type: "image_url",
				ImageURL: &ImageURL{
					URL: dataURL,
				},
			})
		}

		// Handle file data (URLs)
		if part.FileData != nil {
			hasMultimodal = true
			contentParts = append(contentParts, ContentPart{
				Type: "image_url",
				ImageURL: &ImageURL{
					URL: part.FileData.FileUrl,
				},
			})
		}

		// Handle image data
		if part.ImageData != nil {
			hasMultimodal = true
			contentParts = append(contentParts, ContentPart{
				Type: "image_url",
				ImageURL: &ImageURL{
					URL: part.ImageData.FileUrl,
				},
			})
		}
	}

	if len(contentParts) == 0 {
		return nil
	}

	// Return simple string if text-only
	if !hasMultimodal && len(textParts) > 0 {
		return strings.Join(textParts, "\n")
	}

	// Return content parts for multimodal
	return contentParts
}
