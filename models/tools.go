package models

type FunctionDeclaration struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  Parameters  `json:"parameters"`
	Callable    interface{} `json:"-"`
}

// Parameters defines the JSON Schema for function parameters
type Parameters struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Required   []string               `json:"required"`
}
