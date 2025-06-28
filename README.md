# Godantic Package

The **godantic** package is the core business logic layer of the NCA Assistant Backend, providing a clean, modular framework for AI chat interactions with multiple database backends and protocol support.

## 🎯 Purpose

Godantic abstracts away the complexity of AI model interactions, database operations, and tool execution, providing a simple interface for building chat applications. It follows clean architecture principles with clear separation of concerns.

## 📦 Package Structure

```
godantic/
├── agent_session.go     # Session management (HTTP & WebSocket)
├── agent.go            # AI agent creation and tool management
├── config.go           # Configuration and builder pattern
├── tool_approver.go    # Tool auto-approval logic
├── models/             # Data models and AI model interfaces
├── stores/             # Database abstraction layer
└── common_tools/       # Built-in tool implementations
```

## 🏗️ Core Components

### 1. Sessions
**Purpose**: Handle different interaction patterns and protocols

#### HTTPSession
For stateless HTTP-based interactions:
```go
session := godantic.NewHTTPSession(conversationID, &agent, store)

// Single request-response
response, err := session.RunSingleInteraction(userMessage)

// Streaming responses
respChan, errChan := session.RunStreamInteraction(userMessage)

// Server-Sent Events with context cancellation
err := session.RunSSEInteraction(userMessage, sseWriter, ctx)

// Get conversation history
history, err := session.GetChatHistory()
```

#### AgentSession (WebSocket)
For stateful WebSocket connections:
```go
session := godantic.NewAgentSession(sessionID, conn, &agent, store)

// Complete interaction loop with tool execution
err := session.RunInteraction(request)
```

### 2. Agents
**Purpose**: Manage AI models and tool execution

```go
// Create tools from function interfaces
tools, err := godantic.Create_Tools([]interface{}{
    common_tools.GetWeather,
    common_tools.Search,
    MyCustomTool,
})

// Create agent with model and tools
agent := godantic.Create_Agent(&gemini.Gemini_Model{
    Model: "gemini-2.0-flash",
}, tools)
```

### 3. Configuration System
**Purpose**: Fluent configuration API with dependency injection

```go
config := godantic.NewWSConfig().
    WithModelName("gemini-2.0-flash").
    WithSQLiteStore("chat.sqlite").
    WithTools([]interface{}{
        common_tools.GetWeather,
        common_tools.Search,
    })
```

### 4. Store Abstraction
**Purpose**: Database-agnostic persistence layer

```go
// SQLite (default)
config.WithSQLiteStore("chat.sqlite")

// PostgreSQL
config.WithPostgresStore("localhost", "user", "pass", "db", 5432)

// Custom store
config.WithStore(myCustomStore)
```

## 🚀 Quick Start Examples

### Basic HTTP Chat
```go
package main

import (
    "github.com/desarso/NCA_Assistant/godantic"
    "github.com/desarso/NCA_Assistant/godantic/common_tools"
    "github.com/desarso/NCA_Assistant/godantic/models/gemini"
)

func main() {
    // 1. Create tools
    tools, _ := godantic.Create_Tools([]interface{}{
        common_tools.GetWeather,
    })
    
    // 2. Create agent
    agent := godantic.Create_Agent(&gemini.Gemini_Model{
        Model: "gemini-2.0-flash",
    }, tools)
    
    // 3. Create store (SQLite)
    store, _ := stores.NewSQLiteStoreSimple("chat.sqlite")
    
    // 4. Create session
    session := godantic.NewHTTPSession("conv_123", &agent, store)
    
    // 5. Run interaction
    userMsg := models.User_Message{
        Content: models.User_Content{
            Parts: []models.User_Part{{Text: "Hello!"}},
        },
    }
    
    response, err := session.RunSingleInteraction(userMsg)
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("AI Response: %+v\n", response)
}
```

### Configuration-Based Setup
```go
// Create complete configuration
config := godantic.NewWSConfig().
    WithModelName("gemini-2.0-flash").
    WithSQLiteStore("my_chat.sqlite").
    WithTools([]interface{}{
        common_tools.GetWeather,
        common_tools.Search,
        common_tools.Brave_Search,
    })

// Use in controllers (HTTP)
chatController := controllers.NewChatControllers(config)

// Use in controllers (WebSocket)  
wsController := controllers.NewWSControllers(config)
```

## 🔧 Configuration API

### WSConfig Builder Methods

#### Database Configuration
```go
// SQLite with default path
config.WithSQLiteStore("database.sqlite")

// PostgreSQL
config.WithPostgresStore(host, user, password, database, port)

// Custom store implementation
config.WithStore(customStore)
```

#### Model Configuration
```go
// Set AI model
config.WithModelName("gemini-2.0-flash")
config.WithModelName("gpt-4")
config.WithModelName("claude-3")
```

#### Tool Configuration
```go
// Add tools
config.WithTools([]interface{}{
    common_tools.GetWeather,
    common_tools.Search,
    MyCustomTool,
})
```

#### Advanced Configuration
```go
// Chain multiple configurations
config := godantic.NewWSConfig().
    WithModelName("gemini-2.0-flash").
    WithPostgresStore("localhost", "user", "pass", "chatdb", 5432).
    WithTools([]interface{}{
        common_tools.GetWeather,
        common_tools.Search,
        common_tools.Brave_Search,
    })
```

## 🛠️ Tool Integration

### Using Built-in Tools
```go
import "github.com/desarso/NCA_Assistant/godantic/common_tools"

tools := []interface{}{
    common_tools.GetWeather,     // Weather information
    common_tools.Search,         // General web search
    common_tools.Brave_Search,   // Brave search engine
}
```

### Creating Custom Tools
```go
// 1. Define your tool function
func GetCurrentTime(input string) (string, error) {
    type TimeRequest struct {
        Timezone string `json:"timezone"`
    }
    
    var req TimeRequest
    json.Unmarshal([]byte(input), &req)
    
    result := map[string]interface{}{
        "current_time": time.Now().Format(time.RFC3339),
        "timezone": req.Timezone,
    }
    
    resultBytes, _ := json.Marshal(result)
    return string(resultBytes), nil
}

// 2. Generate schema (run once)
// go run cmd/gen_schema/main.go GetCurrentTime

// 3. Use in configuration
config.WithTools([]interface{}{
    GetCurrentTime,
    common_tools.GetWeather,
})
```

### Tool Auto-Approval
Configure which tools execute automatically:

```go
// In godantic/tool_approver.go
func Tool_Approver(toolName string, args map[string]interface{}) (bool, error) {
    switch toolName {
    case "GetWeather":
        return true, nil  // Always auto-approve
    case "Search":
        // Conditional approval
        if query, ok := args["query"].(string); ok {
            return len(query) < 100, nil  // Approve short queries
        }
        return false, nil
    default:
        return false, nil  // Require manual approval
    }
}
```

## 🗄️ Database Stores

### SQLite Store (Default)
```go
// Simple SQLite
store, err := stores.NewSQLiteStoreSimple("chat.sqlite")

// Advanced SQLite configuration
config := &stores.SQLiteConfig{
    FilePath: "chat.sqlite",
    Options: map[string]string{
        "cache": "shared",
        "mode": "rwc",
        "_busy_timeout": "5000",
    },
}
store, err := stores.NewSQLiteStore(config)
```

### PostgreSQL Store
```go
// Basic PostgreSQL
config := &stores.PostgresConfig{
    Host:     "localhost",
    Port:     5432,
    User:     "chat_user",
    Password: "password",
    Database: "chat_db",
    SSLMode:  "disable",
}
store, err := stores.NewPostgresStore(config)

// Production PostgreSQL with connection pooling
config := &stores.PostgresConfig{
    Host:         "db.example.com",
    Port:         5432,
    User:         "chat_user",
    Password:     "secure_password",
    Database:     "chat_production",
    SSLMode:      "require",
    MaxOpenConns: 100,
    MaxIdleConns: 10,
    MaxLifetime:  "30m",
}
store, err := stores.NewPostgresStore(config)
```

### Custom Store Implementation
Implement the `MessageStore` interface:

```go
type MyCustomStore struct {
    // Your implementation
}

func (s *MyCustomStore) SaveMessage(sessionID, role, messageType string, parts interface{}, functionID string) error {
    // Your save logic
    return nil
}

func (s *MyCustomStore) FetchHistory(sessionID string) ([]stores.Message, error) {
    // Your fetch logic
    return nil, nil
}

func (s *MyCustomStore) CreateConversation(convoID, userID string) error {
    // Your conversation creation logic
    return nil
}

func (s *MyCustomStore) ListConversations() ([]string, error) {
    // Your listing logic
    return nil, nil
}

func (s *MyCustomStore) Connect() error { return nil }
func (s *MyCustomStore) Close() error { return nil }
func (s *MyCustomStore) Ping() error { return nil }
```

## 🌊 Streaming and Real-time

### HTTP Streaming (SSE)
```go
// Custom SSE Writer
type MySSEWriter struct {
    writer http.ResponseWriter
}

func (w *MySSEWriter) WriteSSE(data string) error {
    _, err := fmt.Fprintf(w.writer, "data: %s\n\n", data)
    return err
}

func (w *MySSEWriter) WriteSSEError(err error) error {
    errorData, _ := json.Marshal(map[string]string{"error": err.Error()})
    _, writeErr := fmt.Fprintf(w.writer, "event: error\ndata: %s\n\n", errorData)
    return writeErr
}

func (w *MySSEWriter) Flush() {
    if f, ok := w.writer.(http.Flusher); ok {
        f.Flush()
    }
}

// Use with session
err := session.RunSSEInteraction(userMessage, &MySSEWriter{writer: w}, ctx)
```

### WebSocket Real-time
```go
// Complete WebSocket session management
session := godantic.NewAgentSession(sessionID, conn, &agent, store)

for {
    var request models.Model_Request
    if err := conn.ReadJSON(&request); err != nil {
        break
    }
    
    // Handles: parsing, AI interaction, tool execution, responses
    if err := session.RunInteraction(request); err != nil {
        if agentErr, ok := err.(*godantic.AgentError); ok && agentErr.Fatal {
            break // Fatal error, close connection
        }
        // Non-fatal errors are handled internally
    }
}
```

## 🔍 Error Handling

### AgentError Types
```go
type AgentError struct {
    Message string
    Fatal   bool
}

// Check error types
if err := session.RunInteraction(req); err != nil {
    if agentErr, ok := err.(*godantic.AgentError); ok {
        if agentErr.Fatal {
            // Fatal error - close connection/session
            log.Printf("Fatal error: %s", agentErr.Message)
            break
        } else {
            // Non-fatal error - continue session
            log.Printf("Non-fatal error: %s", agentErr.Message)
        }
    }
}
```

### Store Health Checks
```go
// Check store connectivity
if err := store.Ping(); err != nil {
    log.Printf("Database connection failed: %v", err)
}

// Graceful cleanup
defer store.Close()
```

## 📊 Advanced Usage

### Environment-Based Configuration
```go
func createConfigFromEnv() *godantic.WSConfig {
    dbType := os.Getenv("DB_TYPE")
    dbConnection := os.Getenv("DB_CONNECTION")
    modelName := os.Getenv("AI_MODEL")
    
    storeConfig := stores.NewStoreConfig(dbType, dbConnection)
    store, err := stores.NewStore(storeConfig)
    if err != nil {
        panic(err)
    }
    
    return godantic.NewWSConfig().
        WithStore(store).
        WithModelName(modelName).
        WithTools([]interface{}{
            common_tools.GetWeather,
            common_tools.Search,
        })
}
```

### Multi-tenant Configuration
```go
func createTenantConfig(tenantID string) *godantic.WSConfig {
    dbPath := fmt.Sprintf("tenant_%s.sqlite", tenantID)
    
    return godantic.NewWSConfig().
        WithSQLiteStore(dbPath).
        WithModelName("gemini-2.0-flash").
        WithTools(getTenantTools(tenantID))
}
```

### Batch Processing
```go
func processBatch(messages []models.User_Message, session *godantic.HTTPSession) []models.Model_Response {
    responses := make([]models.Model_Response, len(messages))
    
    for i, msg := range messages {
        response, err := session.RunSingleInteraction(msg)
        if err != nil {
            log.Printf("Error processing message %d: %v", i, err)
            continue
        }
        responses[i] = response
    }
    
    return responses
}
```

## 🧪 Testing

### Unit Testing Sessions
```go
func TestHTTPSession(t *testing.T) {
    // Create mock dependencies
    mockStore := &MockMessageStore{}
    mockModel := &MockModel{}
    agent := godantic.Create_Agent(mockModel, []models.FunctionDeclaration{})
    
    // Create session
    session := godantic.NewHTTPSession("test_conv", &agent, mockStore)
    
    // Test interaction
    userMsg := models.User_Message{
        Content: models.User_Content{
            Parts: []models.User_Part{{Text: "Test message"}},
        },
    }
    
    response, err := session.RunSingleInteraction(userMsg)
    
    assert.NoError(t, err)
    assert.NotEmpty(t, response.Parts)
}
```

### Integration Testing
```go
func TestFullIntegration(t *testing.T) {
    // Use in-memory database for testing
    config := godantic.NewWSConfig().
        WithSQLiteStore(":memory:").
        WithModelName("test-model").
        WithTools([]interface{}{testTool})
    
    // Test with real session
    session := createSessionFromConfig(config, "test_conv")
    
    // Run actual interaction
    response, err := session.RunSingleInteraction(testMessage)
    
    assert.NoError(t, err)
    assert.Contains(t, response.Parts[0].Text, "expected content")
}
```

## 🚀 Performance Tips

### Connection Pooling
```go
// PostgreSQL with optimized pooling
config := &stores.PostgresConfig{
    Host:         "localhost",
    Port:         5432,
    User:         "chat_user",
    Password:     "password", 
    Database:     "chat_db",
    MaxOpenConns: 100,      // Adjust based on load
    MaxIdleConns: 10,       // Keep some connections ready
    MaxLifetime:  "30m",    // Recycle connections
}
```

### Memory Management
```go
// Sessions are automatically cleaned up when they go out of scope
// For long-running sessions, ensure proper cleanup:

defer func() {
    if store != nil {
        store.Close()
    }
}()
```

### Streaming Optimization
```go
// Use context cancellation for better resource management
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

err := session.RunSSEInteraction(userMessage, writer, ctx)
```

## 🔗 Integration with Controllers

The godantic package is designed to be used by thin controller layers:

```go
// HTTP Controller integration
func (c *ChatControllers) POST_Chat(ctx *gin.Context) {
    // Parse request (controller responsibility)
    var userMessage models.User_Message
    ctx.ShouldBindJSON(&userMessage)
    
    // Create session (using configuration)
    session := godantic.NewHTTPSession(
        ctx.Param("conversationID"), 
        &c.agent, 
        c.store,
    )
    
    // Delegate business logic to godantic
    response, err := session.RunSingleInteraction(userMessage)
    
    // Handle response (controller responsibility)
    if err != nil {
        ctx.JSON(500, gin.H{"error": err.Error()})
        return
    }
    ctx.JSON(200, response)
}
```

## 📚 API Reference

### Core Functions
- `NewWSConfig()` - Create new configuration builder
- `Create_Tools([]interface{})` - Create tool definitions from functions
- `Create_Agent(model, tools)` - Create AI agent with model and tools
- `NewHTTPSession(id, agent, store)` - Create HTTP session
- `NewAgentSession(id, conn, agent, store)` - Create WebSocket session

### Configuration Methods
- `WithModelName(string)` - Set AI model
- `WithSQLiteStore(path)` - Use SQLite database
- `WithPostgresStore(host, user, pass, db, port)` - Use PostgreSQL
- `WithStore(store)` - Use custom store
- `WithTools([]interface{})` - Set available tools

### Session Methods
- `RunSingleInteraction(msg)` - Single request-response
- `RunStreamInteraction(msg)` - Streaming responses
- `RunSSEInteraction(msg, writer, ctx)` - Server-Sent Events
- `GetChatHistory()` - Retrieve conversation history
- `RunInteraction(req)` - WebSocket interaction loop

This package provides the foundation for building scalable, maintainable AI chat applications with clean separation of concerns and extensive customization options. 