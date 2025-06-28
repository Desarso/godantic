# Architecture Documentation

## Overview

The backend has been refactored to follow a clean, modular architecture with clear separation of concerns. This document explains the architectural decisions and patterns used.

## Architecture Layers

### 1. Controllers Layer (Thin)
**Location**: `controllers/`
**Responsibility**: HTTP/WebSocket protocol handling

#### HTTP Controllers (`controllers/chat_controllers.go`)
- **Size**: ~105 lines (down from 405+ lines)
- **Responsibilities**:
  - Request parsing and validation
  - Response formatting
  - Protocol-specific concerns (SSE headers, JSON responses)
  - Error handling at HTTP level

**Example Controller Structure:**
```go
func (u *Chat_controllers) POST_Chat(c *gin.Context) {
    // 1. Parse and validate request
    conversationID := c.Param("conversationID")
    var userMessage models.User_Message
    if err := c.ShouldBindJSON(&userMessage); err != nil {
        c.JSON(400, HTTPError{Message: err.Error()})
        return
    }

    // 2. Create session (delegating configuration)
    session := godantic.NewHTTPSession(conversationID, &agent, store)

    // 3. Delegate business logic
    response, err := session.RunSingleInteraction(userMessage)

    // 4. Handle response
    if err != nil {
        c.JSON(500, HTTPError{Message: err.Error()})
        return
    }
    c.JSON(200, response)
}
```

#### WebSocket Controllers (`controllers/ws_controllers.go`)
- **Size**: ~85 lines
- **Responsibilities**:
  - WebSocket upgrade and connection management
  - Message loop handling
  - Connection cleanup

**Example WebSocket Structure:**
```go
func (u *WS_controllers) WS_Chat(c *gin.Context) {
    // 1. Upgrade connection
    conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
    
    // 2. Create session
    session := godantic.NewAgentSession(sessionID, conn, &agent, store)
    
    // 3. Message loop with delegation
    for {
        var req models.Model_Request
        conn.ReadJSON(&req)
        session.RunInteraction(req) // All logic delegated
    }
}
```

### 2. Godantic Package (Business Logic)
**Location**: `godantic/`
**Responsibility**: All business logic and AI interaction

#### Session Management
Two main session types handle different interaction patterns:

##### HTTPSession (`godantic/agent_session.go`)
**Methods:**
- `RunSingleInteraction()` - Complete request-response cycle
- `RunStreamInteraction()` - Streaming with accumulation  
- `RunSSEInteraction()` - Server-Sent Events with context cancellation
- `GetChatHistory()` - Database fetching and API conversion
- `saveUserMessage()` - User message processing
- `processAndSaveResponse()` - Model response with auto-approved tools

**Usage Pattern:**
```go
session := godantic.NewHTTPSession(conversationID, &agent, store)
response, err := session.RunSingleInteraction(userMessage)
```

##### AgentSession (`godantic/agent_session.go`)
**Methods:**
- `RunInteraction()` - Complete WebSocket interaction loop
- `processStream()` - Stream processing and accumulation
- `processAccumulatedParts()` - Function call extraction and execution
- `fetchHistory()` - Database history retrieval
- `saveIncomingMessage()` - Message persistence

**Usage Pattern:**
```go
session := godantic.NewAgentSession(sessionID, conn, &agent, store)
err := session.RunInteraction(request)
```

#### Agent Management
- **Tool Creation**: `Create_Tools()` from function interfaces
- **Agent Creation**: `Create_Agent()` with model and tools
- **Tool Execution**: Automatic approval and execution based on rules
- **Model Interaction**: Streaming and single-shot inference

#### Configuration System
**Fluent Builder Pattern:**
```go
config := godantic.NewWSConfig().
    WithModelName("gemini-2.0-flash").
    WithSQLiteStore("chat.sqlite").
    WithTools([]interface{}{
        common_tools.GetWeather,
        common_tools.Search,
    })
```

### 3. Stores Layer (Database)
**Location**: `godantic/stores/`
**Responsibility**: Data persistence abstraction

#### Interface Design
```go
type MessageStore interface {
    SaveMessage(sessionID, role, messageType string, parts interface{}, functionID string) error
    FetchHistory(sessionID string) ([]Message, error)
    CreateConversation(convoID, userID string) error
    ListConversations() ([]string, error)
    Connect() error
    Close() error
    Ping() error
}
```

#### Implementations
- **SQLiteStore**: File-based storage for development/single-instance
- **PostgresStore**: Production database with connection pooling
- **Extensible**: Easy to add Redis, MongoDB, etc.

## Data Flow

### HTTP Request Flow
```
HTTP Request → Controller → HTTPSession → Agent → Store
     ↓              ↓           ↓         ↓       ↓
Parse Request → Validate → Run AI → Execute → Save
     ↓              ↓           ↓         ↓       ↓
HTTP Response ← Format ← Response ← Tools ← Fetch
```

### WebSocket Flow
```
WS Message → Controller → AgentSession → Agent → Store
     ↓           ↓            ↓           ↓       ↓
Parse → Upgrade → Loop → Stream AI → Tools → Save
     ↓           ↓            ↓           ↓       ↓
WS Response ← Send ← Accumulate ← Process ← Fetch
```

### Streaming Flow
```
HTTP Stream → Controller → HTTPSession → Agent → Store
     ↓            ↓            ↓           ↓       ↓
SSE Setup → Headers → RunSSEInteraction → AI → Save
     ↓            ↓            ↓           ↓       ↓
SSE Stream ← Writer ← Channel ← Stream ← Fetch
```

## Key Architectural Patterns

### 1. Separation of Concerns
- **Controllers**: Protocol-specific logic only
- **Sessions**: Business logic and orchestration
- **Stores**: Data persistence abstraction
- **Agents**: AI model interaction

### 2. Dependency Injection
Configuration objects provide dependencies:
```go
type WSConfig struct {
    ModelName string
    Tools     []interface{}
    Store     stores.MessageStore
}
```

### 3. Interface-Based Design
Database abstraction allows multiple backends:
```go
// Easy to swap implementations
store1 := NewSQLiteStore(config)
store2 := NewPostgresStore(config)
config.WithStore(store1) // or store2
```

### 4. Builder Pattern
Fluent configuration API:
```go
config := NewWSConfig().
    WithModelName("model").
    WithTools(tools).
    WithSQLiteStore("db.sqlite")
```

### 5. Strategy Pattern
Different session types for different interaction patterns:
- HTTPSession for request-response
- AgentSession for WebSocket
- Different SSEWriter implementations

## Benefits Achieved

### 1. Maintainability
- **Clear Boundaries**: Each layer has well-defined responsibilities
- **Single Responsibility**: Controllers only handle I/O, sessions handle logic
- **Easy Testing**: Business logic can be unit tested independently

### 2. Extensibility
- **New Databases**: Implement MessageStore interface
- **New Protocols**: Add new controller types
- **New Models**: Implement model interface

### 3. Consistency
- **Uniform Patterns**: All controllers follow same thin pattern
- **Standardized Configuration**: Same config system for all components
- **Error Handling**: Consistent error patterns across layers

### 4. Performance
- **Database Pooling**: Connection pooling for PostgreSQL
- **Stream Processing**: Efficient streaming for large responses
- **Memory Management**: Sessions cleanup automatically

### 5. Testability
- **Mockable Interfaces**: All dependencies are interfaces
- **Isolated Units**: Each layer can be tested independently
- **Configuration Testing**: Easy to test with different configurations

## Migration Benefits

### Before Refactor
```go
// 405 lines of mixed concerns
func POST_Chat(c *gin.Context) {
    // HTTP parsing
    // Database operations
    // AI model calls
    // Tool execution
    // Response formatting
    // Error handling
    // Stream management
}
```

### After Refactor
```go
// 25 lines focused on HTTP concerns
func POST_Chat(c *gin.Context) {
    // Parse request
    session := NewHTTPSession(id, agent, store)
    response := session.RunSingleInteraction(msg)
    // Format response
}
```

## Future Extensibility

### Adding New Database Backend
```go
type RedisStore struct{}
func (r *RedisStore) SaveMessage(...) error { /* Redis logic */ }
func (r *RedisStore) FetchHistory(...) ([]Message, error) { /* Redis logic */ }
// Implement other MessageStore methods
```

### Adding New Protocol
```go
type GRPCControllers struct {
    Config *godantic.WSConfig
}
func (g *GRPCControllers) Chat(req *pb.ChatRequest) (*pb.ChatResponse, error) {
    session := godantic.NewHTTPSession(req.ConversationID, &agent, store)
    return session.RunSingleInteraction(convertRequest(req))
}
```

### Adding New Session Type
```go
type BatchSession struct {
    // For batch processing
}
func (s *BatchSession) RunBatchInteraction(requests []Request) ([]Response, error) {
    // Batch processing logic
}
```

This architecture provides a solid foundation for scaling and extending the system while maintaining clean separation of concerns and testability. 