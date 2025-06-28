package models

import "time"

// ChatMessageResponse defines the structure for messages returned by the chat history API endpoint.
// It excludes internal DB fields like gorm.Model but includes necessary identifiers and timestamps.
type ChatMessageResponse struct {
	ID             uint        `json:"id"`         // Message primary key ID
	CreatedAt      time.Time   `json:"created_at"` // Time the message was created
	UpdatedAt      time.Time   `json:"updated_at"` // Time the message was last updated
	ConversationID string      `json:"conversation_id"`
	Sequence       int         `json:"sequence"`
	Role           string      `json:"role"`                  // "user", "model"
	Type           string      `json:"type"`                  // "user_message", "model_message", "function_call", "function_response"
	FunctionID     string      `json:"function_id,omitempty"` // Associated function call ID (potentially linking bundles)
	Text           string      `json:"text,omitempty"`        // Primary text content, if applicable (extracted from parts)
	Parts          interface{} `json:"parts,omitempty"`       // Unmarshalled parts array (e.g., []User_Part, []Model_Part)
}
