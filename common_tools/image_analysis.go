package common_tools

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/Desarso/godantic/models"
)

// ImageAnalysisConfig holds config for the image analysis tool.
type ImageAnalysisConfig struct {
	// Model is a vision-capable godantic Model for analyzing images.
	Model ImageAnalyzer
	// DefaultPrompt is used when no prompt is provided.
	DefaultPrompt string
}

// ImageAnalyzer is the interface for sending vision requests.
// Matches godantic Model interface signature for Model_Request.
type ImageAnalyzer interface {
	Model_Request(request models.Model_Request, tools []models.FunctionDeclaration, history []interface{}) (models.Model_Response, error)
}

var imageConfig *ImageAnalysisConfig

// SetImageAnalysisConfig wires the vision model at init time.
func SetImageAnalysisConfig(cfg *ImageAnalysisConfig) {
	imageConfig = cfg
}

// imageAnalyzeFunc is the pluggable function for actual analysis (mockable for tests).
var imageAnalyzeFunc = defaultImageAnalyze

// SetImageAnalyzeFunc allows overriding the analysis function for testing.
func SetImageAnalyzeFunc(fn func(imageURL, base64Data, mediaType, prompt string) (string, error)) {
	imageAnalyzeFunc = fn
}

func defaultImageAnalyze(imageURL, base64Data, mediaType, prompt string) (string, error) {
	return "", fmt.Errorf("image analysis not configured; wire up a vision model via SetImageAnalysisConfig")
}

// ImageAnalysisTool returns a FunctionDeclaration for image analysis.
func ImageAnalysisTool() models.FunctionDeclaration {
	return models.FunctionDeclaration{
		Name:        "image_analysis",
		Description: "Analyze an image using a vision-capable LLM. Accepts image URL or base64-encoded data. Returns a description or analysis based on the prompt.",
		Parameters: models.Parameters{
			Type: "object",
			Properties: map[string]interface{}{
				"image_url": map[string]interface{}{
					"type":        "string",
					"description": "HTTP/HTTPS URL of the image to analyze",
				},
				"base64_data": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded image data (without data: prefix)",
				},
				"media_type": map[string]interface{}{
					"type":        "string",
					"description": "MIME type of the image (e.g. image/png, image/jpeg). Required with base64_data.",
				},
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "What to analyze or describe about the image. Default: 'Describe this image in detail.'",
				},
			},
			Required: []string{},
		},
		Callable: AnalyzeImage,
	}
}

// AnalyzeImage processes an image via vision LLM.
func AnalyzeImage(imageURL, base64Data, mediaType, prompt string) (string, error) {
	if imageURL == "" && base64Data == "" {
		return "", fmt.Errorf("either image_url or base64_data is required")
	}

	if prompt == "" {
		prompt = "Describe this image in detail."
	}

	// Validate base64 data if provided
	if base64Data != "" {
		// Strip data: URI prefix if present
		if strings.HasPrefix(base64Data, "data:") {
			parts := strings.SplitN(base64Data, ",", 2)
			if len(parts) == 2 {
				// Extract media type from data URI
				typePart := strings.TrimPrefix(parts[0], "data:")
				typePart = strings.TrimSuffix(typePart, ";base64")
				if mediaType == "" {
					mediaType = typePart
				}
				base64Data = parts[1]
			}
		}
		if mediaType == "" {
			mediaType = detectMediaType(base64Data)
		}
		// Validate it's actual base64
		if _, err := base64.StdEncoding.DecodeString(base64Data); err != nil {
			// Try URL-safe base64
			if _, err2 := base64.URLEncoding.DecodeString(base64Data); err2 != nil {
				return "", fmt.Errorf("invalid base64 data: %w", err)
			}
		}
	}

	if imageURL != "" && !strings.HasPrefix(imageURL, "http://") && !strings.HasPrefix(imageURL, "https://") {
		return "", fmt.Errorf("image_url must be an HTTP or HTTPS URL")
	}

	return imageAnalyzeFunc(imageURL, base64Data, mediaType, prompt)
}

// detectMediaType tries to detect the media type from base64-decoded magic bytes.
func detectMediaType(b64 string) string {
	data, err := base64.StdEncoding.DecodeString(b64[:min(len(b64), 100)])
	if err != nil {
		return "image/png" // fallback
	}
	ct := http.DetectContentType(data)
	if strings.HasPrefix(ct, "image/") {
		return ct
	}
	return "image/png"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
