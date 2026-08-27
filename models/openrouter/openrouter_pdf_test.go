package openrouter

import (
	"testing"

	models "github.com/Desarso/godantic/models"
)

func TestCreateOpenRouterRequestIncludesPDFFileParser(t *testing.T) {
	model := &OpenRouter_Model{
		SupportsPDF: true,
	}
	message := models.User_Message{
		Role: "user",
		Content: models.Content{Parts: []models.User_Part{
			{Text: "Summarize this PDF."},
			{FileData: &models.FileData{
				MimeType: "application/pdf",
				FileUrl:  "https://example.com/report.pdf",
			}},
		}},
	}

	request, err := model.createOpenRouterRequest("z-ai/glm-5.3-flash", message, nil, nil, nil, false)
	if err != nil {
		t.Fatalf("createOpenRouterRequest() error = %v", err)
	}
	if len(request.Plugins) != 1 {
		t.Fatalf("expected one PDF parser plugin, got %d", len(request.Plugins))
	}
	if request.Plugins[0].ID != "file-parser" {
		t.Fatalf("expected file-parser plugin, got %q", request.Plugins[0].ID)
	}
	if request.Plugins[0].PDF == nil || request.Plugins[0].PDF.Engine != DefaultPDFEngine {
		t.Fatalf("expected default PDF engine %q, got %#v", DefaultPDFEngine, request.Plugins[0].PDF)
	}

	content, ok := request.Messages[0].Content.([]ContentPart)
	if !ok {
		t.Fatalf("expected multimodal content parts, got %T", request.Messages[0].Content)
	}
	if len(content) != 2 || content[1].Type != "file" || content[1].File == nil {
		t.Fatalf("expected PDF file content part, got %#v", content)
	}
	if content[1].File.FileData != "https://example.com/report.pdf" {
		t.Fatalf("unexpected PDF URL %q", content[1].File.FileData)
	}
}

func TestCreateOpenRouterRequestSupportsInlinePDFAndEngineOverride(t *testing.T) {
	model := &OpenRouter_Model{
		SupportsPDF: true,
		PDFEngine:   "cloudflare-ai",
	}
	message := models.User_Message{
		Role: "user",
		Content: models.Content{Parts: []models.User_Part{
			{InlineData: &models.InlineData{
				MimeType: "application/pdf",
				Data:     "cGRm",
			}},
		}},
	}

	request, err := model.createOpenRouterRequest("z-ai/glm-5.3-flash", message, nil, nil, nil, false)
	if err != nil {
		t.Fatalf("createOpenRouterRequest() error = %v", err)
	}
	if len(request.Plugins) != 1 || request.Plugins[0].PDF == nil || request.Plugins[0].PDF.Engine != "cloudflare-ai" {
		t.Fatalf("expected cloudflare-ai PDF parser, got %#v", request.Plugins)
	}

	content, ok := request.Messages[0].Content.([]ContentPart)
	if !ok || len(content) != 1 || content[0].File == nil {
		t.Fatalf("expected one inline PDF file content part, got %#v", request.Messages[0].Content)
	}
	if content[0].File.FileData != "data:application/pdf;base64,cGRm" {
		t.Fatalf("unexpected inline PDF data URL %q", content[0].File.FileData)
	}
}
