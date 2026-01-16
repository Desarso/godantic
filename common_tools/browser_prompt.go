package common_tools

//go:generate ../../gen_schema -func=Browser_Prompt -file=browser_prompt.go -out=../schemas/cached_schemas

// Browser_Prompt triggers a prompt dialog in the user's browser that asks for input.
//
// NOTE: This is a frontend tool that is handled specially by the WebSocket session.
// This function itself is never actually called - the session intercepts calls to
// Browser_Prompt and handles them directly with WebSocket communication.
// This declaration exists only for schema generation and tool registration.
func Browser_Prompt(message string) (string, error) {
	// This code should never execute - frontend tools are intercepted by the session
	return `{"error": "Browser_Prompt should be handled by session, not called directly"}`, nil
}
