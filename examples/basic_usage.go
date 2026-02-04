package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	// "github.com/desarso/NCA_Assistant/controllers"
	"github.com/Desarso/godantic"
	"github.com/Desarso/godantic/common_tools"
	models "github.com/Desarso/godantic/models"
	"github.com/Desarso/godantic/models/gemini"
	"github.com/Desarso/godantic/stores"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// ExampleSSEWriter implements SSEWriter for demonstration
type ExampleSSEWriter struct {
	responseCount int
}

func (w *ExampleSSEWriter) WriteSSE(data string) error {
	w.responseCount++
	fmt.Printf("SSE Data %d: %s\n", w.responseCount, data)
	return nil
}

func (w *ExampleSSEWriter) WriteSSEError(err error) error {
	fmt.Printf("SSE Error: %v\n", err)
	return nil
}

func (w *ExampleSSEWriter) Flush() {
	fmt.Println("SSE Flush called")
}

// Example 1: Basic HTTP Chat Setup
func basicHTTPChatExample() {
	fmt.Println("=== Basic HTTP Chat Example ===")

	// Create agent
	tools, err := godantic.Create_Tools([]interface{}{
		common_tools.Search,
	})
	if err != nil {
		log.Fatal(err)
	}

	agent := godantic.Create_Agent(&gemini.Gemini_Model{
		Model: "gemini-2.0-flash",
	}, tools, nil)

	// Create store
	store, err := stores.NewSQLiteStoreSimple("example_chat.sqlite")
	if err != nil {
		log.Fatal(err)
	}

	// Set up routes with basic handlers
	router := gin.Default()
	r := router.Group("/api/v1")

	// Basic chat endpoint
	r.POST("/chat/:conversationID", func(c *gin.Context) {
		conversationID := c.Param("conversationID")

		var req models.Model_Request
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Validate request has either user message or tool results
		if req.User_Message == nil && req.Tool_Results == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "request must contain either user message or tool results"})
			return
		}

		// Create tools and agent from configuration (like WebSocket controllers)
		tools, err := godantic.Create_Tools([]interface{}{
			common_tools.Search,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create tools: " + err.Error()})
			return
		}

		agent := godantic.Create_Agent(&gemini.Gemini_Model{
			Model: "gemini-2.0-flash",
		}, tools, nil)

		// Create agent session like WebSocket (but without WebSocket connection)
		session := godantic.NewHTTPSession(conversationID, &agent, store)

		// Use streaming but collect all results for single response
		respChan, errChan := session.RunStreamInteraction(*req.User_Message)

		var finalResponse models.Model_Response
		var allParts []models.Model_Part

		// Collect all streaming chunks into a single response
		for {
			select {
			case response, ok := <-respChan:
				if !ok {
					// Stream finished
					finalResponse.Parts = allParts
					c.JSON(http.StatusOK, finalResponse)
					return
				}
				allParts = append(allParts, response.Parts...)

			case err, ok := <-errChan:
				if ok && err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				if !ok {
					errChan = nil
				}
			}

			if respChan == nil && errChan == nil {
				finalResponse.Parts = allParts
				c.JSON(http.StatusOK, finalResponse)
				return
			}
		}
	})

	// Streaming chat endpoint
	r.POST("/chat/stream/:conversationID", func(c *gin.Context) {
		conversationID := c.Param("conversationID")

		var req models.Model_Request
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Validate request has either user message or tool results
		if req.User_Message == nil && req.Tool_Results == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "request must contain either user message or tool results"})
			return
		}

		// Set SSE headers
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		// Create session
		session := godantic.NewHTTPSession(conversationID, &agent, store)

		// Create custom SSE writer for gin context
		writer := &GinSSEWriter{Context: c}

		// Run streaming interaction
		ctx := context.Background()
		err := session.RunSSEInteraction(*req.User_Message, writer, ctx)
		if err != nil {
			writer.WriteSSEError(err)
		}
	})

	// Chat history endpoint - this one keeps :conversationID in URL
	r.GET("/chat/history/:conversationID", func(c *gin.Context) {
		conversationID := c.Param("conversationID")

		// Create session
		session := godantic.NewHTTPSession(conversationID, &agent, store)

		// Get history
		history, err := session.GetChatHistory()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"history": history})
	})

	fmt.Println("Server starting on :8000")
	router.Run(":8000")
}

// GinSSEWriter implements SSEWriter for Gin context
type GinSSEWriter struct {
	Context *gin.Context
}

func (w *GinSSEWriter) WriteSSE(data string) error {
	w.Context.SSEvent("message", data)
	w.Context.Writer.Flush()
	return nil
}

func (w *GinSSEWriter) WriteSSEError(err error) error {
	w.Context.SSEvent("error", err.Error())
	w.Context.Writer.Flush()
	return nil
}

func (w *GinSSEWriter) Flush() {
	w.Context.Writer.Flush()
}

// Example 2: Direct Session Usage
func directSessionExample() {
	fmt.Println("=== Direct Session Usage Example ===")

	// Create agent
	tools, err := godantic.Create_Tools([]interface{}{
		common_tools.Search,
	})
	if err != nil {
		log.Fatal(err)
	}

	agent := godantic.Create_Agent(&gemini.Gemini_Model{
		Model: "gemini-2.0-flash",
	}, tools, nil)

	// Create store
	store, err := stores.NewSQLiteStoreSimple("direct_session.sqlite")
	if err != nil {
		log.Fatal(err)
	}

	// Create session
	session := godantic.NewHTTPSession("direct_example", &agent, store)

	// Single interaction
	userMsg := models.User_Message{
		Content: models.Content{
			Parts: []models.User_Part{
				{Text: "Search for the latest news about electric vehicles."},
			},
		},
	}

	response, err := session.RunSingleInteraction(userMsg)
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("Response: %+v\n", response)

	// Get chat history
	history, err := session.GetChatHistory()
	if err != nil {
		log.Printf("Error getting history: %v", err)
		return
	}

	fmt.Printf("History has %d messages\n", len(history))
}

// Example 3: Streaming Chat with Custom SSE Writer
func streamingChatExample() {
	fmt.Println("=== Streaming Chat Example ===")

	// Create session
	tools, _ := godantic.Create_Tools([]interface{}{common_tools.Search})
	agent := godantic.Create_Agent(&gemini.Gemini_Model{Model: "gemini-2.0-flash"}, tools, nil)
	store, _ := stores.NewSQLiteStoreSimple("streaming_example.sqlite")
	session := godantic.NewHTTPSession("streaming_conv", &agent, store)

	// User message
	userMsg := models.User_Message{
		Content: models.Content{
			Parts: []models.User_Part{
				{Text: "Tell me a short story about AI"},
			},
		},
	}

	// Run streaming interaction
	writer := &ExampleSSEWriter{}
	ctx := context.Background()

	err := session.RunSSEInteraction(userMsg, writer, ctx)
	if err != nil {
		log.Printf("Streaming error: %v", err)
	}

	fmt.Printf("Streaming completed. Total responses: %d\n", writer.responseCount)
}

// Example 4: WebSocket Session
func websocketSessionExample() {
	fmt.Println("=== WebSocket Session Example ===")

	// This would typically be in a WebSocket handler
	handleWebSocketConnection := func(conn *websocket.Conn, sessionID string) {
		defer conn.Close()

		// Create agent and store
		tools, _ := godantic.Create_Tools([]interface{}{
			common_tools.Search,
		})
		agent := godantic.Create_Agent(&gemini.Gemini_Model{
			Model: "gemini-2.0-flash",
		}, tools, nil)
		store, _ := stores.NewSQLiteStoreSimple("websocket_example.sqlite")

		// Create agent session (sessionID, userID, conn, agent, store, memory)
		session := godantic.NewAgentSession(sessionID, "", conn, &agent, store, nil)

		// Message loop
		for {
			var req models.Model_Request
			if err := conn.ReadJSON(&req); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
					log.Printf("WebSocket error: %v", err)
				}
				break
			}

			// Run interaction - handles everything
			if err := session.RunInteraction(req); err != nil {
				if agentErr, ok := err.(*godantic.AgentError); ok && agentErr.Fatal {
					log.Printf("Fatal error: %v", err)
					break
				}
				log.Printf("Non-fatal error: %v", err)
			}
		}

		log.Printf("WebSocket session %s ended", sessionID)
	}

	// Example of setting up WebSocket server
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}

		sessionID := r.URL.Query().Get("session_id")
		if sessionID == "" {
			sessionID = "default_session"
		}

		handleWebSocketConnection(conn, sessionID)
	})

	fmt.Println("WebSocket server would start on :8081")
	// http.ListenAndServe(":8081", nil) // Uncomment to actually start
}

// Example 5: Multiple Database Store Examples
func multipleStoreExample() {
	fmt.Println("=== Multiple Store Examples ===")

	// SQLite examples
	fmt.Println("--- SQLite Examples ---")

	// Default SQLite
	store1, err := stores.NewSQLiteStoreSimple("default.sqlite")
	if err != nil {
		log.Printf("SQLite error: %v", err)
	} else {
		fmt.Println("✓ Default SQLite store created")
		store1.Close()
	}

	// SQLite with custom config using StoreConfig
	fmt.Println("--- SQLite with StoreConfig ---")
	sqliteConfig := stores.NewStoreConfig("sqlite", "custom.sqlite")
	store2, err := stores.NewSQLiteStore(sqliteConfig)
	if err != nil {
		log.Printf("Custom SQLite error: %v", err)
	} else {
		fmt.Println("✓ Custom SQLite store created")
		store2.Close()
	}

	// PostgreSQL examples (would require actual database)
	fmt.Println("--- PostgreSQL Examples ---")

	// PostgreSQL connection string
	postgresConnectionString := "host=localhost user=chat_user password=password dbname=chat_db port=5432 sslmode=disable"

	// This would fail without actual PostgreSQL database
	store3, err := stores.NewPostgresStoreSimple(postgresConnectionString)
	if err != nil {
		fmt.Printf("✗ PostgreSQL store failed (expected): %v\n", err)
	} else {
		fmt.Println("✓ PostgreSQL store created")
		store3.Close()
	}

	// Store from configuration
	fmt.Println("--- Store from Configuration ---")

	storeConfig := stores.NewStoreConfig("sqlite", "config_example.sqlite")
	store4, err := stores.NewStore(storeConfig)
	if err != nil {
		log.Printf("Config store error: %v", err)
	} else {
		fmt.Println("✓ Store from configuration created")
		store4.Close()
	}

	// Environment-based configuration
	fmt.Println("--- Environment-based Configuration ---")

	// Set example environment variables
	os.Setenv("DB_TYPE", "sqlite")
	os.Setenv("DB_CONNECTION", "env_example.sqlite")

	envStoreConfig := stores.NewStoreConfig(
		os.Getenv("DB_TYPE"),
		os.Getenv("DB_CONNECTION"),
	)

	store5, err := stores.NewStore(envStoreConfig)
	if err != nil {
		log.Printf("Environment store error: %v", err)
	} else {
		fmt.Println("✓ Environment-based store created")
		store5.Close()
	}
}

// GetCurrentTime is a custom tool function example
func GetCurrentTime(input string) (string, error) {
	type TimeRequest struct {
		Timezone string `json:"timezone"`
		Format   string `json:"format"`
	}

	var req TimeRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Default format
	if req.Format == "" {
		req.Format = "2006-01-02 15:04:05"
	}

	// Simple time response (would normally handle timezone properly)
	result := map[string]interface{}{
		"current_time": "2024-01-01 12:00:00", // Placeholder
		"timezone":     req.Timezone,
		"format":       req.Format,
	}

	resultBytes, _ := json.Marshal(result)
	return string(resultBytes), nil
}

// Example 6: Custom Tool Creation
func customToolExample() {
	fmt.Println("=== Custom Tool Example ===")

	// Note: In real usage, you would generate the schema with:
	// go run cmd/gen_schema/main.go GetCurrentTime

	// Create tools including custom one
	tools, err := godantic.Create_Tools([]interface{}{
		GetCurrentTime,
	})
	if err != nil {
		log.Printf("Error creating tools: %v", err)
		return
	}

	fmt.Printf("Created %d tools including custom GetCurrentTime\n", len(tools))

	// Use in configuration
	config := godantic.NewWSConfig().
		WithModelName("gemini-2.0-flash").
		WithTools([]interface{}{
			GetCurrentTime,
			common_tools.Search,
		})

	fmt.Printf("Configuration created with %d tools\n", len(config.Tools))
}

// Example 7: Error Handling and Debugging
func errorHandlingExample() {
	fmt.Println("=== Error Handling Example ===")

	// Create a session with invalid configuration to demonstrate error handling
	tools, _ := godantic.Create_Tools([]interface{}{})
	agent := godantic.Create_Agent(&gemini.Gemini_Model{
		Model: "invalid-model", // This might cause issues
	}, tools, nil)

	// Try to create store with invalid path
	store, err := stores.NewSQLiteStoreSimple("/invalid/path/database.sqlite")
	if err != nil {
		fmt.Printf("Expected store error: %v\n", err)
		// Fallback to memory database
		store, _ = stores.NewSQLiteStoreSimple(":memory:")
	}

	session := godantic.NewHTTPSession("error_example", &agent, store)

	// Try interaction that might fail
	userMsg := models.User_Message{
		Content: models.Content{
			Parts: []models.User_Part{
				{Text: "This might fail due to invalid model"},
			},
		},
	}

	response, err := session.RunSingleInteraction(userMsg)
	if err != nil {
		fmt.Printf("Interaction error (expected): %v\n", err)

		// Handle different types of errors
		if agentErr, ok := err.(*godantic.AgentError); ok {
			fmt.Printf("Agent error details - Fatal: %v, Message: %s\n",
				agentErr.Fatal, agentErr.Message)
		}
	} else {
		fmt.Printf("Unexpected success: %+v\n", response)
	}

	// Test store health
	if err := store.Ping(); err != nil {
		fmt.Printf("Store health check failed: %v\n", err)
	} else {
		fmt.Println("Store health check passed")
	}

	store.Close()
}

// Main function to run all examples
func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "http":
			basicHTTPChatExample()
		case "session":
			directSessionExample()
		case "stream":
			streamingChatExample()
		case "websocket":
			websocketSessionExample()
		case "stores":
			multipleStoreExample()
		case "tools":
			customToolExample()
		case "errors":
			errorHandlingExample()
		default:
			fmt.Println("Available examples: http, session, stream, websocket, stores, tools, errors")
		}
	} else {
		fmt.Println("=== NCA Assistant Backend Examples ===")
		fmt.Println()
		fmt.Println("Run with argument to see specific examples:")
		fmt.Println("  go run examples/basic_usage.go http      - Basic HTTP server")
		fmt.Println("  go run examples/basic_usage.go session  - Direct session usage")
		fmt.Println("  go run examples/basic_usage.go stream   - Streaming chat")
		fmt.Println("  go run examples/basic_usage.go websocket - WebSocket example")
		fmt.Println("  go run examples/basic_usage.go stores   - Multiple store types")
		fmt.Println("  go run examples/basic_usage.go tools    - Custom tool creation")
		fmt.Println("  go run examples/basic_usage.go errors   - Error handling")
		fmt.Println()

		// Run a quick demo of all examples
		fmt.Println("Running quick demo of all examples...")
		fmt.Println()

		directSessionExample()
		fmt.Println()

		multipleStoreExample()
		fmt.Println()

		customToolExample()
		fmt.Println()

		errorHandlingExample()
		fmt.Println()

		fmt.Println("Demo completed! Run with specific arguments for detailed examples.")
	}
}
