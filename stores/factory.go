package stores

import (
	"fmt"
)

// NewStore creates a new message store based on the configuration
func NewStore(config *StoreConfig) (MessageStore, error) {
	switch config.Type {
	case "sqlite":
		return NewSQLiteStore(config)
	case "postgres":
		return NewPostgresStore(config)
	default:
		return nil, fmt.Errorf("unsupported store type: %s", config.Type)
	}
}

// NewSQLiteStoreDefault creates a SQLite store with default settings
func NewSQLiteStoreDefault() (MessageStore, error) {
	return NewSQLiteStoreSimple("chat_history.sqlite")
}

// NewPostgresStoreDefault creates a PostgreSQL store with environment-based configuration
// You would typically get these from environment variables
func NewPostgresStoreDefault(host, user, password, dbname string, port int) (MessageStore, error) {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable",
		host, user, password, dbname, port)
	return NewPostgresStoreSimple(dsn)
}
