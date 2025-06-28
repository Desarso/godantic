package godantic

import (
	"github.com/Desarso/godantic/stores"
)

// WSConfig holds configuration for WebSocket controllers
type WSConfig struct {
	ModelName string
	Tools     []interface{}
	Store     stores.MessageStore
}

// NewWSConfig creates a new WebSocket configuration with default values
func NewWSConfig() *WSConfig {
	// Create a default SQLite store
	defaultStore, err := stores.NewSQLiteStoreDefault()
	if err != nil {
		// If we can't create the default store, panic or use a nil store
		// In production, you might want to handle this more gracefully
		panic("Failed to create default SQLite store: " + err.Error())
	}

	return &WSConfig{
		ModelName: "gemini-2.0-flash",
		Tools:     []interface{}{},
		Store:     defaultStore,
	}
}

// WithModelName sets the model name for the configuration
func (c *WSConfig) WithModelName(modelName string) *WSConfig {
	c.ModelName = modelName
	return c
}

// WithTools sets the tools for the configuration
func (c *WSConfig) WithTools(tools []interface{}) *WSConfig {
	c.Tools = tools
	return c
}

// WithStore sets the message store for the configuration
func (c *WSConfig) WithStore(store stores.MessageStore) *WSConfig {
	c.Store = store
	return c
}

// WithSQLiteStore sets a SQLite store with the specified database path
func (c *WSConfig) WithSQLiteStore(dbPath string) *WSConfig {
	store, err := stores.NewSQLiteStoreSimple(dbPath)
	if err != nil {
		panic("Failed to create SQLite store: " + err.Error())
	}
	c.Store = store
	return c
}

// WithPostgresStore sets a PostgreSQL store with the specified connection parameters
func (c *WSConfig) WithPostgresStore(host, user, password, dbname string, port int) *WSConfig {
	store, err := stores.NewPostgresStoreDefault(host, user, password, dbname, port)
	if err != nil {
		panic("Failed to create PostgreSQL store: " + err.Error())
	}
	c.Store = store
	return c
}
