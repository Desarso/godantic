package stores

import (
	"gorm.io/gorm"
)

// Message represents any chat message or function interaction within a conversation turn.
// This matches the structure from helpers/DBManager/models.go exactly
type Message struct {
	gorm.Model
	ConversationID string `gorm:"index;not null"`
	Sequence       int    `gorm:"not null"`
	Role           string `gorm:"not null"` // "user", "model"
	Type           string `gorm:"not null"` // "user_message", "model_message", "function_call", "function_response"
	// FunctionID might be used to link a function_response bundle back to a function_call bundle, TBD if needed.
	FunctionID string `gorm:"index" json:"function_id,omitempty"` // Kept for potential linking
	// PartsJSON stores the JSON marshaled array of content parts for this turn.
	// This could be []models.User_Part or []models.Model_Part depending on the Role/Type.
	PartsJSON string `gorm:"type:json"`
}

// Conversation holds metadata for a chat conversation
// This matches the structure from helpers/DBManager/models.go exactly
type Conversation struct {
	gorm.Model
	ConversationID string    `gorm:"uniqueIndex;not null"`
	UserID         string    `gorm:"index;not null"`
	Title          string    `gorm:"type:text"` // Conversation title (migrated from old system or AI-generated)
	MessageCount   int       `gorm:"default:0"`
	Messages       []Message `gorm:"foreignKey:ConversationID;references:ConversationID"`
}

// ConversationInfo holds basic conversation metadata for listing
type ConversationInfo struct {
	ConversationID string
	UserID         string
	Title          string
	MessageCount   int
	CreatedAt      string
	UpdatedAt      string
}

// MessageStore interface for abstracting database operations
type MessageStore interface {
	// Message operations
	SaveMessage(sessionID, role, messageType string, parts interface{}, functionID string) error
	FetchHistory(sessionID string, limit int) ([]Message, error)

	// Conversation operations
	CreateConversation(convoID, userID string) error
	ListConversations() ([]string, error)
	ListConversationsForUser(userID string) ([]ConversationInfo, error) // Returns conversations with details for a user

	// Connection management
	Connect() error
	Close() error

	// Health check
	Ping() error
}

// StoreConfig holds configuration for database stores
type StoreConfig struct {
	Type       string            `json:"type"`       // "sqlite", "postgres", "mysql", etc.
	Connection string            `json:"connection"` // connection string
	Options    map[string]string `json:"options"`    // additional options
}

// NewStoreConfig creates a new store configuration
func NewStoreConfig(storeType, connection string) *StoreConfig {
	return &StoreConfig{
		Type:       storeType,
		Connection: connection,
		Options:    make(map[string]string),
	}
}

// WithOption adds an option to the store configuration
func (c *StoreConfig) WithOption(key, value string) *StoreConfig {
	c.Options[key] = value
	return c
}
