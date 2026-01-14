package sessions

import (
	"log"
	"time"

	"github.com/Desarso/godantic/models"
	"github.com/Desarso/godantic/stores"
	"github.com/gorilla/websocket"
)

// AgentError represents errors that can occur during agent operations
type AgentError struct {
	Message string
	Fatal   bool
}

func (e *AgentError) Error() string {
	return e.Message
}

// WebSocketWriter handles all WebSocket communication
type WebSocketWriter struct {
	Conn             *websocket.Conn
	Logger           *log.Logger
	StartTime        time.Time
	FirstTokenTime   *time.Time
	FirstTokenLogged bool
}

func (w *WebSocketWriter) WriteResponse(resp interface{}) error {
	// Track time to first token
	if !w.FirstTokenLogged && w.FirstTokenTime == nil && !w.StartTime.IsZero() {
		now := time.Now()
		w.FirstTokenTime = &now
		timeToFirstToken := now.Sub(w.StartTime)
		w.Logger.Printf("Time to first token: %v", timeToFirstToken)
		w.FirstTokenLogged = true
	}
	return w.Conn.WriteJSON(resp)
}

func (w *WebSocketWriter) WriteError(message string) error {
	return w.Conn.WriteJSON(map[string]string{"error": message})
}

func (w *WebSocketWriter) WriteDone() error {
	return w.Conn.WriteJSON(map[string]string{"type": "done"})
}

// WebSocketToolResultMessage represents tool results sent over WebSocket
type WebSocketToolResultMessage struct {
	Type         string                 `json:"type"` // e.g., "tool_result"
	FunctionName string                 `json:"function_name"`
	FunctionID   string                 `json:"function_id"`
	Result       map[string]interface{} `json:"result"`      // Parsed result data
	ResultJSON   string                 `json:"result_json"` // Raw JSON string of result
}

// AgentSession encapsulates WebSocket agent interaction logic
type AgentSession struct {
	Agent     AgentInterface
	SessionID string
	Writer    *WebSocketWriter
	Store     stores.MessageStore
	Logger    *log.Logger
	History   []stores.Message
}

// HTTPSession handles HTTP-based chat interactions
type HTTPSession struct {
	Agent          AgentInterface
	ConversationID string
	Store          stores.MessageStore
	Logger         *log.Logger
}

// SSEWriter handles Server-Sent Events writing
type SSEWriter interface {
	WriteSSE(data string) error
	WriteSSEError(err error) error
	Flush()
}

// AgentInterface defines the interface that agents must implement
type AgentInterface interface {
	Run(request models.Model_Request, history []stores.Message) (models.Model_Response, error)
	Run_Stream(request models.Model_Request, history []stores.Message) (<-chan models.Model_Response, <-chan error)
	ExecuteTool(name string, args map[string]interface{}, sessionID string) (string, error)
	ApproveTool(name string, args map[string]interface{}) (bool, error)
}
