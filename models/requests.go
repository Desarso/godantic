package models

type Chat_Request struct {
	Message         User_Message `json:"message"`
	Conversation_ID string       `json:"conversation_id"`
}

type Model_Request struct {
	User_Message *User_Message  `json:"message,omitempty"`
	Tool_Results *[]Tool_Result `json:"tool_results,omitempty"`
	// Client_ID optionally identifies the calling client for prompt selection.
	Client_ID string `json:"client_id,omitempty"`
	// Input_Mode optionally indicates how the user provided the message.
	// Supported values: "text" (default), "voice".
	Input_Mode string `json:"input_mode,omitempty"`
	// Language_Code optionally indicates the user's preferred language.
	// Supported values: "en" (English, default), "es" (Spanish), etc.
	Language_Code string `json:"language_code,omitempty"`
}

type Tool_Result struct {
	Tool_ID     string `json:"tool_id"` // The tool call ID to match with the tool call
	Tool_Name   string `json:"tool_name"`
	Tool_Output string `json:"tool_output"`
}
