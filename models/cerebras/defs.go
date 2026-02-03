package cerebras

import "github.com/Desarso/godantic/models"

// Cerebras API Request/Response types (OpenAI-compatible format)

// Request types

type CerebrasRequest struct {
	Model       string      `json:"model"`
	Messages    []Message   `json:"messages"`
	Tools       []Tool      `json:"tools,omitempty"`
	ToolChoice  interface{} `json:"tool_choice,omitempty"` // "auto", "none", or specific tool
	Stream      bool        `json:"stream,omitempty"`
	MaxTokens   *int        `json:"max_tokens,omitempty"`
	Temperature *float64    `json:"temperature,omitempty"`
	TopP        *float64    `json:"top_p,omitempty"`
	Seed        *int        `json:"seed,omitempty"`
}

type Message struct {
	Role       string      `json:"role"`              // "system", "user", "assistant", "tool"
	Content    interface{} `json:"content,omitempty"` // string or []ContentPart for multimodal
	Name       *string     `json:"name,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`   // For assistant messages with tool calls
	ToolCallID *string     `json:"tool_call_id,omitempty"` // For tool response messages
}

type ContentPart struct {
	Type     string    `json:"type"` // "text" or "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL    string  `json:"url"`
	Detail *string `json:"detail,omitempty"` // "auto", "low", "high"
}

type Tool struct {
	Type     string       `json:"type"` // "function"
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"` // JSON Schema object
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string of arguments
}

// Response types

type CerebrasResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"` // "chat.completion"
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	Usage             *Usage   `json:"usage,omitempty"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}

type Choice struct {
	Index        int      `json:"index"`
	Message      Message  `json:"message,omitempty"`       // For non-streaming
	Delta        *Message `json:"delta,omitempty"`         // For streaming
	FinishReason *string  `json:"finish_reason,omitempty"` // "stop", "tool_calls", "length", etc.
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Streaming response (Server-Sent Events format)
type StreamResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"` // "chat.completion.chunk"
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}

// Error response - Cerebras returns error fields at root level, not nested
type ErrorResponse struct {
	Message string      `json:"message"`
	Type    string      `json:"type"`
	Param   interface{} `json:"param,omitempty"`
	Code    string      `json:"code,omitempty"`
}

// Legacy nested error structure (kept for backwards compatibility)
type CerebrasError struct {
	Message string      `json:"message"`
	Type    string      `json:"type"`
	Param   interface{} `json:"param,omitempty"`
	Code    string      `json:"code,omitempty"`
}

// Helper function to convert FunctionDeclaration to Cerebras Tool format
func ConvertToCerebrasTool(fd models.FunctionDeclaration) Tool {
	return Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        fd.Name,
			Description: fd.Description,
			Parameters:  fd.Parameters,
		},
	}
}

// Helper function to convert multiple FunctionDeclarations to Cerebras Tools
func ConvertToCerebrasTools(fds []models.FunctionDeclaration) []Tool {
	tools := make([]Tool, len(fds))
	for i, fd := range fds {
		tools[i] = ConvertToCerebrasTool(fd)
	}
	return tools
}
