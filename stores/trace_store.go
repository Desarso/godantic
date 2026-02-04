package stores

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ExecutionTrace represents a single trace event stored in the database
// Indexed by conversation_id and tool_call_id for efficient retrieval
type ExecutionTrace struct {
	ID             uint           `gorm:"primarykey" json:"-"`
	CreatedAt      time.Time      `json:"-"`
	ConversationID string         `gorm:"index:idx_trace_conv;not null" json:"conversation_id"`
	ToolCallID     string         `gorm:"index:idx_trace_conv;index:idx_trace_tool;not null" json:"tool_call_id"`
	TraceID        string         `gorm:"not null" json:"trace_id"`
	ParentID       string         `json:"parent_id,omitempty"`
	Tool           string         `json:"tool"`
	Operation      string         `json:"operation"`
	Status         string         `gorm:"not null" json:"status"` // start, progress, end, error
	Label          string         `gorm:"not null" json:"label"`
	DetailsJSON    string         `gorm:"type:text" json:"-"`         // Stored as JSON string
	Details        map[string]any `gorm:"-" json:"details,omitempty"` // Not stored, computed from DetailsJSON
	Timestamp      int64          `gorm:"not null" json:"timestamp"`
	DurationMS     int64          `json:"duration_ms,omitempty"`
}

// BeforeSave marshals Details to DetailsJSON
func (t *ExecutionTrace) BeforeSave(tx *gorm.DB) error {
	if t.Details != nil {
		data, err := json.Marshal(t.Details)
		if err != nil {
			return err
		}
		t.DetailsJSON = string(data)
	}
	return nil
}

// AfterFind unmarshals DetailsJSON to Details
func (t *ExecutionTrace) AfterFind(tx *gorm.DB) error {
	if t.DetailsJSON != "" {
		return json.Unmarshal([]byte(t.DetailsJSON), &t.Details)
	}
	return nil
}

// TraceStore interface for trace persistence operations
type TraceStore interface {
	// SaveTrace saves a single trace event
	SaveTrace(trace *ExecutionTrace) error

	// SaveTraces saves multiple trace events in a batch
	SaveTraces(traces []*ExecutionTrace) error

	// GetTracesByConversation retrieves all traces for a conversation
	GetTracesByConversation(conversationID string) ([]*ExecutionTrace, error)

	// GetTracesByToolCall retrieves all traces for a specific tool call
	GetTracesByToolCall(toolCallID string) ([]*ExecutionTrace, error)

	// DeleteTracesByConversation removes all traces for a conversation
	DeleteTracesByConversation(conversationID string) error
}

// SQLiteTraceStore implements TraceStore for SQLite/PostgreSQL via GORM
type GORMTraceStore struct {
	db *gorm.DB
}

// NewGORMTraceStore creates a trace store from an existing GORM database connection
func NewGORMTraceStore(db *gorm.DB) (*GORMTraceStore, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	// Auto-migrate the trace table
	if err := db.AutoMigrate(&ExecutionTrace{}); err != nil {
		return nil, fmt.Errorf("failed to migrate execution_traces table: %w", err)
	}

	return &GORMTraceStore{db: db}, nil
}

// SaveTrace saves a single trace event
func (s *GORMTraceStore) SaveTrace(trace *ExecutionTrace) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}
	return s.db.Create(trace).Error
}

// SaveTraces saves multiple trace events in a batch
func (s *GORMTraceStore) SaveTraces(traces []*ExecutionTrace) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}
	if len(traces) == 0 {
		return nil
	}
	return s.db.CreateInBatches(traces, 100).Error
}

// GetTracesByConversation retrieves all traces for a conversation, ordered by timestamp
func (s *GORMTraceStore) GetTracesByConversation(conversationID string) ([]*ExecutionTrace, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	var traces []*ExecutionTrace
	err := s.db.Where("conversation_id = ?", conversationID).
		Order("timestamp ASC").
		Find(&traces).Error

	return traces, err
}

// GetTracesByToolCall retrieves all traces for a specific tool call
func (s *GORMTraceStore) GetTracesByToolCall(toolCallID string) ([]*ExecutionTrace, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	var traces []*ExecutionTrace
	err := s.db.Where("tool_call_id = ?", toolCallID).
		Order("timestamp ASC").
		Find(&traces).Error

	return traces, err
}

// DeleteTracesByConversation removes all traces for a conversation
func (s *GORMTraceStore) DeleteTracesByConversation(conversationID string) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}
	return s.db.Where("conversation_id = ?", conversationID).Delete(&ExecutionTrace{}).Error
}
