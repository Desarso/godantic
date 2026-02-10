package common_tools

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestAnalyzeImage_RequiresInput(t *testing.T) {
	_, err := AnalyzeImage("", "", "", "")
	if err == nil {
		t.Fatal("expected error for empty inputs")
	}
	if !strings.Contains(err.Error(), "either image_url or base64_data is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnalyzeImage_InvalidURL(t *testing.T) {
	_, err := AnalyzeImage("ftp://bad", "", "", "describe")
	if err == nil || !strings.Contains(err.Error(), "HTTP or HTTPS") {
		t.Fatalf("expected URL validation error, got: %v", err)
	}
}

func TestAnalyzeImage_InvalidBase64(t *testing.T) {
	_, err := AnalyzeImage("", "not-valid-base64!!!", "image/png", "describe")
	if err == nil || !strings.Contains(err.Error(), "invalid base64") {
		t.Fatalf("expected base64 error, got: %v", err)
	}
}

func TestAnalyzeImage_ValidBase64(t *testing.T) {
	called := false
	SetImageAnalyzeFunc(func(url, b64, mt, prompt string) (string, error) {
		called = true
		if url != "" {
			t.Error("expected empty url")
		}
		if mt != "image/jpeg" {
			t.Errorf("expected image/jpeg, got %s", mt)
		}
		if prompt != "What is this?" {
			t.Errorf("unexpected prompt: %s", prompt)
		}
		return "A cat", nil
	})
	defer SetImageAnalyzeFunc(defaultImageAnalyze)

	// Valid base64 (a few bytes)
	data := base64.StdEncoding.EncodeToString([]byte{0xFF, 0xD8, 0xFF, 0xE0}) // JPEG magic
	result, err := AnalyzeImage("", data, "image/jpeg", "What is this?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "A cat" {
		t.Errorf("unexpected result: %s", result)
	}
	if !called {
		t.Error("analyze func was not called")
	}
}

func TestAnalyzeImage_ValidURL(t *testing.T) {
	called := false
	SetImageAnalyzeFunc(func(url, b64, mt, prompt string) (string, error) {
		called = true
		if url != "https://example.com/image.png" {
			t.Errorf("unexpected url: %s", url)
		}
		if prompt != "Describe this image in detail." {
			t.Errorf("unexpected default prompt: %s", prompt)
		}
		return "A landscape", nil
	})
	defer SetImageAnalyzeFunc(defaultImageAnalyze)

	result, err := AnalyzeImage("https://example.com/image.png", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "A landscape" {
		t.Errorf("unexpected result: %s", result)
	}
	if !called {
		t.Error("analyze func was not called")
	}
}

func TestAnalyzeImage_DataURIStripping(t *testing.T) {
	SetImageAnalyzeFunc(func(url, b64, mt, prompt string) (string, error) {
		if mt != "image/png" {
			t.Errorf("expected image/png from data URI, got %s", mt)
		}
		// b64 should be just the data part, not the full data URI
		if strings.HasPrefix(b64, "data:") {
			t.Error("data: prefix should have been stripped")
		}
		return "ok", nil
	})
	defer SetImageAnalyzeFunc(defaultImageAnalyze)

	raw := base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4E, 0x47}) // PNG magic
	dataURI := "data:image/png;base64," + raw
	_, err := AnalyzeImage("", dataURI, "", "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImageAnalysisTool_Registration(t *testing.T) {
	tool := ImageAnalysisTool()
	if tool.Name != "image_analysis" {
		t.Errorf("unexpected name: %s", tool.Name)
	}
	if tool.Callable == nil {
		t.Error("Callable should not be nil")
	}
}

func TestDetectMediaType(t *testing.T) {
	// JPEG magic bytes
	jpegData := base64.StdEncoding.EncodeToString([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10})
	mt := detectMediaType(jpegData)
	// http.DetectContentType should detect this
	if !strings.HasPrefix(mt, "image/") {
		t.Errorf("expected image/* type, got %s", mt)
	}
}
