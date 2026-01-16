package common_tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/genai"
)

//go:generate ../../gen_schema -func=Generate_Image -file=generate_image.go -out=../schemas/cached_schemas

// Generate_Image generates an image using Gemini's image generation model
// Saves the image to a file and returns a markdown formatted URL
func Generate_Image(prompt string) (string, error) {
	// If prompt is empty, use "nano banana" as default
	if strings.TrimSpace(prompt) == "" {
		prompt = "a nano banana"
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create Gemini client: %w", err)
	}

	// Use Gemini 2.5 Flash Image model for image generation
	result, err := client.Models.GenerateContent(
		ctx,
		"gemini-2.5-flash-image",
		genai.Text(prompt),
		nil, // config
	)
	if err != nil {
		return "", fmt.Errorf("failed to generate image: %w", err)
	}

	// Check if we have candidates
	if len(result.Candidates) == 0 || result.Candidates[0].Content == nil {
		return "", fmt.Errorf("no image generated in response")
	}

	// Look through parts for image data
	for _, part := range result.Candidates[0].Content.Parts {
		// Check for text part
		if part.Text != "" {
			// Skip text parts
			continue
		}

		// Check for inline image data
		if part.InlineData != nil {
			imageBytes := part.InlineData.Data
			mimeType := part.InlineData.MIMEType

			// Determine file extension from MIME type
			extension := "png"
			if strings.Contains(mimeType, "jpeg") || strings.Contains(mimeType, "jpg") {
				extension = "jpg"
			} else if strings.Contains(mimeType, "webp") {
				extension = "webp"
			}

			// Generate unique filename with timestamp
			timestamp := time.Now().Format("20060102_150405")
			filename := fmt.Sprintf("generated_image_%s.%s", timestamp, extension)

			// Create images directory if it doesn't exist
			imagesDir := "images"
			if err := os.MkdirAll(imagesDir, 0755); err != nil {
				return "", fmt.Errorf("failed to create images directory: %w", err)
			}

			// Save to images directory
			filePath := filepath.Join(imagesDir, filename)

			err = os.WriteFile(filePath, imageBytes, 0644)
			if err != nil {
				return "", fmt.Errorf("failed to save image: %w", err)
			}

			// Get server host from environment variable, default to localhost:8080
			serverHost := os.Getenv("SERVER_HOST")
			if serverHost == "" {
				serverHost = "http://localhost:8080"
			}

			// Return markdown formatted image with full URL
			imageURL := fmt.Sprintf("%s/images/%s", serverHost, filename)
			return fmt.Sprintf("![Generated: %s](%s)\n\nImage generated successfully for prompt: \"%s\"", prompt, imageURL, prompt), nil
		}
	}

	return "", fmt.Errorf("no image data found in response")
}
