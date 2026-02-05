package stores

import (
	"encoding/json"
	"fmt"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// PostgresStore implements MessageStore for PostgreSQL databases
type PostgresStore struct {
	db  *gorm.DB
	dsn string
}

// NewPostgresStore creates a new PostgreSQL store
func NewPostgresStore(config *StoreConfig) (*PostgresStore, error) {
	if config.Type != "postgres" {
		return nil, fmt.Errorf("invalid store type for PostgreSQL store: %s", config.Type)
	}

	store := &PostgresStore{
		dsn: config.Connection,
	}

	if err := store.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL database: %w", err)
	}

	return store, nil
}

// NewPostgresStoreSimple creates a new PostgreSQL store with just a DSN
func NewPostgresStoreSimple(dsn string) (*PostgresStore, error) {
	config := NewStoreConfig("postgres", dsn)
	return NewPostgresStore(config)
}

// Connect establishes a connection to the PostgreSQL database
func (s *PostgresStore) Connect() error {
	db, err := gorm.Open(postgres.Open(s.dsn), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL database: %w", err)
	}

	s.db = db

	// Auto-migrate the schema
	if err := s.db.AutoMigrate(&Conversation{}, &Message{}); err != nil {
		return fmt.Errorf("failed to migrate database schema: %w", err)
	}

	return nil
}

// Close closes the database connection
func (s *PostgresStore) Close() error {
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
func (s *PostgresStore) Ping() error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}

	return sqlDB.Ping()
}

// SaveMessage saves a message to the database (without user association - for backward compatibility)
func (s *PostgresStore) SaveMessage(sessionID, role, messageType string, parts interface{}, functionID string) error {
	return s.SaveMessageWithUser(sessionID, "", role, messageType, parts, functionID)
}

// SaveMessageWithUser saves a message to the database with user association
func (s *PostgresStore) SaveMessageWithUser(sessionID, userID, role, messageType string, parts interface{}, functionID string) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// Ensure conversation record exists (create if first message)
	var convCount int64
	if err := s.db.Model(&Conversation{}).Where("conversation_id = ?", sessionID).Count(&convCount).Error; err != nil {
		log.Printf("Warning: Error checking for conversation %s: %v", sessionID, err)
	} else if convCount == 0 {
		// Conversation doesn't exist, create it with user ID
		if err := s.CreateConversation(sessionID, userID); err != nil {
			log.Printf("Warning: Failed to create conversation record for %s: %v", sessionID, err)
		}
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
// limit: maximum number of messages to retrieve (0 = return all messages)
// The returned history is sanitized to ensure valid turn structure for LLM APIs.
func (s *PostgresStore) FetchHistory(sessionID string, limit int) ([]Message, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	var msgs []Message
	query := s.db.Where("conversation_id = ?", sessionID).Order("sequence ASC")

	if limit > 0 {
		// Get total count first
		var count int64
		if err := s.db.Model(&Message{}).Where("conversation_id = ?", sessionID).Count(&count).Error; err != nil {
			return nil, fmt.Errorf("failed to count messages: %w", err)
		}

		// If more than limit, offset to get only last N messages
		// Fetch extra to allow sanitization to find valid start point
		// Need larger buffer because tool cycles can be long (multiple calls + responses)
		if count > int64(limit) {
			// Fetch extra messages in case we need to skip orphaned function_responses
			// Use 2x limit as buffer to handle long tool call sequences
			extraBuffer := limit
			if extraBuffer < 10 {
				extraBuffer = 10
			}
			offset := int(count) - limit - extraBuffer
			if offset < 0 {
				offset = 0
			}
			query = query.Offset(offset)
		}
	}

	if err := query.Find(&msgs).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}

	// Sanitize history to ensure valid turn structure
	// This handles truncation breaking tool cycles and corrupted history
	msgs = SanitizeHistory(msgs)

	// If we fetched extra and now have more than limit, trim to limit
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
		// Re-sanitize after trimming to ensure we still have valid start
		msgs = SanitizeHistory(msgs)
	}

	return msgs, nil
}

// CreateConversation creates a new conversation record
func (s *PostgresStore) CreateConversation(convoID, userID string) error {
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
func (s *PostgresStore) ListConversations() ([]string, error) {
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

// ListConversationsForUser returns all conversations with details for a specific user
// MessageCount is computed on the fly from the messages table
func (s *PostgresStore) ListConversationsForUser(userID string) ([]ConversationInfo, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	// Query conversations with computed message count via subquery
	type ConvWithCount struct {
		Conversation
		ComputedMessageCount int `gorm:"column:computed_message_count"`
	}

	var convs []ConvWithCount
	err := s.db.Model(&Conversation{}).
		Select("conversations.*, (SELECT COUNT(*) FROM messages WHERE messages.conversation_id = conversations.conversation_id) as computed_message_count").
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Find(&convs).Error

	if err != nil {
		return nil, fmt.Errorf("failed to fetch conversations: %w", err)
	}

	result := make([]ConversationInfo, len(convs))
	for i, c := range convs {
		result[i] = ConversationInfo{
			ConversationID: c.ConversationID,
			UserID:         c.UserID,
			Title:          c.Title,
			MessageCount:   c.ComputedMessageCount, // Use computed count, not stored
			CreatedAt:      c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt:      c.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	return result, nil
}
