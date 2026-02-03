# Godantic Stores

The godantic stores package provides a flexible interface for storing chat messages and conversation history with support for multiple database backends.

## Supported Databases

- **SQLite** - File-based database, perfect for development and single-instance deployments
- **PostgreSQL** - Full-featured SQL database for production use
- **Extensible** - Easy to add support for MySQL, MongoDB, Redis, etc.

## Quick Start

### Using Default SQLite Store

```go
import "github.com/desarso/NCA_Assistant/godantic"

// Create config with default SQLite store (chat_history.sqlite)
config := godantic.NewWSConfig()
```

### Using Custom SQLite Store

```go
// Option 1: Using the convenience method
config := godantic.NewWSConfig().
    WithSQLiteStore("my_custom_database.sqlite")

// Option 2: Creating store manually
store, err := stores.NewSQLiteStoreSimple("my_database.sqlite")
if err != nil {
    log.Fatal("Failed to create store:", err)
}
config := godantic.NewWSConfig().WithStore(store)
```

### Using PostgreSQL Store

```go
// Option 1: Using the convenience method
config := godantic.NewWSConfig().
    WithPostgresStore("localhost", "username", "password", "dbname", 5432)

// Option 2: Using connection string
dsn := "host=localhost user=username password=password dbname=dbname port=5432 sslmode=disable"
store, err := stores.NewPostgresStoreSimple(dsn)
if err != nil {
    log.Fatal("Failed to create PostgreSQL store:", err)
}
config := godantic.NewWSConfig().WithStore(store)
```

### Using Store Configuration

```go
import "github.com/desarso/NCA_Assistant/godantic/stores"

// SQLite configuration
sqliteConfig := stores.NewStoreConfig("sqlite", "chat_history.sqlite")
store, err := stores.NewStore(sqliteConfig)

// PostgreSQL configuration
pgConfig := stores.NewStoreConfig("postgres", "host=localhost user=username password=password dbname=dbname port=5432 sslmode=disable")
store, err := stores.NewStore(pgConfig)

// Use the store
config := godantic.NewWSConfig().WithStore(store)
```

## Environment-Based Configuration

You can easily switch between databases based on environment variables:

```go
func createStoreFromEnv() stores.MessageStore {
    dbType := os.Getenv("DB_TYPE") // "sqlite" or "postgres"
    dbConnection := os.Getenv("DB_CONNECTION")
    
    if dbType == "" {
        dbType = "sqlite"
        dbConnection = "chat_history.sqlite"
    }
    
    config := stores.NewStoreConfig(dbType, dbConnection)
    store, err := stores.NewStore(config)
    if err != nil {
        log.Fatal("Failed to create store:", err)
    }
    return store
}

// Usage
config := godantic.NewWSConfig().WithStore(createStoreFromEnv())
```

## Example Environment Variables

```bash
# For SQLite
export DB_TYPE=sqlite
export DB_CONNECTION=chat_history.sqlite

# For PostgreSQL
export DB_TYPE=postgres
export DB_CONNECTION="host=localhost user=username password=password dbname=chatdb port=5432 sslmode=disable"
```

## Complete WebSocket Controller Setup

```go
package routes

import (
    "github.com/desarso/NCA_Assistant/controllers"
    "github.com/desarso/NCA_Assistant/godantic"
    "github.com/desarso/NCA_Assistant/godantic/common_tools"
    "github.com/gin-gonic/gin"
)

func createWSController() *controllers.WS_controllers {
    config := godantic.NewWSConfig().
        WithModelName("gemini-2.0-flash").
        WithTools([]interface{}{
            common_tools.Search,
            common_tools.Brave_Search,
        }).
        WithSQLiteStore("chat_history.sqlite") // or WithPostgresStore(...)
    
    return controllers.NewWSControllers(config)
}
```

## Database Schema

All stores automatically create the required tables:

### Conversations Table
- `id` - Primary key
- `created_at` - Creation timestamp
- `updated_at` - Last update timestamp
- `conversation_id` - Unique conversation identifier
- `user_id` - User who owns the conversation
- `message_count` - Number of messages in conversation

### Messages Table
- `id` - Primary key
- `created_at` - Creation timestamp
- `updated_at` - Last update timestamp
- `conversation_id` - Foreign key to conversation
- `sequence` - Message order within conversation
- `role` - "user" or "model"
- `type` - "user_message", "model_message", "function_call", "function_response"
- `function_id` - Optional function call identifier
- `parts_json` - JSON-encoded message parts

## Adding New Database Support

To add support for a new database (e.g., MySQL):

1. Create `mysql_store.go` in the stores package
2. Implement the `MessageStore` interface
3. Add the case to the factory function in `factory.go`
4. Add convenience methods to `config.go`

Example structure:
```go
type MySQLStore struct {
    db *gorm.DB
}

func NewMySQLStore(config *StoreConfig) (*MySQLStore, error) {
    // Implementation
}

func (s *MySQLStore) SaveMessage(sessionID, role, messageType string, parts interface{}, functionID string) error {
    // Implementation
}

// ... implement other MessageStore methods
```

## Best Practices

1. **Development**: Use SQLite for simplicity
2. **Production**: Use PostgreSQL for scalability and features
3. **Testing**: Use in-memory SQLite (`:memory:`)
4. **Environment Variables**: Configure database connection via environment
5. **Connection Pooling**: GORM handles this automatically
6. **Migrations**: Stores auto-migrate schemas on connect

## Error Handling

All store operations return errors that should be handled appropriately:

```go
store, err := stores.NewSQLiteStoreSimple("chat.sqlite")
if err != nil {
    log.Fatal("Failed to create store:", err)
}

if err := store.Ping(); err != nil {
    log.Error("Database connection failed:", err)
}
``` 
