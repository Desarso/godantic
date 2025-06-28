package stores

import (
	"encoding/json"
	"fmt"
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// SQLiteStore implements MessageStore for SQLite databases
type SQLiteStore struct {
	db   *gorm.DB
	path string
}

// NewSQLiteStore creates a new SQLite store
func NewSQLiteStore(config *StoreConfig) (*SQLiteStore, error) {
	if config.Type != "sqlite" {
		return nil, fmt.Errorf("invalid store type for SQLite store: %s", config.Type)
	}

	store := &SQLiteStore{
		path: config.Connection,
	}

	if err := store.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to SQLite database: %w", err)
	}

	return store, nil
}

// NewSQLiteStoreSimple creates a new SQLite store with just a file path
func NewSQLiteStoreSimple(dbPath string) (*SQLiteStore, error) {
	config := NewStoreConfig("sqlite", dbPath)
	return NewSQLiteStore(config)
}

// Connect establishes a connection to the SQLite database
func (s *SQLiteStore) Connect() error {
	db, err := gorm.Open(sqlite.Open(s.path), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to connect to SQLite database: %w", err)
	}

	s.db = db

	// Auto-migrate the schema
	if err := s.db.AutoMigrate(&Conversation{}, &Message{}); err != nil {
		return fmt.Errorf("failed to migrate database schema: %w", err)
	}

	return nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	if s.db != nil {
		sqlDB, err := s.db.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

// Ping checks if the database connection is alive
func (s *SQLiteStore) Ping() error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}

	return sqlDB.Ping()
}

// SaveMessage saves a message to the database
func (s *SQLiteStore) SaveMessage(sessionID, role, messageType string, parts interface{}, functionID string) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	var count int64
	if err := s.db.Model(&Message{}).Where("conversation_id = ?", sessionID).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to count existing messages: %w", err)
	}

	seq := int(count) + 1

	// Marshal the provided parts into JSON
	partsJSONBytes, err := json.Marshal(parts)
	if err != nil {
		log.Printf("Error marshalling parts for DB storage (ConvID: %s): %v", sessionID, err)
		return fmt.Errorf("failed to marshal parts for database: %w", err)
	}
	partsJSONStr := string(partsJSONBytes)

	// Ensure partsJSONStr is not empty or just "null"
	if parts == nil || partsJSONStr == "null" || partsJSONStr == "[]" {
		log.Printf("Warning: Saving message with empty/null parts for ConvID: %s, Role: %s, Type: %s", sessionID, role, messageType)
		partsJSONStr = "{}" // Save as empty JSON object
	}

	msg := Message{
		ConversationID: sessionID,
		Sequence:       seq,
		Role:           role,
		Type:           messageType,
		PartsJSON:      partsJSONStr,
		FunctionID:     functionID,
	}

	tx := s.db.Begin()
	if err := tx.Create(&msg).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to create message record: %w", err)
	}

	if err := tx.Model(&Conversation{}).Where("conversation_id = ?", sessionID).Update("message_count", seq).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update conversation message count: %w", err)
	}

	return tx.Commit().Error
}

// FetchHistory retrieves messages for a conversation in sequence order
func (s *SQLiteStore) FetchHistory(sessionID string) ([]Message, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	var msgs []Message
	if err := s.db.Where("conversation_id = ?", sessionID).Order("sequence asc").Find(&msgs).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}

	return msgs, nil
}

// CreateConversation creates a new conversation record
func (s *SQLiteStore) CreateConversation(convoID, userID string) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	conv := Conversation{
		ConversationID: convoID,
		UserID:         userID,
		MessageCount:   0,
	}

	return s.db.Create(&conv).Error
}

// ListConversations returns all conversation IDs
func (s *SQLiteStore) ListConversations() ([]string, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	var convs []Conversation
	if err := s.db.Find(&convs).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch conversations: %w", err)
	}

	ids := make([]string, len(convs))
	for i, c := range convs {
		ids[i] = c.ConversationID
	}

	return ids, nil
}
