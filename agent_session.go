package godantic

import (
	"github.com/Desarso/godantic/sessions"
	"github.com/Desarso/godantic/stores"
	"github.com/gorilla/websocket"
)

// Re-export session types for backward compatibility
type AgentSession = sessions.AgentSession
type HTTPSession = sessions.HTTPSession
type WebSocketWriter = sessions.WebSocketWriter
type WebSocketToolResultMessage = sessions.WebSocketToolResultMessage
type AgentError = sessions.AgentError
type SSEWriter = sessions.SSEWriter
type ResponseWaiter = sessions.ResponseWaiter
type AgentInterface = sessions.AgentInterface
type ToolExecutorFunc = sessions.ToolExecutorFunc
type MemoryManager = sessions.MemoryManager

// Re-export constructor functions
func NewAgentSession(sessionID string, userID string, conn *websocket.Conn, agent *Agent, store stores.MessageStore, memory MemoryManager) *AgentSession {
	return sessions.NewAgentSession(sessionID, userID, conn, agent, store, memory)
}

func NewHTTPSession(conversationID string, agent *Agent, store stores.MessageStore) *HTTPSession {
	return sessions.NewHTTPSession(conversationID, agent, store)
}
