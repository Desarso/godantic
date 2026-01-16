package models

type Chat_Request struct {
	Message         User_Message `json:"message"`
	Conversation_ID string       `json:"conversation_id"`
}

type Model_Request struct {
	User_Message *User_Message  `json:"message,omitempty"`
	Tool_Results *[]Tool_Result `json:"tool_results,omitempty"`
}

type Tool_Result struct {
	Tool_ID     string `json:"tool_id"` // The tool call ID to match with the tool call
	Tool_Name   string `json:"tool_name"`
	Tool_Output string `json:"tool_output"`
}
