package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/Desarso/godantic"
	"golang.org/x/tools/go/packages"
)

// JSONSchema represents a basic JSON Schema structure
type JSONSchema struct {
	Type                 string                `json:"type,omitempty"` // Use omitempty for interface{}
	Description          string                `json:"description,omitempty"`
	Title                string                `json:"title,omitempty"` // For named types
	Properties           map[string]JSONSchema `json:"properties,omitempty"`
	Items                *JSONSchema           `json:"items,omitempty"`                // For slices/arrays
	Required             []string              `json:"required,omitempty"`             // For objects
	AdditionalProperties *JSONSchema           `json:"additionalProperties,omitempty"` // For maps
	// Add other fields like format if needed, e.g., for time.Time
}

// ToolFunctionSchema represents the desired top-level structure for the JSON output
type ToolFunctionSchema struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  JSONSchema `json:"parameters"`
}

func main() {
	funcName := flag.String("func", "", "Name of the function to generate schema for")
	fileName := flag.String("file", "main.go", "Go source file containing the function")        // Allow specifying the file
	outDir := flag.String("out", "cached_schemas", "Output directory for the generated schema") // Add output directory flag
	flag.Parse()

	if *funcName == "" {
		log.Fatal("Function name must be provided using -func flag")
	}

	// --- Load the package ---
	// Use go/packages for more robust loading and type checking
	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedTypes |
			packages.NeedTypesSizes |
			packages.NeedSyntax |
			packages.NeedTypesInfo,
		Fset: token.NewFileSet(),
		// Potentially add Tests: false if you don't need test files
	}

	// Construct the pattern to load. Using the directory containing the file.
	loadPattern := filepath.Dir(*fileName)
	log.Printf("Loading package info for pattern: %s", loadPattern)
	pkgs, err := packages.Load(cfg, loadPattern)
	if err != nil {
		log.Fatalf("Failed to load package(s) for pattern '%s': %v", loadPattern, err)
	}

	if len(pkgs) == 0 {
		log.Fatalf("No packages found for pattern: %s", loadPattern)
	}

	// Typically, we expect one package matching the directory
	if len(pkgs) > 1 {
		log.Printf("Warning: Loaded multiple packages. Using the first one: %s", pkgs[0].PkgPath)
	}

	pkg := pkgs[0]

	// Check for errors during loading/type-checking
	// Aggregate errors from all loaded packages (though we focus on the first)
	var loadErrors []string
	for _, p := range pkgs {
		for _, err := range p.Errors {
			loadErrors = append(loadErrors, err.Error())
		}
	}
	if len(loadErrors) > 0 {
		log.Fatalf("Errors during package loading/type checking:\n%s", strings.Join(loadErrors, "\n"))
	}

	// Get the *types.Info and *token.FileSet from the loaded package
	info := pkg.TypesInfo
	fset := cfg.Fset // Use the FileSet from the config

	// --- Find the specific AST file node for the target file ---
	var node *ast.File
	absoluteFileName, err := filepath.Abs(*fileName)
	if err != nil {
		log.Fatalf("Failed to get absolute path for target file '%s': %v", *fileName, err)
	}
	for _, syntaxFile := range pkg.Syntax {
		// Get the absolute path of the syntax file being checked
		filePos := fset.Position(syntaxFile.Pos())
		absoluteSyntaxFileName, err := filepath.Abs(filePos.Filename)
		if err != nil {
			log.Printf("Warning: Could not get absolute path for syntax file '%s': %v. Skipping.", filePos.Filename, err)
			continue
		}
		if absoluteSyntaxFileName == absoluteFileName {
			node = syntaxFile
			break
		}
	}
	if node == nil {
		log.Fatalf("Could not find AST node for the target file '%s' within package '%s'", *fileName, pkg.PkgPath)
	}

	// --- Find the function object ---
	// Now use the package's scope to find the function
	scope := pkg.Types.Scope()
	obj := scope.Lookup(*funcName)

	if obj == nil {
		log.Fatalf("Function '%s' not found in package '%s'", *funcName, pkg.PkgPath)
	}

	funcObj, ok := obj.(*types.Func)
	if !ok {
		log.Fatalf("Object '%s' found but is not a function", *funcName)
	}

	// Check if the function is actually defined in our target file (important if package has multiple files)
	funcPos := fset.Position(funcObj.Pos())
	absoluteFuncFileName, err := filepath.Abs(funcPos.Filename)
	if err != nil {
		log.Printf("Warning: Could not get absolute path for function definition file '%s': %v", funcPos.Filename, err)
	}
	if absoluteFuncFileName != absoluteFileName {
		log.Fatalf("Function '%s' found in package '%s', but it is defined in file '%s', not the target file '%s'", *funcName, pkg.PkgPath, funcPos.Filename, *fileName)
	}

	// --- Extract function signature ---
	funcSig, ok := funcObj.Type().(*types.Signature)
	if !ok {
		// This should theoretically not happen if it's a *types.Func, but check anyway
		log.Fatalf("Object '%s' found but is not a function signature", *funcName)
	}

	// --- Find the function's AST Node for Doc Comments ---
	var funcDecl *ast.FuncDecl
	ast.Inspect(node, func(n ast.Node) bool {
		// Check if the node is a function declaration
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true // Continue inspecting
		}
		// Check if the function name matches the one we found via type checking
		if fn.Name != nil && info.Defs[fn.Name] == funcObj {
			funcDecl = fn
			return false // Stop inspection, we found it
		}
		return true
	})

	funcDescription := ""
	if funcDecl != nil && funcDecl.Doc != nil {
		funcDescription = strings.TrimSpace(funcDecl.Doc.Text()) // Get comment text
	} else {
		log.Printf("Warning: No documentation comment found for function '%s'", *funcName)
	}

	// --- Generate schema for parameters ---
	params := funcSig.Params() // Get the tuple of parameters
	// This part generates the schema that will go *inside* the 'parameters' field
	parameterSchema := JSONSchema{
		Type:       "object",
		Properties: make(map[string]JSONSchema),
		Required:   []string{},
		// We can add a generic description here, or leave it blank as the main description is above
		// Description: fmt.Sprintf("Parameters for function '%s'", *funcName),
	}

	log.Printf("Generating schema for %d parameters of function '%s'...", params.Len(), *funcName)

	for i := 0; i < params.Len(); i++ {
		param := params.At(i)
		paramName := param.Name()
		paramType := param.Type()

		log.Printf("  Processing parameter: %s (%s)", paramName, paramType.String())

		// Generate schema for this parameter's type
		paramFieldSchema, err := generateSchemaForType(paramType, pkg)
		if err != nil {
			log.Printf("Warning: Could not generate schema for parameter '%s' (type %s): %v. Skipping.", paramName, paramType.String(), err)
			continue // Skip this parameter if schema generation fails
		}

		// Extract parameter description (if applicable, e.g., from struct fields)
		// This logic is complex and might need refinement or a separate function
		// if paramFieldSchema.Type == "object" && paramFieldSchema.Title != "" {
		// 	 addFieldDescriptions(&paramFieldSchema, paramType, info, node)
		// }

		// Add the generated schema to the properties map
		parameterSchema.Properties[paramName] = paramFieldSchema

		// Determine if the parameter is required.
		// Simple heuristic: non-pointer types are often required.
		// Let's keep making them required for this example, assuming non-pointer means required.
		if _, isPointer := paramType.(*types.Pointer); !isPointer {
			parameterSchema.Required = append(parameterSchema.Required, paramName)
		}

	}
	sort.Strings(parameterSchema.Required) // Keep required fields sorted for deterministic output

	// --- Assemble the final ToolFunctionSchema ---
	finalSchema := ToolFunctionSchema{
		Name:        *funcName,
		Description: funcDescription,
		Parameters:  parameterSchema,
	}

	// --- Create cache directory ---
	cacheDir := *outDir // Use the output directory flag
	log.Printf("Ensuring output directory '%s' exists...", cacheDir)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Fatalf("Failed to create directory '%s': %v", cacheDir, err)
	}

	// --- Marshal schema to JSON ---
	log.Println("Marshalling schema to JSON...")
	schemaJSON, err := json.MarshalIndent(finalSchema, "", "  ") // Marshal the final structure
	if err != nil {
		log.Fatalf("Failed to marshal schema to JSON: %v", err)
	}

	// --- Save JSON to file ---
	outputFileName := *funcName + ".json"
	outputFile := filepath.Join(cacheDir, outputFileName)
	log.Printf("Writing schema to file '%s'...", outputFile)
	err = os.WriteFile(outputFile, schemaJSON, 0644) // Standard file permissions
	if err != nil {
		log.Fatalf("Failed to write schema to file '%s': %v", outputFile, err)
	}

	log.Printf("Successfully generated schema for function '%s' and saved to '%s'", *funcName, outputFile)
}

// generateSchemaForType recursively generates JSONSchema for a given Go type.
// Updated to accept *packages.Package to potentially access more info later
func generateSchemaForType(t types.Type, pkg *packages.Package) (JSONSchema, error) {
	// Use Underlying() to handle defined types (e.g., type MyInt int) correctly
	switch typ := t.Underlying().(type) {
	case *types.Basic:
		// Handle basic types (int, string, bool, etc.)
		schema := JSONSchema{}
		kind := typ.Kind()
		switch kind {
		case types.Bool:
			schema.Type = "boolean"
		case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
			types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.Uintptr:
			schema.Type = "integer"
		case types.Float32, types.Float64:
			schema.Type = "number"
		case types.Complex64, types.Complex128:
			// Represent complex numbers as strings or objects? String is simpler.
			schema.Type = "string"
			schema.Description = "Complex number (represented as string)"
		case types.String:
			schema.Type = "string"
		// case types.UnsafePointer: // Ignored
		// case types.UntypedNil: // Ignored
		default:
			// Handle Untyped types if necessary, or return error
			return JSONSchema{}, fmt.Errorf("unsupported basic type kind: %s", typ.String())
		}
		// Add title for named basic types (e.g., type MyString string)
		if named, ok := t.(*types.Named); ok {
			schema.Title = named.Obj().Name()
		}
		return schema, nil

	case *types.Slice:
		// Handle slices -> JSON array
		elemSchema, err := generateSchemaForType(typ.Elem(), pkg)
		if err != nil {
			return JSONSchema{}, fmt.Errorf("failed to get schema for slice element type '%s': %w", typ.Elem().String(), err)
		}
		schema := JSONSchema{Type: "array", Items: &elemSchema}
		if named, ok := t.(*types.Named); ok {
			schema.Title = named.Obj().Name()
		}
		return schema, nil

	case *types.Array:
		// Handle fixed-size arrays -> JSON array (could add minItems/maxItems)
		elemSchema, err := generateSchemaForType(typ.Elem(), pkg)
		if err != nil {
			return JSONSchema{}, fmt.Errorf("failed to get schema for array element type '%s': %w", typ.Elem().String(), err)
		}
		schema := JSONSchema{Type: "array", Items: &elemSchema}
		// Could add: MinItems: typ.Len(), MaxItems: typ.Len() if needed in schema struct
		if named, ok := t.(*types.Named); ok {
			schema.Title = named.Obj().Name()
		}
		return schema, nil

	case *types.Struct:
		// Handle structs -> JSON object
		schema := JSONSchema{
			Type:       "object",
			Properties: make(map[string]JSONSchema),
			Required:   []string{},
		}
		// Add title for named structs
		if named, ok := t.(*types.Named); ok {
			schema.Title = named.Obj().Name()
		}

		for i := 0; i < typ.NumFields(); i++ {
			field := typ.Field(i)
			if !field.Exported() { // Skip unexported fields as they aren't typically part of JSON
				continue
			}

			fieldName := field.Name() // Default field name

			// Check for json tag
			tag := typ.Tag(i)                                // Get struct tag string: `json:"name,omitempty"`
			jsonInfo := parseJsonTag(reflect.StructTag(tag)) // Cast string tag to reflect.StructTag

			if jsonInfo.Name == "-" { // Skip fields explicitly ignored by json tag
				continue
			}
			if jsonInfo.Name != "" {
				fieldName = jsonInfo.Name // Use name from tag
			}

			// Generate schema for the field's type
			fieldSchema, err := generateSchemaForType(field.Type(), pkg)
			if err != nil {
				log.Printf("Warning: Could not generate schema for struct field '%s.%s': %v. Skipping field.", schema.Title, field.Name(), err)
				continue // Skip field if schema generation fails
			}

			// Add description from field comments? (Requires AST analysis - complex)
			// fieldSchema.Description = ...

			schema.Properties[fieldName] = fieldSchema

			// Determine if field is required based on `omitempty` tag
			if !jsonInfo.OmitEmpty {
				schema.Required = append(schema.Required, fieldName)
			}
		}
		// Sort required fields for deterministic output
		sort.Strings(schema.Required)
		// If no fields were added (e.g., all unexported or skipped), return empty object or handle as needed
		return schema, nil

	case *types.Pointer:
		// Handle pointers.
		// Let's generate the schema for the element type.
		elemSchema, err := generateSchemaForType(typ.Elem(), pkg)
		if err != nil {
			return JSONSchema{}, fmt.Errorf("failed to get schema for pointer element type '%s': %w", typ.Elem().String(), err)
		}
		// Indicate nullability by making the field itself optional (not required)
		// The caller (in main loop) will check if the param type is a pointer.
		// We could also add `"type": ["typename", "null"]` or similar if needed by the schema spec.
		// For simplicity, just return the underlying type's schema.
		return elemSchema, nil

	case *types.Map:
		// Handle maps -> JSON object with additionalProperties
		// JSON object keys must be strings. Check if Go map key is string-compatible.
		keyType := typ.Key().Underlying()
		if b, ok := keyType.(*types.Basic); !ok || b.Kind() != types.String {
			log.Printf("Warning: Map key type '%s' is not string. JSON schema for additionalProperties might be inaccurate as object keys must be strings.", keyType.String())
			// Fallback to a generic object? Or proceed assuming string conversion?
		}

		// Generate schema for the map value type
		valueSchema, err := generateSchemaForType(typ.Elem(), pkg)
		if err != nil {
			return JSONSchema{}, fmt.Errorf("failed to get schema for map value type '%s': %w", typ.Elem().String(), err)
		}

		schema := JSONSchema{Type: "object", AdditionalProperties: &valueSchema}
		schema.Description = fmt.Sprintf("Map with %s keys and %s values", typ.Key().String(), typ.Elem().String())
		if named, ok := t.(*types.Named); ok {
			schema.Title = named.Obj().Name()
		}
		return schema, nil

	case *types.Interface:
		// Handle interfaces. interface{} allows any type. Specific interfaces are harder.
		if typ.Empty() {
			// For interface{}, omit type field, allowing any JSON type.
			schema := JSONSchema{Description: "Any type (interface{})"}
			if named, ok := t.(*types.Named); ok { // Handle type Any = interface{}
				schema.Title = named.Obj().Name()
			}
			return schema, nil
		}
		// For non-empty interfaces, it's ambiguous what JSON schema to generate.
		// Treat as a generic object for now, but this might not be accurate.
		log.Printf("Warning: Generating generic 'object' schema for non-empty interface '%s'. This may not capture the expected structure.", t.String())
		schema := JSONSchema{Type: "object", Description: fmt.Sprintf("Interface type: %s (represented as generic object)", t.String())}
		if named, ok := t.(*types.Named); ok {
			schema.Title = named.Obj().Name()
		}
		return schema, nil

	case *types.Named:
		// This case handles types defined like `type MyType UnderlyingType`.
		// Recurse on the underlying type. The title should be added by the specific type case.
		// We can potentially extract the doc comment for the named type here if needed.
		// obj := typ.Obj() // Get the type name object
		// if typeSpec := findAstTypeSpec(rootNode, obj); typeSpec != nil && typeSpec.Doc != nil {
		// 	 namedTypeDescription = typeSpec.Doc.Text()
		// }
		underlyingSchema, err := generateSchemaForType(typ.Underlying(), pkg)
		if err != nil {
			return JSONSchema{}, err
		}
		// Ensure the title is set to the named type's name
		if underlyingSchema.Title == "" { // Only set if not already set by struct/basic handler
			underlyingSchema.Title = typ.Obj().Name()
		}
		// Add description from named type definition?
		// underlyingSchema.Description = namedTypeDescription

		return underlyingSchema, nil

	case *types.Chan:
		return JSONSchema{}, fmt.Errorf("unsupported type: channel (%s)", t.String())

	case *types.Tuple:
		// Tuples are usually function parameters or results, not standalone types
		// We handle function params/results specifically in the main logic
		return JSONSchema{}, fmt.Errorf("unexpected tuple type encountered: %s", t.String())

	default:
		// Catch-all for unsupported types
		return JSONSchema{}, fmt.Errorf("unhandled type: %T (%s)", t, t.String())
	}
}

// --- Helper Functions ---

// jsonTagInfo holds parsed information from a `json:"..."` struct tag.
type jsonTagInfo struct {
	Name      string
	OmitEmpty bool
	// Add other tag options like 'string' if needed
}

// parseJsonTag extracts relevant info from a struct field's JSON tag.
func parseJsonTag(tag reflect.StructTag) jsonTagInfo {
	jsonValue := tag.Get("json")
	if jsonValue == "" {
		// No json tag present
		return jsonTagInfo{}
	}

	parts := strings.Split(jsonValue, ",")
	info := jsonTagInfo{Name: parts[0]} // First part is the name (can be empty)

	for _, part := range parts[1:] {
		switch strings.TrimSpace(part) {
		case "omitempty":
			info.OmitEmpty = true
		case "string":
			// Handle 'string' tag if needed (e.g., forces number to be marshalled as string)
			// This might influence the generated schema type.
		}
	}

	return info
}

// Helper function for dynamic tool execution
func executeToolDynamically(agent godantic.Agent, functionName string, functionCallArgs map[string]interface{}, sessionID string) (string, error) {
	var toolResultJSON string
	var toolExecErr error
	toolFound := false

	for _, tool := range agent.Tools {
		if tool.Name == functionName {
			toolFound = true
			callableFunc := reflect.ValueOf(tool.Callable)

			// --- Basic Validation: Is it a function? ---
			if callableFunc.Kind() != reflect.Func {
				toolExecErr = fmt.Errorf("internal error: tool '%s' is not callable", functionName)
				// toolResultJSON generation happens after the loop
				break
			}

			funcType := callableFunc.Type()
			// --- Signature Validation: func(string) (string, error) ---
			// (Assuming simple tools for now, can be expanded later)
			if !(funcType.NumIn() == 1 && funcType.In(0).Kind() == reflect.String &&
				funcType.NumOut() == 2 && funcType.Out(0).Kind() == reflect.String &&
				funcType.Out(1).Implements(reflect.TypeOf((*error)(nil)).Elem())) {

				log.Printf("[WS %s] Error: Tool function '%s' must have signature func(string) (string, error) for current dynamic execution.", sessionID, functionName)
				toolExecErr = fmt.Errorf("internal error: tool '%s' has incompatible signature", functionName)
				break
			}
			// -------------------------------------------------------------

			// --- Argument Extraction for single string parameter ---
			var stringArg string
			// Expect exactly one argument in the map from the model
			if len(functionCallArgs) != 1 {
				toolExecErr = fmt.Errorf("tool '%s' expects exactly one argument object from model, but got %d keys", functionName, len(functionCallArgs))
				break
			}

			// Extract the value - assuming only one key/value pair
			var argName string
			var argValueInterface interface{}
			for key, val := range functionCallArgs {
				argName = key // For logging/error messages
				argValueInterface = val
				break // Only take the first one
			}

			// Assert that the value is a string
			var ok bool
			stringArg, ok = argValueInterface.(string)
			if !ok {
				toolExecErr = fmt.Errorf("invalid argument type for '%s': expected string for argument '%s', but got %T", functionName, argName, argValueInterface)
				break
			}
			// -------------------------------------------------------

			// Call the function with the extracted string argument
			argsToPass := []reflect.Value{reflect.ValueOf(stringArg)}
			log.Printf("[WS %s] Calling tool %s dynamically with arg: %s", sessionID, functionName, stringArg)
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
						toolExecErr = fmt.Errorf("failed to marshal successful tool result for '%s': %v", functionName, marshalErr)
					} else {
						toolResultJSON = string(resultBytes) // Store the JSON string of the result map
						log.Printf("[WS %s] Tool %s executed successfully. Result: %s", sessionID, functionName, toolResultJSON)
					}
				} else {
					toolExecErr = fmt.Errorf("internal error: tool '%s' returned non-string type in result position", functionName)
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
