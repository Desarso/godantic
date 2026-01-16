package gemini

import "github.com/Desarso/godantic/models"

type Gemini_response struct {
	Candidates    []Candidate   `json:"candidates"`
	UsageMetadata UsageMetadata `json:"usageMetadata"`
	ModelVersion  string        `json:"modelVersion"`
}

type Candidate struct {
	Content      Content `json:"content"`
	FinishReason string  `json:"finishReason,omitempty"`
}

type Content struct {
	Parts []Part `json:"parts"`
	Role  string `json:"role"`
}

type Part struct {
	Text         *string       `json:"text,omitempty"`
	FunctionCall *FunctionCall `json:"functionCall,omitempty"`
}

type FunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type UsageMetadata struct {
	PromptTokenCount        int                     `json:"promptTokenCount"`
	CandidatesTokenCount    int                     `json:"candidatesTokenCount"`
	TotalTokenCount         int                     `json:"totalTokenCount"`
	PromptTokensDetails     []PromptTokenDetail     `json:"promptTokensDetails"`
	CandidatesTokensDetails []CandidatesTokenDetail `json:"candidatesTokensDetails"`
}

type PromptTokenDetail struct {
	Modality   string `json:"modality"`
	TokenCount int    `json:"tokenCount"`
}

type CandidatesTokenDetail struct {
	Modality   string `json:"modality"`
	TokenCount int    `json:"tokenCount"`
}

type FileInfoResponse struct {
	File GoogleFileData `json:"file"`
}

type GoogleFileData struct {
	MimeType string `json:"mime_type,omitempty"`
	URI      string `json:"uri,omitempty"`
}

// Define structs for JSON parsing
type UploadStartRequest struct {
	File FileMetadata `json:"file"`
}

type FileMetadata struct {
	DisplayName string `json:"display_name"`
}

// type UploadStartResponse struct {
// 	FileUploadURL string `json:"x-goog-upload-url"` // We'll get this from headers, no need to parse JSON body for this
// }

type Request_Part struct {
	Text             string                   `json:"text,omitempty"`
	FileData         *FileData                `json:"file_data,omitempty"`
	InlineData       *InlineData              `json:"inline_data,omitempty"`
	FunctionCall     *models.FunctionCall     `json:"function_call,omitempty"`
	FunctionResponse *models.FunctionResponse `json:"function_response,omitempty"`
}

type FileData struct {
	MimeType string `json:"mime_type,omitempty"`
	URI      string `json:"file_uri,omitempty"`
}

type InlineData struct {
	MimeType string `json:"mime_type,omitempty"`
	Data     string `json:"data,omitempty"`
}

type Gemini_Request_Body struct {
	Contents          *[]Gemini_Content  `json:"contents"`
	Tools             *[]Gemini_Tools    `json:"tools,omitempty"`
	SystemInstruction *SystemInstruction `json:"systemInstruction,omitempty"`
}

type SystemInstruction struct {
	Parts []SystemPart `json:"parts"`
}

type SystemPart struct {
	Text string `json:"text"`
}

type Gemini_Content struct {
	Role  string         `json:"role"`
	Parts []Request_Part `json:"parts"`
}

type Gemini_Tools struct {
	FunctionDeclarations []models.FunctionDeclaration `json:"functionDeclarations"`
}
