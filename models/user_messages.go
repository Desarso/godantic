package models

type User_Message struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

type Content struct {
	Parts []User_Part `json:"parts"`
}

type User_Part struct {
	Text             string            `json:"text,omitempty"`
	InlineData       *InlineData       `json:"inline_data,omitempty"`
	ImageData        *ImageData        `json:"image_data,omitempty"`
	FileData         *FileData         `json:"file_data,omitempty"`
	FunctionResponse *FunctionResponse `json:"function_response,omitempty"`
}

type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type ImageData struct {
	MimeType  string  `json:"mimeType"`
	FileUrl   string  `json:"fileUrl,omitempty"`
	GoogleUri *string `json:"googleUri,omitempty"`
}

type FileData struct {
	MimeType  string  `json:"mimeType"`
	FileUrl   string  `json:"fileUrl"`
	GoogleUri *string `json:"googleUri,omitempty"`
}

// FunctionResponse represents a tool's response in user messages
type FunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}
