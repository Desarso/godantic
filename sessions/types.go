package sessions

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Desarso/godantic/models"
	"github.com/Desarso/godantic/stores"
	"github.com/gorilla/websocket"

	eleven_tts "github.com/Desarso/godantic/elevenlabs/tts/multi"
)

// MemoryManager interface for dependency injection - use interface{} to avoid import cycle
type MemoryManager interface {
	AddMemory(content string, metadata map[string]interface{}) error
	RetrieveMemories(queryText string, limit int) ([]string, error)
}

// FlowLogger interface for logging message flow events
// Implement this interface and set it on AgentSession.FlowLogger to receive flow events
type FlowLogger interface {
	// LogUserMessage is called when a user message is received
	LogUserMessage(sessionID string, text string)
	// LogAgentMessage is called when the agent produces a text response
	LogAgentMessage(sessionID string, text string)
	// LogToolCall is called when a tool is about to be executed
	LogToolCall(sessionID string, toolName string, args map[string]interface{})
	// LogToolResult is called when a tool returns a result
	LogToolResult(sessionID string, toolName string, resultPreview string)
}

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
	mu               sync.Mutex
}

func (w *WebSocketWriter) WriteResponse(resp interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()
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
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Conn.WriteJSON(map[string]string{"error": message})
}

func (w *WebSocketWriter) WriteDone() error {
	w.mu.Lock()
	defer w.mu.Unlock()
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

// WebSocketTraceMessage represents a trace event sent over WebSocket
// These are for UI visualization only and are NOT stored in chat history
type WebSocketTraceMessage struct {
	Type       string                 `json:"type"` // "execution_trace"
	TraceID    string                 `json:"trace_id"`
	ParentID   string                 `json:"parent_id,omitempty"`
	ToolCallID string                 `json:"tool_call_id"`      // Links trace to the tool call
	Tool       string                 `json:"tool"`              // e.g., 'web', 'tavily', 'math', 'graph', 'skills'
	Operation  string                 `json:"operation"`         // e.g., 'get', 'search', 'calculate'
	Status     string                 `json:"status"`            // start, progress, end, error
	Label      string                 `json:"label"`             // Human-readable description
	Details    map[string]interface{} `json:"details,omitempty"` // Optional metadata
	Timestamp  int64                  `json:"timestamp"`
	DurationMS int64                  `json:"duration_ms,omitempty"` // Set on 'end' status
}

// WebSocketTraceEmitter implements TraceEmitter by sending traces over WebSocket
type WebSocketTraceEmitter struct {
	Writer     *WebSocketWriter
	ToolCallID string // The tool_call_id this trace belongs to
}

// TraceEvent matches the structure from common_tools
type TraceEvent struct {
	TraceID    string                 `json:"trace_id"`
	ParentID   string                 `json:"parent_id,omitempty"`
	Tool       string                 `json:"tool"`
	Operation  string                 `json:"operation"`
	Status     string                 `json:"status"`
	Label      string                 `json:"label"`
	Details    map[string]interface{} `json:"details,omitempty"`
	Timestamp  int64                  `json:"timestamp"`
	DurationMS int64                  `json:"duration_ms,omitempty"`
}

// EmitTrace sends a trace event over WebSocket
func (e *WebSocketTraceEmitter) EmitTrace(trace TraceEvent) error {
	msg := WebSocketTraceMessage{
		Type:       "execution_trace",
		TraceID:    trace.TraceID,
		ParentID:   trace.ParentID,
		ToolCallID: e.ToolCallID,
		Tool:       trace.Tool,
		Operation:  trace.Operation,
		Status:     trace.Status,
		Label:      trace.Label,
		Details:    trace.Details,
		Timestamp:  trace.Timestamp,
		DurationMS: trace.DurationMS,
	}
	return e.Writer.WriteResponse(msg)
}

// ResponseWaiter allows tools to wait for user input from the frontend
type ResponseWaiter struct {
	responseChan chan string
	isWaiting    bool
	mu           sync.Mutex
}

// NewResponseWaiter creates a new response waiter
func NewResponseWaiter() *ResponseWaiter {
	return &ResponseWaiter{
		responseChan: make(chan string, 1),
		isWaiting:    false,
	}
}

// WaitForResponse blocks until a response is received or timeout
func (rw *ResponseWaiter) WaitForResponse() (string, bool) {
	rw.mu.Lock()
	rw.isWaiting = true
	rw.mu.Unlock()

	defer func() {
		rw.mu.Lock()
		rw.isWaiting = false
		rw.mu.Unlock()
	}()

	response, ok := <-rw.responseChan
	return response, ok
}

// ProvideResponse provides a response from the frontend
func (rw *ResponseWaiter) ProvideResponse(response string) bool {
	// Important: do NOT require "isWaiting" to be true.
	// The frontend may ACK very quickly (e.g., Browser_Navigate / Browser_Alert),
	// and the response can arrive before WaitForResponse() flips the flag.
	// If we drop that early response, the tool will hang until a timeout.
	select {
	case rw.responseChan <- response:
		return true
	default:
		// Channel full (stale response). Drop one and try again.
		select {
		case <-rw.responseChan:
		default:
		}
		select {
		case rw.responseChan <- response:
			return true
		default:
			return false
		}
	}
}

// IsWaiting returns whether the waiter is currently waiting
func (rw *ResponseWaiter) IsWaiting() bool {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.isWaiting
}

// ToolExecutorFunc is a function type for custom tool execution
type ToolExecutorFunc func(
	functionName string,
	functionCallArgs map[string]interface{},
	agent AgentInterface,
	sessionID string,
	writer *WebSocketWriter,
	responseWaiter *ResponseWaiter,
	logger *log.Logger,
) (string, error)

// AgentSession encapsulates WebSocket agent interaction logic
type AgentSession struct {
	Agent                AgentInterface
	SessionID            string
	UserID               string // User ID for associating conversations with users
	Writer               *WebSocketWriter
	Store                stores.MessageStore
	TraceStore           stores.TraceStore // Optional: for persisting execution traces
	Logger               *log.Logger
	History              []stores.Message
	ResponseWaiter       *ResponseWaiter
	FrontendToolExecutor FrontendToolExecutor // Optional: for handling frontend tools
	ToolExecutor         ToolExecutorFunc     // Optional: custom tool executor function
	Memory               MemoryManager        // Optional: for memory storage and retrieval
	FlowLogger           FlowLogger           // Optional: for logging message flow events

	// TTS (optional): when enabled, text deltas are forwarded to ElevenLabs and audio chunks are streamed to the client.
	ttsClient    *eleven_tts.Client
	ttsContextID string
	ttsMu        sync.Mutex
	ttsPending   strings.Builder
	ttsFormat    string
	ttsVoiceID   string

	ttsForwarderStop chan struct{}
	ttsForwarderOnce sync.Once

	// TTS connection lifetime context (must outlive a single interaction).
	// The per-interaction ctx is cancelled on barge-in / after the turn completes; using it for
	// the ElevenLabs socket kills audio streaming mid-flight.
	ttsConnCtx    context.Context
	ttsConnCancel context.CancelFunc
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
	// SetHistoryWarningCallback sets a callback for history warnings if the model supports it
	// Returns true if the model supports warnings, false otherwise
	SetHistoryWarningCallback(callback func(warnings []models.HistoryWarning)) bool
}
