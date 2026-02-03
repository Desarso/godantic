package sessions

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Desarso/godantic/stores"
	"github.com/gorilla/websocket"
)

// NewAgentSession creates a new WebSocket agent session
func NewAgentSession(sessionID string, userID string, conn *websocket.Conn, agent AgentInterface, store stores.MessageStore, memory MemoryManager) *AgentSession {
	logger := log.New(os.Stdout, fmt.Sprintf("[WS %s] ", sessionID), log.LstdFlags)
	writer := &WebSocketWriter{
		Conn:      conn,
		Logger:    logger,
		StartTime: time.Now(),
	}

	ttsConnCtx, ttsConnCancel := context.WithCancel(context.Background())
	return &AgentSession{
		Agent:          agent,
		SessionID:      sessionID,
		UserID:         userID,
		Writer:         writer,
		Store:          store,
		Logger:         logger,
		ResponseWaiter: NewResponseWaiter(),
		Memory:         memory,
		ttsConnCtx:     ttsConnCtx,
		ttsConnCancel:  ttsConnCancel,
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
