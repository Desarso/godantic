package common_tools

import "fmt"

// Consult_Model is a stub tool function for consulting a more capable AI model.
// The actual execution is handled by the session's consultant engine via special-case
// routing in executeTool (similar to Execute_TypeScript). This stub exists so the
// schema resolver can find the function and Create_Tool can register it.
//
// Parameters:
//   - mode: "advisor" (text advice) or "takeover" (consultant runs tools)
//   - goal: what the agent is trying to accomplish
//   - what_tried: what approaches were attempted and why they failed
//   - context: error messages, tool outputs, constraints (optional)
//   - specific_ask: the specific question for the consultant
func Consult_Model(mode, goal, what_tried, context, specific_ask string) (string, error) {
	// This should never be called directly — the session routes this tool
	// through the consultant engine before it reaches here.
	return "", fmt.Errorf("Consult_Model must be executed through the session's consultant engine")
}
