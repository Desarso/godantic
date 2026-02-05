package models

// HistoryWarning represents a warning about content that was filtered/modified
// when adapting conversation history for a specific model
type HistoryWarning struct {
	Type    string `json:"type"`    // "unsupported_content", "conversion_error", etc.
	Message string `json:"message"` // Human-readable description
	Details string `json:"details"` // Additional details (e.g., which message, what content)
}

type Model_Response struct {
	Parts    []Model_Part     `json:"parts"`
	Warnings []HistoryWarning `json:"warnings,omitempty"` // Warnings about history adaptation (only sent in first chunk)
}

//may be a string or a function call and it will be parts

type FunctionCall struct {
	ID   string                 `json:"id,omitempty"` // Unique ID for this specific call instance
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type Model_Part struct {
	Text         *string       `json:"text,omitempty"`
	FunctionCall *FunctionCall `json:"functionCall,omitempty"`
	Reasoning    *string       `json:"reasoning,omitempty"` // Chain-of-thought reasoning content
}

type Model_Text_Part struct {
	Text string `json:"text"`
}

type Model_Text_Part_Delta struct {
	Text string `json:"text"`
}

type Model_Function_Call_Part struct {
	FunctionCall FunctionCall `json:"functionCall"`
}
