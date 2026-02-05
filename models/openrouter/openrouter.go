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
	Model           string // Model identifier (e.g., "openai/gpt-4o", "anthropic/claude-3-opus")
	Temperature     *float64
	MaxTokens       *int
	SiteURL         string                                 // Optional: Your site URL for OpenRouter rankings
	SiteName        string                                 // Optional: Your site name for OpenRouter rankings
	SystemPrompt    string                                 // Optional: System prompt for the AI
	BaseURL         string                                 // Optional: Custom API base URL (defaults to OpenRouter)
	APIKeyEnv       string                                 // Optional: Environment variable name for API key (defaults to OPENROUTER_API_KEY)
	SupportsVision  bool                                   // Whether the model supports image/vision input
	WarningCallback func(warnings []models.HistoryWarning) `json:"-"` // Called when history is adapted with warnings
}

// SetHistoryWarningCallback sets the callback function for history adaptation warnings
// This implements the HistoryWarner interface
func (o *OpenRouter_Model) SetHistoryWarningCallback(callback func(warnings []models.HistoryWarning)) {
	o.WarningCallback = callback
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

				// Handle reasoning delta (chain-of-thought from models like Kimi K2.5, DeepSeek-R1)
				// Check both field names as different models use different conventions
				var reasoningContent string
				if choice.Delta.Reasoning != nil && *choice.Delta.Reasoning != "" {
					reasoningContent = *choice.Delta.Reasoning
				} else if choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
					reasoningContent = *choice.Delta.ReasoningContent
				}
				if reasoningContent != "" {
					modelResp.Parts = append(modelResp.Parts, models.Model_Part{
						Reasoning: &reasoningContent,
					})
				}

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

				// Send text/reasoning parts immediately
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

// historyOrMessageContainsImages checks if the conversation history or current message contains any images
func (o *OpenRouter_Model) historyOrMessageContainsImages(conversationHistory []stores.Message, message models.User_Message) bool {
	// Check current message
	for _, part := range message.Content.Parts {
		if part.InlineData != nil && isImageMimeType(part.InlineData.MimeType) {
			return true
		}
		if part.FileData != nil && isImageMimeType(part.FileData.MimeType) {
			return true
		}
		if part.ImageData != nil {
			return true
		}
	}

	// Check conversation history
	for _, histMsg := range conversationHistory {
		if histMsg.Role != "user" || histMsg.PartsJSON == "" {
			continue
		}

		var userParts []models.User_Part
		if err := json.Unmarshal([]byte(histMsg.PartsJSON), &userParts); err != nil {
			continue
		}

		for _, part := range userParts {
			if part.InlineData != nil && isImageMimeType(part.InlineData.MimeType) {
				return true
			}
			if part.FileData != nil && isImageMimeType(part.FileData.MimeType) {
				return true
			}
			if part.ImageData != nil {
				return true
			}
		}
	}

	return false
}

// createOpenRouterRequest builds the request body for OpenRouter API
func (o *OpenRouter_Model) createOpenRouterRequest(model string, message models.User_Message, tools []models.FunctionDeclaration, toolResults *[]models.Tool_Result, conversationHistory []stores.Message, stream bool) (OpenRouterRequest, error) {
	messages := []Message{}
	var allWarnings []models.HistoryWarning

	// Check if we need to strip images (model doesn't support vision)
	stripImages := !o.SupportsVision
	if stripImages {
		// Check if there are any images in history or current message that will be stripped
		hasImages := o.historyOrMessageContainsImages(conversationHistory, message)
		if hasImages {
			allWarnings = append(allWarnings, models.HistoryWarning{
				Type:    "images_stripped",
				Message: "This model doesn't support images",
				Details: "Images in conversation history were removed because the selected model doesn't support image input",
			})
		}
	}

	// Add system prompt as first message if provided
	if o.SystemPrompt != "" {
		messages = append(messages, Message{
			Role:    "system",
			Content: o.SystemPrompt,
		})
	}

	// 1. Process conversation history
	for _, histMsg := range conversationHistory {
		msg, warnings, err := o.convertHistoryMessageWithWarnings(histMsg, stripImages)
		if err != nil {
			log.Printf("Warning: Failed to convert history message %d: %v", histMsg.ID, err)
			allWarnings = append(allWarnings, models.HistoryWarning{
				Type:    "parse_error",
				Message: "Failed to parse message content",
				Details: err.Error(),
			})
			continue
		}
		allWarnings = append(allWarnings, warnings...)
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
		userMsg, err := o.convertUserMessage(message, stripImages)
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

	// Call warning callback if there are warnings and callback is set
	if len(allWarnings) > 0 && o.WarningCallback != nil {
		// Deduplicate warnings
		allWarnings = deduplicateWarnings(allWarnings)
		o.WarningCallback(allWarnings)
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

// convertHistoryMessageWithWarnings converts a stored message to OpenRouter Message format
// Returns the message, any warnings generated, and any error
// If stripImages is true, image content will be skipped
func (o *OpenRouter_Model) convertHistoryMessageWithWarnings(histMsg stores.Message, stripImages bool) (*Message, []models.HistoryWarning, error) {
	var warnings []models.HistoryWarning

	if histMsg.PartsJSON == "" || histMsg.PartsJSON == "{}" || histMsg.PartsJSON == "null" {
		return nil, warnings, nil
	}

	role := histMsg.Role

	if role == "user" {
		var userParts []models.User_Part
		if err := json.Unmarshal([]byte(histMsg.PartsJSON), &userParts); err != nil {
			return nil, warnings, fmt.Errorf("failed to unmarshal user parts: %w", err)
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
				}, warnings, nil
			}
		}

		// Regular user message
		content, contentWarnings := o.buildContentFromUserPartsWithWarnings(userParts, stripImages)
		warnings = append(warnings, contentWarnings...)
		if content == nil {
			return nil, warnings, nil
		}
		return &Message{
			Role:    "user",
			Content: content,
		}, warnings, nil

	} else if role == "model" {
		var modelParts []models.Model_Part
		if err := json.Unmarshal([]byte(histMsg.PartsJSON), &modelParts); err != nil {
			return nil, warnings, fmt.Errorf("failed to unmarshal model parts: %w", err)
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
			// Note: Reasoning content is internal and not sent to OpenRouter
		}

		if textContent.Len() > 0 {
			msg.Content = textContent.String()
		}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}

		if msg.Content == nil && len(msg.ToolCalls) == 0 {
			return nil, warnings, nil
		}

		return msg, warnings, nil
	}

	return nil, warnings, fmt.Errorf("unknown role: %s", role)
}

// convertHistoryMessage converts a stored message to OpenRouter Message format (legacy wrapper)
func (o *OpenRouter_Model) convertHistoryMessage(histMsg stores.Message) (*Message, error) {
	msg, _, err := o.convertHistoryMessageWithWarnings(histMsg, false)
	return msg, err
}

// deduplicateWarnings removes duplicate warnings based on type and message
func deduplicateWarnings(warnings []models.HistoryWarning) []models.HistoryWarning {
	seen := make(map[string]bool)
	result := []models.HistoryWarning{}

	for _, w := range warnings {
		key := w.Type + ":" + w.Message
		if !seen[key] {
			seen[key] = true
			result = append(result, w)
		}
	}

	return result
}

// convertUserMessage converts a User_Message to OpenRouter Message format
// If stripImages is true, image content will be skipped
func (o *OpenRouter_Model) convertUserMessage(message models.User_Message, stripImages bool) (*Message, error) {
	if len(message.Content.Parts) == 0 {
		return nil, nil
	}

	content, _ := o.buildContentFromUserPartsWithWarnings(message.Content.Parts, stripImages)
	if content == nil {
		return nil, nil
	}

	return &Message{
		Role:    "user",
		Content: content,
	}, nil
}

// buildContentFromUserPartsWithWarnings builds message content from user parts
// Returns content (string for text-only, []ContentPart for multimodal) and any warnings
// If stripImages is true, all image content will be skipped (for non-vision models)
func (o *OpenRouter_Model) buildContentFromUserPartsWithWarnings(parts []models.User_Part, stripImages bool) (interface{}, []models.HistoryWarning) {
	var textParts []string
	var contentParts []ContentPart
	var warnings []models.HistoryWarning
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
			// Skip images if model doesn't support vision
			if stripImages {
				continue
			}
			// Check if it's a supported image type
			if isImageMimeType(part.InlineData.MimeType) {
				hasMultimodal = true
				dataURL := fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, part.InlineData.Data)
				contentParts = append(contentParts, ContentPart{
					Type: "image_url",
					ImageURL: &ImageURL{
						URL: dataURL,
					},
				})
			} else {
				log.Printf("Warning: Skipping non-image InlineData (mime: %s)", part.InlineData.MimeType)
				warnings = append(warnings, models.HistoryWarning{
					Type:    "unsupported_content",
					Message: "File type not supported",
					Details: fmt.Sprintf("Only images are supported, skipping %s", part.InlineData.MimeType),
				})
			}
		}

		// Handle file data (URLs) - only send images, skip PDFs and other files
		if part.FileData != nil {
			// Skip images if model doesn't support vision
			if stripImages && isImageMimeType(part.FileData.MimeType) {
				continue
			}
			if part.FileData.FileUrl != "" {
				// Check if it's an image type that OpenRouter/providers support
				if isImageMimeType(part.FileData.MimeType) {
					hasMultimodal = true
					contentParts = append(contentParts, ContentPart{
						Type: "image_url",
						ImageURL: &ImageURL{
							URL: part.FileData.FileUrl,
						},
					})
				} else {
					// Non-image file (PDF, etc.) - skip with warning
					log.Printf("Warning: Skipping non-image file in history (mime: %s)", part.FileData.MimeType)
					warnings = append(warnings, models.HistoryWarning{
						Type:    "unsupported_content",
						Message: "File type not supported by this model",
						Details: fmt.Sprintf("Only images are supported, skipping %s file", part.FileData.MimeType),
					})
				}
			} else if part.FileData.GoogleUri != nil && *part.FileData.GoogleUri != "" {
				// File was uploaded to Gemini but we don't have a public URL
				log.Printf("Warning: FileData has GoogleUri but no FileUrl, cannot use for OpenRouter")
				warnings = append(warnings, models.HistoryWarning{
					Type:    "unsupported_content",
					Message: "File not available for this model",
					Details: "Files uploaded to Gemini cannot be accessed by other models",
				})
			}
		}

		// Handle image data
		if part.ImageData != nil {
			// Skip images if model doesn't support vision
			if stripImages {
				continue
			}
			if part.ImageData.FileUrl != "" {
				// Check if it's an image type that OpenRouter/providers support
				if isImageMimeType(part.ImageData.MimeType) {
					hasMultimodal = true
					contentParts = append(contentParts, ContentPart{
						Type: "image_url",
						ImageURL: &ImageURL{
							URL: part.ImageData.FileUrl,
						},
					})
				} else {
					log.Printf("Warning: Skipping non-image ImageData (mime: %s)", part.ImageData.MimeType)
					warnings = append(warnings, models.HistoryWarning{
						Type:    "unsupported_content",
						Message: "File type not supported",
						Details: fmt.Sprintf("Only images are supported, skipping %s", part.ImageData.MimeType),
					})
				}
			} else {
				log.Printf("Warning: ImageData has no FileUrl")
				warnings = append(warnings, models.HistoryWarning{
					Type:    "unsupported_content",
					Message: "Image not available",
					Details: "Image reference is missing URL",
				})
			}
		}
	}

	if len(contentParts) == 0 {
		return nil, warnings
	}

	// Return simple string if text-only
	if !hasMultimodal && len(textParts) > 0 {
		return strings.Join(textParts, "\n"), warnings
	}

	// Return content parts for multimodal
	return contentParts, warnings
}

// isImageMimeType checks if the mime type is a supported image format
// Most providers support jpeg, png, webp, and gif
func isImageMimeType(mimeType string) bool {
	switch mimeType {
	case "image/jpeg", "image/jpg", "image/png", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

// buildContentFromUserParts builds message content from user parts (legacy wrapper)
// Returns string for text-only, []ContentPart for multimodal
func (o *OpenRouter_Model) buildContentFromUserParts(parts []models.User_Part) interface{} {
	content, _ := o.buildContentFromUserPartsWithWarnings(parts, false)
	return content
}
