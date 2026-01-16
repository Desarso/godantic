package godantic

import (
	"github.com/Desarso/godantic/stores"
)

// ModelProvider represents the AI model provider to use
type ModelProvider string

const (
	ProviderGemini     ModelProvider = "gemini"
	ProviderOpenRouter ModelProvider = "openrouter"
)

// WSConfig holds configuration for WebSocket controllers
type WSConfig struct {
	ModelName    string
	Tools        []interface{}
	Store        stores.MessageStore
	Provider     ModelProvider // AI model provider (gemini, openrouter)
	SiteURL      string        // Optional: Site URL for OpenRouter rankings
	SiteName     string        // Optional: Site name for OpenRouter rankings
	Temperature  *float64      // Optional: Temperature for model generation
	MaxTokens    *int          // Optional: Max tokens for model generation
	SystemPrompt string        // Optional: System prompt for the AI
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
		Provider:  ProviderGemini,
	}
}

// NewOpenRouterConfig creates a new configuration with OpenRouter as the provider
func NewOpenRouterConfig(model string) *WSConfig {
	// Create a default SQLite store
	defaultStore, err := stores.NewSQLiteStoreDefault()
	if err != nil {
		panic("Failed to create default SQLite store: " + err.Error())
	}

	if model == "" {
		model = "openai/gpt-4o-mini"
	}

	return &WSConfig{
		ModelName: model,
		Tools:     []interface{}{},
		Store:     defaultStore,
		Provider:  ProviderOpenRouter,
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

// WithSystemPrompt sets the system prompt for the AI
func (c *WSConfig) WithSystemPrompt(systemPrompt string) *WSConfig {
	c.SystemPrompt = systemPrompt
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

// WithProvider sets the AI model provider
func (c *WSConfig) WithProvider(provider ModelProvider) *WSConfig {
	c.Provider = provider
	return c
}

// WithOpenRouter sets OpenRouter as the provider with the specified model
func (c *WSConfig) WithOpenRouter(model string) *WSConfig {
	c.Provider = ProviderOpenRouter
	if model != "" {
		c.ModelName = model
	}
	return c
}

// WithSiteInfo sets the site URL and name for OpenRouter rankings
func (c *WSConfig) WithSiteInfo(url, name string) *WSConfig {
	c.SiteURL = url
	c.SiteName = name
	return c
}

// WithTemperature sets the temperature for model generation
func (c *WSConfig) WithTemperature(temp float64) *WSConfig {
	c.Temperature = &temp
	return c
}

// WithMaxTokens sets the max tokens for model generation
func (c *WSConfig) WithMaxTokens(tokens int) *WSConfig {
	c.MaxTokens = &tokens
	return c
}
