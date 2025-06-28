package sessions

import (
	"fmt"
	"log"
	"os"

	"github.com/Desarso/godantic/stores"
	"github.com/gorilla/websocket"
)

// NewAgentSession creates a new WebSocket agent session
func NewAgentSession(sessionID string, conn *websocket.Conn, agent AgentInterface, store stores.MessageStore) *AgentSession {
	logger := log.New(os.Stdout, fmt.Sprintf("[WS %s] ", sessionID), log.LstdFlags)
	writer := &WebSocketWriter{
		Conn:   conn,
		Logger: logger,
	}

	return &AgentSession{
		Agent:     agent,
		SessionID: sessionID,
		Writer:    writer,
		Store:     store,
		Logger:    logger,
	}
}

// NewHTTPSession creates a new HTTP session
func NewHTTPSession(conversationID string, agent AgentInterface, store stores.MessageStore) *HTTPSession {
	logger := log.New(os.Stdout, fmt.Sprintf("[HTTP %s] ", conversationID), log.LstdFlags)

	return &HTTPSession{
		Agent:          agent,
		ConversationID: conversationID,
		Store:          store,
		Logger:         logger,
	}
}
