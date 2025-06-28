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

// Re-export constructor functions
func NewAgentSession(sessionID string, conn *websocket.Conn, agent *Agent, store stores.MessageStore) *AgentSession {
	return sessions.NewAgentSession(sessionID, conn, agent, store)
}

func NewHTTPSession(conversationID string, agent *Agent, store stores.MessageStore) *HTTPSession {
	return sessions.NewHTTPSession(conversationID, agent, store)
}
