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
	"github.com/Desarso/godantic/stores"
)

//go:embed schemas/cached_schemas/*.json
var schemaFiles embed.FS

type Model interface {
	Model_Request(request models.Model_Request, tools []models.FunctionDeclaration, conversationHistory []stores.Message) (models.Model_Response, error)
	Stream_Model_Request(request models.Model_Request, tools []models.FunctionDeclaration, conversationHistory []stores.Message) (<-chan models.Model_Response, <-chan error)
}

type Agent struct {
	Model Model
	Tools []models.FunctionDeclaration
}

// create_agent is a placeholder/example function
func Create_Agent(model Model, tools []models.FunctionDeclaration) Agent {
	// Implementation depends on how agents are actually created and used.
	// For now, just return a basic struct.
	return Agent{
		Model: model,
		Tools: tools,
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
			// Validate signature: func(string) (string, error)
			if !(funcType.NumIn() == 1 && funcType.In(0).Kind() == reflect.String &&
				funcType.NumOut() == 2 && funcType.Out(0).Kind() == reflect.String &&
				funcType.Out(1).Implements(reflect.TypeOf((*error)(nil)).Elem())) {
				toolExecErr = fmt.Errorf("internal error: tool '%s' has incompatible signature", functionName)
				break
			}

			// Argument Extraction
			var stringArg string
			if len(functionCallArgs) != 1 {
				toolExecErr = fmt.Errorf("tool '%s' expects 1 argument from model, got %d args: %v", functionName, len(functionCallArgs), functionCallArgs)
				break
			}
			var argName string
			var argValueInterface interface{}
			for key, val := range functionCallArgs { // Get the single key/value
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

			// Call Function
			argsToPass := []reflect.Value{reflect.ValueOf(stringArg)}
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
