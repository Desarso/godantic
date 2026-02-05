package godantic

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	models "github.com/Desarso/godantic/models"
	"github.com/Desarso/godantic/models/cerebras"
	"github.com/Desarso/godantic/models/gemini"
	"github.com/Desarso/godantic/models/groq"
	"github.com/Desarso/godantic/models/openrouter"
	"github.com/Desarso/godantic/stores"
)

//go:embed schemas/cached_schemas/*.json
var schemaFiles embed.FS

type Model interface {
	Model_Request(request models.Model_Request, tools []models.FunctionDeclaration, conversationHistory []stores.Message) (models.Model_Response, error)
	Stream_Model_Request(request models.Model_Request, tools []models.FunctionDeclaration, conversationHistory []stores.Message) (<-chan models.Model_Response, <-chan error)
}

type Agent struct {
	Model  Model
	Tools  []models.FunctionDeclaration
	Memory MemoryManager
}

// Create_Agent creates an agent with the given model and tools
func Create_Agent(model Model, tools []models.FunctionDeclaration, memory ...MemoryManager) Agent {
	var mem MemoryManager
	if len(memory) > 0 {
		mem = memory[0]
	}
	return Agent{
		Model:  model,
		Tools:  tools,
		Memory: mem,
	}
}

// Create_Agent_From_Config creates an agent from a WSConfig
func Create_Agent_From_Config(config *WSConfig, tools []models.FunctionDeclaration, memory ...MemoryManager) Agent {
	var model Model

	switch config.Provider {
	case ProviderOpenRouter:
		model = &openrouter.OpenRouter_Model{
			Model:       config.ModelName,
			Temperature: config.Temperature,
			MaxTokens:   config.MaxTokens,
			SiteURL:     config.SiteURL,
			SiteName:    config.SiteName,
		}
	case ProviderGroq:
		model = &groq.Groq_Model{
			Model:        config.ModelName,
			Temperature:  config.Temperature,
			MaxTokens:    config.MaxTokens,
			SystemPrompt: config.SystemPrompt,
		}
	case ProviderCerebras:
		model = &cerebras.Cerebras_Model{
			Model:        config.ModelName,
			Temperature:  config.Temperature,
			MaxTokens:    config.MaxTokens,
			SystemPrompt: config.SystemPrompt,
		}
	case ProviderGemini:
		fallthrough
	default:
		model = &gemini.Gemini_Model{
			Model: config.ModelName,
		}
	}

	var mem MemoryManager
	if len(memory) > 0 {
		mem = memory[0]
	}
	return Agent{
		Model:  model,
		Tools:  tools,
		Memory: mem,
	}
}

// NewGeminiModel creates a new Gemini model instance
func NewGeminiModel(modelName string) *gemini.Gemini_Model {
	if modelName == "" {
		modelName = "gemini-2.0-flash"
	}
	return &gemini.Gemini_Model{
		Model: modelName,
	}
}

// NewOpenRouterModel creates a new OpenRouter model instance
func NewOpenRouterModel(modelName string) *openrouter.OpenRouter_Model {
	if modelName == "" {
		modelName = "openai/gpt-4o-mini"
	}
	return &openrouter.OpenRouter_Model{
		Model: modelName,
	}
}

// NewOpenRouterModelWithOptions creates a new OpenRouter model with full configuration
func NewOpenRouterModelWithOptions(modelName string, temperature *float64, maxTokens *int, siteURL, siteName string) *openrouter.OpenRouter_Model {
	if modelName == "" {
		modelName = "openai/gpt-4o-mini"
	}
	return &openrouter.OpenRouter_Model{
		Model:       modelName,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		SiteURL:     siteURL,
		SiteName:    siteName,
	}
}

// NewOpenRouterModelWithBaseURL creates a new OpenRouter-compatible model with custom base URL and API key
func NewOpenRouterModelWithBaseURL(modelName, baseURL, apiKeyEnv string, temperature *float64, maxTokens *int) *openrouter.OpenRouter_Model {
	if modelName == "" {
		modelName = "openai/gpt-4o-mini"
	}
	return &openrouter.OpenRouter_Model{
		Model:       modelName,
		BaseURL:     baseURL,
		APIKeyEnv:   apiKeyEnv,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}
}

// NewGroqModel creates a new Groq model instance
func NewGroqModel(modelName string) *groq.Groq_Model {
	if modelName == "" {
		modelName = "llama-3.1-70b-versatile"
	}
	return &groq.Groq_Model{
		Model: modelName,
	}
}

// NewGroqModelWithOptions creates a new Groq model with full configuration
func NewGroqModelWithOptions(modelName string, temperature *float64, maxTokens *int, systemPrompt string) *groq.Groq_Model {
	if modelName == "" {
		modelName = "llama-3.1-70b-versatile"
	}
	return &groq.Groq_Model{
		Model:        modelName,
		Temperature:  temperature,
		MaxTokens:    maxTokens,
		SystemPrompt: systemPrompt,
	}
}

// NewCerebrasModel creates a new Cerebras model instance
func NewCerebrasModel(modelName string) *cerebras.Cerebras_Model {
	if modelName == "" {
		modelName = "llama-3.3-70b"
	}
	return &cerebras.Cerebras_Model{
		Model: modelName,
	}
}

// NewCerebrasModelWithOptions creates a new Cerebras model with full configuration
func NewCerebrasModelWithOptions(modelName string, temperature *float64, maxTokens *int, systemPrompt string, topP *float64, seed *int) *cerebras.Cerebras_Model {
	if modelName == "" {
		modelName = "llama-3.3-70b"
	}
	return &cerebras.Cerebras_Model{
		Model:        modelName,
		Temperature:  temperature,
		MaxTokens:    maxTokens,
		SystemPrompt: systemPrompt,
		TopP:         topP,
		Seed:         seed,
	}
}

// tool takes a function, finds its generated JSON schema, and returns a Tool struct.
func Create_Tool(fn interface{}) (models.FunctionDeclaration, error) {
	fnValue := reflect.ValueOf(fn)
	if fnValue.Kind() != reflect.Func {
		return models.FunctionDeclaration{}, errors.New("input must be a function")
	}

	// Get the function name
	fullName := runtime.FuncForPC(fnValue.Pointer()).Name()
	// Extract the base name (e.g., "Search_Google" from "main.Search_Google" or "package.Search_Google")
	lastDot := strings.LastIndex(fullName, ".")
	funcName := fullName
	if lastDot != -1 {
		funcName = fullName[lastDot+1:]
	}

	// Construct the path to the schema file in the embedded filesystem
	schemaPath := filepath.Join("schemas", "cached_schemas", funcName+".json")

	// Read the schema file from embedded filesystem
	schemaBytes, err := schemaFiles.ReadFile(schemaPath)
	if err != nil {
		return models.FunctionDeclaration{}, fmt.Errorf("failed to read embedded schema file '%s': %w", schemaPath, err)
	}

	// Unmarshal the JSON schema into FunctionDeclarations
	// Note: The gen_schema tool seems to output the schema for *one* function per file.
	var funcDecl models.FunctionDeclaration
	err = json.Unmarshal(schemaBytes, &funcDecl)
	if err != nil {
		return models.FunctionDeclaration{}, fmt.Errorf("failed to unmarshal schema from '%s': %w", schemaPath, err)
	}

	// Construct the Tool struct
	tool := models.FunctionDeclaration{
		Name:        funcDecl.Name,
		Description: funcDecl.Description,
		Parameters:  funcDecl.Parameters,
		Callable:    fn,
	}

	return tool, nil
}

func Create_Tools(fns []interface{}) ([]models.FunctionDeclaration, error) {
	tools := []models.FunctionDeclaration{}
	for _, fn := range fns {
		tool, err := Create_Tool(fn)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func (agent *Agent) Run(request models.Model_Request, conversationHistory []stores.Message) (models.Model_Response, error) {
	return agent.Model.Model_Request(request, agent.Tools, conversationHistory)
}

func (agent *Agent) Run_Stream(request models.Model_Request, conversationHistory []stores.Message) (<-chan models.Model_Response, <-chan error) {
	return agent.Model.Stream_Model_Request(request, agent.Tools, conversationHistory)
}

// ExecuteTool executes a tool dynamically by name and arguments
func (agent *Agent) ExecuteTool(functionName string, functionCallArgs map[string]interface{}, sessionID string) (string, error) {
	var toolResultJSON string
	var toolExecErr error
	toolFound := false

	// Trim whitespace from function name (some models output with leading/trailing spaces)
	functionName = strings.TrimSpace(functionName)

	for _, tool := range agent.Tools {
		if tool.Name == functionName {
			toolFound = true
			callableFunc := reflect.ValueOf(tool.Callable)

			// Basic Validation
			if callableFunc.Kind() != reflect.Func {
				toolExecErr = fmt.Errorf("internal error: tool '%s' is not callable", functionName)
				break
			}
			funcType := callableFunc.Type()

			// Validate return signature: must return (string, error)
			if !(funcType.NumOut() == 2 && funcType.Out(0).Kind() == reflect.String &&
				funcType.Out(1).Implements(reflect.TypeOf((*error)(nil)).Elem())) {
				toolExecErr = fmt.Errorf("internal error: tool '%s' has incompatible return signature", functionName)
				break
			}

			var argsToPass []reflect.Value

			// Handle different input signatures
			numIn := funcType.NumIn()
			if numIn == 0 {
				// No parameters: func() (string, error)
				argsToPass = []reflect.Value{}
			} else {
				// Check if all parameters are strings
				allStrings := true
				for i := 0; i < numIn; i++ {
					if funcType.In(i).Kind() != reflect.String {
						allStrings = false
						break
					}
				}

				if !allStrings {
					toolExecErr = fmt.Errorf("internal error: tool '%s' has incompatible input signature (all params must be string)", functionName)
					break
				}

				if numIn == 1 {
					// Single string parameter: func(string) (string, error)
					var stringArg string
					if len(functionCallArgs) == 0 {
						stringArg = "" // Allow empty args for single-param functions
					} else if len(functionCallArgs) == 1 {
						var argName string
						var argValueInterface interface{}
						for key, val := range functionCallArgs {
							argName = key
							argValueInterface = val
							break
						}
						var ok bool
						stringArg, ok = argValueInterface.(string)
						if !ok {
							toolExecErr = fmt.Errorf("invalid argument type for '%s': expected string for arg '%s', got %T", functionName, argName, argValueInterface)
							break
						}
					} else {
						toolExecErr = fmt.Errorf("tool '%s' expects 1 argument from model, got %d args: %v", functionName, len(functionCallArgs), functionCallArgs)
						break
					}
					argsToPass = []reflect.Value{reflect.ValueOf(stringArg)}
				} else {
					// Multiple string parameters: func(s1, s2, ...) (string, error)
					// We need to get parameter names from the schema to match them correctly
					argsToPass = make([]reflect.Value, numIn)

					// Get parameter names from schema (tool is in scope from the outer loop)
					schema := tool

					// Extract parameter order from schema's "required" field (maintains order)
					paramOrder := schema.Parameters.Required
					if len(paramOrder) != numIn {
						// Fallback: try to get from properties keys (though order may not be guaranteed)
						paramOrder = make([]string, 0, len(schema.Parameters.Properties))
						for key := range schema.Parameters.Properties {
							paramOrder = append(paramOrder, key)
						}
					}

					if len(paramOrder) != numIn {
						toolExecErr = fmt.Errorf("internal error: tool '%s' parameter count mismatch (schema: %d, func: %d)", functionName, len(paramOrder), numIn)
						break
					}

					// Map args by parameter name in order
					validArgs := true
					for i, paramName := range paramOrder {
						argValue, exists := functionCallArgs[paramName]
						if !exists {
							toolExecErr = fmt.Errorf("missing required argument '%s' for tool '%s'", paramName, functionName)
							validArgs = false
							break
						}
						stringVal, ok := argValue.(string)
						if !ok {
							toolExecErr = fmt.Errorf("invalid argument type for '%s': expected string for arg '%s', got %T", functionName, paramName, argValue)
							validArgs = false
							break
						}
						argsToPass[i] = reflect.ValueOf(stringVal)
					}
					if !validArgs {
						break
					}
				}
			}

			// Call Function
			results := callableFunc.Call(argsToPass)

			// Process results (string, error)
			if errResult := results[1].Interface(); errResult != nil {
				if execErr, ok := errResult.(error); ok {
					toolExecErr = execErr // Store the actual error from the tool
				} else {
					toolExecErr = fmt.Errorf("internal error: tool '%s' returned invalid error type", functionName)
				}
			} else {
				// Success: Extract the string result
				if successResultString, ok := results[0].Interface().(string); ok {
					// Wrap the string result in a standard JSON object for the FunctionResponse part
					resultMap := map[string]string{"result": successResultString}
					resultBytes, marshalErr := json.Marshal(resultMap)
					if marshalErr != nil {
						toolExecErr = fmt.Errorf("failed marshal result for '%s': %v", functionName, marshalErr)
					} else {
						toolResultJSON = string(resultBytes) // Store the JSON string of the result map
					}
				} else {
					toolExecErr = fmt.Errorf("internal error: tool '%s' returned non-string result", functionName)
				}
			}
			break // Tool found and execution attempted
		}
	}

	if !toolFound {
		toolExecErr = fmt.Errorf("unknown or unavailable tool: %s", functionName)
	}

	// If execution resulted in an error (any stage), ensure toolResultJSON reflects it
	if toolExecErr != nil {
		errorMap := map[string]string{"error": toolExecErr.Error()}
		errorBytes, _ := json.Marshal(errorMap) // Marshal the error map
		toolResultJSON = string(errorBytes)     // This becomes the result
	}

	return toolResultJSON, toolExecErr // Return the JSON string and the Go error
}

// ApproveTool checks if a tool should be auto-approved
func (agent *Agent) ApproveTool(name string, args map[string]interface{}) (bool, error) {
	return Tool_Approver(name, args)
}

// HistoryWarner is an optional interface that models can implement
// to report warnings when adapting conversation history
type HistoryWarner interface {
	SetHistoryWarningCallback(callback func(warnings []models.HistoryWarning))
}

// SetHistoryWarningCallback sets a callback for history warnings if the model supports it
// Returns true if the model supports warnings, false otherwise
func (agent *Agent) SetHistoryWarningCallback(callback func(warnings []models.HistoryWarning)) bool {
	if warner, ok := agent.Model.(HistoryWarner); ok {
		warner.SetHistoryWarningCallback(callback)
		return true
	}
	return false
}
