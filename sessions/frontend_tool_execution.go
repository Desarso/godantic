package sessions

// FrontendToolExecutor is an interface for executing frontend tools
// Applications can provide their own implementations
type FrontendToolExecutor interface {
	IsFrontendTool(functionName string) bool
	ExecuteFrontendTool(functionName string, functionCallArgs map[string]interface{}) (string, error)
}

// ExecuteToolWithContext executes a tool with WebSocket context
// If a FrontendToolExecutor is set, it checks for frontend tools first
func (as *AgentSession) ExecuteToolWithContext(functionName string, functionCallArgs map[string]interface{}) (string, error) {
	// Check if we have a frontend tool executor and if this is a frontend tool
	if as.FrontendToolExecutor != nil && as.FrontendToolExecutor.IsFrontendTool(functionName) {
		return as.FrontendToolExecutor.ExecuteFrontendTool(functionName, functionCallArgs)
	}

	// For regular tools, use the standard agent ExecuteTool
	return as.Agent.ExecuteTool(functionName, functionCallArgs, as.SessionID)
}
