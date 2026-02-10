package anthropic

import "github.com/Desarso/godantic/models"

// Anthropic Messages API types

// AnthropicRequest is the request body for the Messages API.
type AnthropicRequest struct {
	Model       string           `json:"model"`
	MaxTokens   int              `json:"max_tokens"`
	Messages    []AnthropicMsg   `json:"messages"`
	System      string           `json:"system,omitempty"`
	Tools       []AnthropicTool  `json:"tools,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
	TopP        *float64         `json:"top_p,omitempty"`
}

// AnthropicMsg is a message in the Anthropic format.
type AnthropicMsg struct {
	Role    string      `json:"role"` // "user" or "assistant"
	Content interface{} `json:"content"` // string or []ContentBlock
}

// ContentBlock is a polymorphic content element.
type ContentBlock struct {
	Type      string      `json:"type"`
	Text      string      `json:"text,omitempty"`
	ID        string      `json:"id,omitempty"`          // tool_use ID
	Name      string      `json:"name,omitempty"`        // tool name
	Input     interface{} `json:"input,omitempty"`       // tool input (map)
	ToolUseID string      `json:"tool_use_id,omitempty"` // for tool_result
	Content   interface{} `json:"content,omitempty"`     // for tool_result (string or nested blocks)
	IsError   bool        `json:"is_error,omitempty"`    // for tool_result
	Source    *ImageSource `json:"source,omitempty"`     // for image
}

// ImageSource for base64-encoded images.
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", etc.
	Data      string `json:"data"`
}

// AnthropicTool defines a tool for the Anthropic API.
type AnthropicTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"input_schema"`
}

// AnthropicResponse is the non-streaming response.
type AnthropicResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"` // "message"
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ErrorResponse from the API.
type ErrorResponse struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Streaming SSE event types
const (
	EventMessageStart      = "message_start"
	EventMessageDelta      = "message_delta"
	EventMessageStop       = "message_stop"
	EventContentBlockStart = "content_block_start"
	EventContentBlockDelta = "content_block_delta"
	EventContentBlockStop  = "content_block_stop"
)

// SanitizedInputSchema ensures proper structure for Anthropic tool schemas.
type SanitizedInputSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

// ConvertToAnthropicTool converts a godantic FunctionDeclaration to an Anthropic tool.
func ConvertToAnthropicTool(fd models.FunctionDeclaration) AnthropicTool {
	schema := SanitizedInputSchema{
		Type:       fd.Parameters.Type,
		Properties: fd.Parameters.Properties,
		Required:   fd.Parameters.Required,
	}
	if schema.Properties == nil {
		schema.Properties = make(map[string]interface{})
	}
	if schema.Type == "" {
		schema.Type = "object"
	}
	return AnthropicTool{
		Name:        fd.Name,
		Description: fd.Description,
		InputSchema: schema,
	}
}

// ConvertToAnthropicTools converts multiple FunctionDeclarations.
func ConvertToAnthropicTools(fds []models.FunctionDeclaration) []AnthropicTool {
	tools := make([]AnthropicTool, len(fds))
	for i, fd := range fds {
		tools[i] = ConvertToAnthropicTool(fd)
	}
	return tools
}
