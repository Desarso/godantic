package anthropic

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
	DefaultBaseURL    = "https://api.anthropic.com/v1/messages"
	DefaultAPIVersion = "2023-06-01"
	DefaultModel      = "claude-sonnet-4-20250514"
	DefaultMaxTokens  = 4096
)

func init() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}
}

// Anthropic_Model implements the godantic Model interface for the Anthropic Messages API.
type Anthropic_Model struct {
	Model        string
	Temperature  *float64
	MaxTokens    *int
	SystemPrompt string
	BaseURL      string   // Optional: custom API endpoint
	APIKeyEnv    string   // Optional: env var name for API key (defaults to ANTHROPIC_API_KEY)
	SupportsVision bool

	WarningCallback func(warnings []models.HistoryWarning) `json:"-"`
}

// SetHistoryWarningCallback implements the HistoryWarner interface.
func (a *Anthropic_Model) SetHistoryWarningCallback(callback func(warnings []models.HistoryWarning)) {
	a.WarningCallback = callback
}

// Model_Request implements the Model interface for non-streaming requests.
func (a *Anthropic_Model) Model_Request(request models.Model_Request, tools []models.FunctionDeclaration, conversationHistory []stores.Message) (models.Model_Response, error) {
	if request.User_Message == nil && request.Tool_Results == nil {
		return models.Model_Response{}, fmt.Errorf("request must contain either user message or tool results")
	}

	var msg models.User_Message
	if request.User_Message != nil {
		msg = *request.User_Message
	}

	modelToUse := a.Model
	if modelToUse == "" {
		modelToUse = DefaultModel
	}

	anthropicReq, err := a.buildRequest(modelToUse, msg, tools, request.Tool_Results, conversationHistory, false)
	if err != nil {
		return models.Model_Response{}, err
	}

	jsonBytes, err := json.Marshal(anthropicReq)
	if err != nil {
		return models.Model_Response{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	baseURL := a.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	req, err := http.NewRequest("POST", baseURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return models.Model_Response{}, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	a.setHeaders(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return models.Model_Response{}, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return models.Model_Response{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return models.Model_Response{}, fmt.Errorf("Anthropic API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var anthropicResp AnthropicResponse
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		return models.Model_Response{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return a.toModelResponse(anthropicResp), nil
}

// Stream_Model_Request implements the Model interface for streaming requests.
func (a *Anthropic_Model) Stream_Model_Request(request models.Model_Request, tools []models.FunctionDeclaration, conversationHistory []stores.Message) (<-chan models.Model_Response, <-chan error) {
	respChan := make(chan models.Model_Response)
	errChan := make(chan error, 1)

	if request.User_Message == nil && request.Tool_Results == nil {
		errChan <- fmt.Errorf("request must contain either user message or tool results")
		close(errChan)
		close(respChan)
		return respChan, errChan
	}

	var msg models.User_Message
	if request.User_Message != nil {
		msg = *request.User_Message
	}

	modelToUse := a.Model
	if modelToUse == "" {
		modelToUse = DefaultModel
	}

	go func() {
		defer close(respChan)
		defer close(errChan)

		anthropicReq, err := a.buildRequest(modelToUse, msg, tools, request.Tool_Results, conversationHistory, true)
		if err != nil {
			errChan <- err
			return
		}

		jsonBytes, err := json.Marshal(anthropicReq)
		if err != nil {
			errChan <- fmt.Errorf("failed to marshal request: %w", err)
			return
		}

		baseURL := a.BaseURL
		if baseURL == "" {
			baseURL = DefaultBaseURL
		}

		req, err := http.NewRequest("POST", baseURL, bytes.NewReader(jsonBytes))
		if err != nil {
			errChan <- fmt.Errorf("failed to create HTTP request: %w", err)
			return
		}
		a.setHeaders(req)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			errChan <- fmt.Errorf("HTTP request failed: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errChan <- fmt.Errorf("Anthropic API error: status %d, body: %s", resp.StatusCode, string(body))
			return
		}

		a.parseSSEStream(resp.Body, respChan, errChan)
	}()

	return respChan, errChan
}

// parseSSEStream reads Anthropic SSE events and sends Model_Response chunks.
func (a *Anthropic_Model) parseSSEStream(r io.Reader, respChan chan<- models.Model_Response, errChan chan<- error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Track tool use blocks being built
	type toolBlock struct {
		id   string
		name string
		json strings.Builder
	}
	toolBlocks := make(map[int]*toolBlock)

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var raw struct {
			Type         string          `json:"type"`
			Index        int             `json:"index"`
			Message      json.RawMessage `json:"message"`
			ContentBlock json.RawMessage `json:"content_block"`
			Delta        json.RawMessage `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			continue
		}

		switch raw.Type {
		case EventContentBlockStart:
			if raw.ContentBlock != nil {
				var block ContentBlock
				json.Unmarshal(raw.ContentBlock, &block)
				if block.Type == "tool_use" {
					toolBlocks[raw.Index] = &toolBlock{
						id:   block.ID,
						name: block.Name,
					}
				}
			}

		case EventContentBlockDelta:
			if raw.Delta != nil {
				var delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				}
				json.Unmarshal(raw.Delta, &delta)

				if delta.Type == "text_delta" && delta.Text != "" {
					text := delta.Text
					respChan <- models.Model_Response{
						Parts: []models.Model_Part{{Text: &text}},
					}
				} else if delta.Type == "input_json_delta" {
					if tb, ok := toolBlocks[raw.Index]; ok {
						tb.json.WriteString(delta.PartialJSON)
					}
				}
			}

		case EventContentBlockStop:
			// Finalize tool call if this was a tool_use block
			if tb, ok := toolBlocks[raw.Index]; ok {
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(tb.json.String()), &args); err != nil {
					args = map[string]interface{}{}
				}
				respChan <- models.Model_Response{
					Parts: []models.Model_Part{
						{
							FunctionCall: &models.FunctionCall{
								ID:   tb.id,
								Name: tb.name,
								Args: args,
							},
						},
					},
				}
				delete(toolBlocks, raw.Index)
			}

		case EventMessageStop:
			return
		}
	}

	if err := scanner.Err(); err != nil {
		errChan <- fmt.Errorf("error reading stream: %w", err)
	}
}

// toModelResponse converts an Anthropic response to godantic's Model_Response.
func (a *Anthropic_Model) toModelResponse(resp AnthropicResponse) models.Model_Response {
	modelResp := models.Model_Response{}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				text := block.Text
				modelResp.Parts = append(modelResp.Parts, models.Model_Part{Text: &text})
			}
		case "tool_use":
			args := make(map[string]interface{})
			if block.Input != nil {
				// Input can be a map or raw JSON
				switch v := block.Input.(type) {
				case map[string]interface{}:
					args = v
				default:
					b, _ := json.Marshal(v)
					json.Unmarshal(b, &args)
				}
			}
			modelResp.Parts = append(modelResp.Parts, models.Model_Part{
				FunctionCall: &models.FunctionCall{
					ID:   block.ID,
					Name: block.Name,
					Args: args,
				},
			})
		}
	}

	return modelResp
}

// buildRequest constructs the Anthropic API request.
func (a *Anthropic_Model) buildRequest(model string, message models.User_Message, tools []models.FunctionDeclaration, toolResults *[]models.Tool_Result, conversationHistory []stores.Message, stream bool) (AnthropicRequest, error) {
	messages := []AnthropicMsg{}

	// Convert conversation history
	for _, histMsg := range conversationHistory {
		msg, err := a.convertHistoryMessage(histMsg)
		if err != nil {
			log.Printf("Warning: Failed to convert history message %d: %v", histMsg.ID, err)
			continue
		}
		if msg != nil {
			messages = append(messages, *msg)
		}
	}

	// Handle tool results
	if toolResults != nil && len(*toolResults) > 0 {
		var blocks []ContentBlock
		for _, tr := range *toolResults {
			blocks = append(blocks, ContentBlock{
				Type:      "tool_result",
				ToolUseID: tr.Tool_ID,
				Content:   tr.Tool_Output,
			})
		}
		messages = append(messages, AnthropicMsg{
			Role:    "user",
			Content: blocks,
		})
	} else {
		// Process current user message
		userMsg := a.convertUserMessage(message)
		if userMsg != nil {
			messages = append(messages, *userMsg)
		}
	}

	if len(messages) == 0 {
		return AnthropicRequest{}, fmt.Errorf("cannot create Anthropic request with no messages")
	}

	// Merge consecutive same-role messages (Anthropic requires alternating roles)
	messages = mergeConsecutiveMessages(messages)

	maxTokens := DefaultMaxTokens
	if a.MaxTokens != nil {
		maxTokens = *a.MaxTokens
	}

	systemPrompt := a.SystemPrompt

	req := AnthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  messages,
		System:    systemPrompt,
		Stream:    stream,
	}

	if len(tools) > 0 {
		req.Tools = ConvertToAnthropicTools(tools)
	}

	if a.Temperature != nil {
		req.Temperature = a.Temperature
	}

	return req, nil
}

// mergeConsecutiveMessages merges consecutive messages with the same role.
// Anthropic requires strictly alternating user/assistant roles.
func mergeConsecutiveMessages(messages []AnthropicMsg) []AnthropicMsg {
	if len(messages) <= 1 {
		return messages
	}

	var result []AnthropicMsg
	for _, msg := range messages {
		if len(result) > 0 && result[len(result)-1].Role == msg.Role {
			// Merge into previous message
			prev := &result[len(result)-1]
			prevBlocks := toContentBlocks(prev.Content)
			newBlocks := toContentBlocks(msg.Content)
			prev.Content = append(prevBlocks, newBlocks...)
		} else {
			result = append(result, msg)
		}
	}
	return result
}

// toContentBlocks converts a message content (string or []ContentBlock) to []ContentBlock.
func toContentBlocks(content interface{}) []ContentBlock {
	switch v := content.(type) {
	case string:
		return []ContentBlock{{Type: "text", Text: v}}
	case []ContentBlock:
		return v
	default:
		// Try JSON roundtrip
		b, _ := json.Marshal(v)
		var blocks []ContentBlock
		if json.Unmarshal(b, &blocks) == nil {
			return blocks
		}
		return nil
	}
}

// convertHistoryMessage converts a stored message to Anthropic format.
func (a *Anthropic_Model) convertHistoryMessage(histMsg stores.Message) (*AnthropicMsg, error) {
	if histMsg.PartsJSON == "" || histMsg.PartsJSON == "{}" || histMsg.PartsJSON == "null" {
		return nil, nil
	}

	role := histMsg.Role

	if role == "user" {
		var userParts []models.User_Part
		if err := json.Unmarshal([]byte(histMsg.PartsJSON), &userParts); err != nil {
			return nil, fmt.Errorf("failed to unmarshal user parts: %w", err)
		}

		// Check for function responses
		for _, part := range userParts {
			if part.FunctionResponse != nil {
				responseBytes, _ := json.Marshal(part.FunctionResponse.Response)
				return &AnthropicMsg{
					Role: "user",
					Content: []ContentBlock{{
						Type:      "tool_result",
						ToolUseID: part.FunctionResponse.ID,
						Content:   string(responseBytes),
					}},
				}, nil
			}
		}

		// Regular user message - extract text
		var texts []string
		for _, part := range userParts {
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
		if len(texts) == 0 {
			return nil, nil
		}
		return &AnthropicMsg{
			Role:    "user",
			Content: strings.Join(texts, "\n"),
		}, nil

	} else if role == "model" {
		var modelParts []models.Model_Part
		if err := json.Unmarshal([]byte(histMsg.PartsJSON), &modelParts); err != nil {
			return nil, fmt.Errorf("failed to unmarshal model parts: %w", err)
		}

		var blocks []ContentBlock
		for _, part := range modelParts {
			if part.Text != nil && *part.Text != "" {
				blocks = append(blocks, ContentBlock{Type: "text", Text: *part.Text})
			}
			if part.FunctionCall != nil {
				blocks = append(blocks, ContentBlock{
					Type:  "tool_use",
					ID:    part.FunctionCall.ID,
					Name:  part.FunctionCall.Name,
					Input: part.FunctionCall.Args,
				})
			}
		}

		if len(blocks) == 0 {
			return nil, nil
		}

		return &AnthropicMsg{
			Role:    "assistant",
			Content: blocks,
		}, nil
	}

	return nil, fmt.Errorf("unknown role: %s", role)
}

// convertUserMessage converts a godantic User_Message to Anthropic format.
func (a *Anthropic_Model) convertUserMessage(message models.User_Message) *AnthropicMsg {
	if len(message.Content.Parts) == 0 {
		return nil
	}

	var texts []string
	for _, part := range message.Content.Parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}

	if len(texts) == 0 {
		return nil
	}

	return &AnthropicMsg{
		Role:    "user",
		Content: strings.Join(texts, "\n"),
	}
}

// setHeaders sets required headers for Anthropic API requests.
func (a *Anthropic_Model) setHeaders(req *http.Request) {
	apiKeyEnv := a.APIKeyEnv
	if apiKeyEnv == "" {
		apiKeyEnv = "ANTHROPIC_API_KEY"
	}
	apiKey := os.Getenv(apiKeyEnv)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("anthropic-version", DefaultAPIVersion)
}
