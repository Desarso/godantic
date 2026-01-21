package multi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// IncomingMessage is a parsed server message (either audio chunk or final).
type IncomingMessage struct {
	Kind      string          `json:"kind"` // "audio" | "final" | "unknown"
	ContextID string          `json:"context_id,omitempty"`
	AudioB64  string          `json:"audio_base_64,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}

type Client struct {
	conn   *websocket.Conn
	events chan IncomingMessage
	errors chan error

	sendCh chan any
	once   sync.Once
	mu     sync.Mutex
	closed bool
}

func Dial(ctx context.Context, cfg ConnectConfig, headers http.Header) (*Client, error) {
	if cfg.VoiceID == "" {
		return nil, fmt.Errorf("missing voice_id")
	}
	if headers == nil {
		headers = http.Header{}
	}
	if cfg.APIKey != "" {
		headers.Set("xi-api-key", cfg.APIKey)
	}
	if cfg.Authorization != "" && headers.Get("authorization") == "" {
		headers.Set("authorization", cfg.Authorization)
	}

	u, err := BuildURL(cfg)
	if err != nil {
		return nil, err
	}

	d := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := d.DialContext(ctx, u, headers)
	if err != nil {
		return nil, err
	}

	c := &Client{
		conn:   conn,
		events: make(chan IncomingMessage, 256),
		errors: make(chan error, 16),
		sendCh: make(chan any, 256),
	}
	c.startLoops(ctx)
	return c, nil
}

func (c *Client) Events() <-chan IncomingMessage { return c.events }
func (c *Client) Errors() <-chan error           { return c.errors }

func (c *Client) Close() error {
	var err error
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		close(c.sendCh)
		_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, "closing"), time.Now().Add(250*time.Millisecond))
		err = c.conn.Close()
		close(c.events)
		close(c.errors)
	})
	return err
}

func (c *Client) startLoops(ctx context.Context) {
	// Writer loop
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-c.sendCh:
				if !ok {
					return
				}
				if err := c.conn.WriteJSON(msg); err != nil {
					c.tryEmitErr(err)
					return
				}
			}
		}
	}()

	// Reader loop
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			_, b, err := c.conn.ReadMessage()
			if err != nil {
				c.tryEmitErr(err)
				return
			}

			// ElevenLabs sends either:
			// { "audio": "...", "contextId": "..." , ... }
			// or { "isFinal": true, "contextId": "..." }
			var raw map[string]any
			if err := json.Unmarshal(b, &raw); err != nil {
				c.tryEmitErr(fmt.Errorf("tts: invalid json: %w", err))
				continue
			}

			ctxID := ""
			if v, ok := raw["contextId"].(string); ok {
				ctxID = v
			} else if v, ok := raw["context_id"].(string); ok {
				ctxID = v
			}

			if aud, ok := raw["audio"].(string); ok && aud != "" {
				select {
				case c.events <- IncomingMessage{Kind: "audio", ContextID: ctxID, AudioB64: aud, Raw: json.RawMessage(b)}:
				default:
				}
				continue
			}

			if isFinal, ok := raw["isFinal"].(bool); ok && isFinal {
				select {
				case c.events <- IncomingMessage{Kind: "final", ContextID: ctxID, Raw: json.RawMessage(b)}:
				default:
				}
				continue
			}

			select {
			case c.events <- IncomingMessage{Kind: "unknown", ContextID: ctxID, Raw: json.RawMessage(b)}:
			default:
			}
		}
	}()
}

func (c *Client) tryEmitErr(err error) {
	if err == nil {
		return
	}
	select {
	case c.errors <- err:
	default:
	}
}

// --- Outgoing messages (client -> ElevenLabs) ---

type initializeConnectionMulti struct {
	Text          string `json:"text"`
	ContextID     string `json:"context_id,omitempty"`
	XIAPIKey      string `json:"xi_api_key,omitempty"`
	Authorization string `json:"authorization,omitempty"`
}

type sendTextMulti struct {
	Text      string `json:"text"`
	ContextID string `json:"context_id,omitempty"`
	Flush     bool   `json:"flush,omitempty"`
}

type flushContextClient struct {
	ContextID string `json:"context_id"`
	Text      string `json:"text,omitempty"`
	Flush     bool   `json:"flush"`
}

type closeSocketClient struct {
	CloseSocket bool `json:"close_socket"`
}

func (c *Client) InitializeContext(ctx context.Context, contextID string) error {
	msg := initializeConnectionMulti{
		Text:      " ",
		ContextID: contextID,
	}
	return c.send(ctx, msg)
}

func (c *Client) SendText(ctx context.Context, contextID string, text string, flush bool) error {
	t := strings.ReplaceAll(text, "\r\n", "\n")
	msg := sendTextMulti{
		Text:      t,
		ContextID: contextID,
		Flush:     flush,
	}
	return c.send(ctx, msg)
}

func (c *Client) Flush(ctx context.Context, contextID string, text string) error {
	msg := flushContextClient{
		ContextID: contextID,
		Text:      text,
		Flush:     true,
	}
	return c.send(ctx, msg)
}

func (c *Client) CloseSocket(ctx context.Context) error {
	return c.send(ctx, closeSocketClient{CloseSocket: true})
}

func (c *Client) send(ctx context.Context, v any) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return fmt.Errorf("tts: client closed")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c.sendCh <- v:
		return nil
	}
}
