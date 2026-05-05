package common_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/genai"
)

//go:generate ../../gen_schema -func=Generate_Image -file=generate_image.go -out=../schemas/cached_schemas

// Generate_Image generates an image using Google's Imagen image generation model.
// The generated image is automatically displayed in the UI - do NOT include the image URL in your response to avoid showing duplicate images.
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

	config := &genai.GenerateImagesConfig{
		NumberOfImages: 1,
	}

	result, err := client.Models.GenerateImages(
		ctx,
		"imagen-4.0-generate-001",
		prompt,
		config,
	)
	if err != nil {
		return "", fmt.Errorf("failed to generate image: %w", err)
	}

	if len(result.GeneratedImages) == 0 {
		return "", fmt.Errorf("no image generated in response")
	}

	imageBytes := result.GeneratedImages[0].Image.ImageBytes
	if len(imageBytes) == 0 {
		return "", fmt.Errorf("generated image contained no bytes")
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("generated_image_%s.png", timestamp)

	imagesDir := "images"
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create images directory: %w", err)
	}

	filePath := filepath.Join(imagesDir, filename)
	if err := os.WriteFile(filePath, imageBytes, 0644); err != nil {
		return "", fmt.Errorf("failed to save image: %w", err)
	}

	serverHost := os.Getenv("SERVER_HOST")
	if serverHost == "" {
		serverHost = "http://localhost:8080"
	}

	imageURL := fmt.Sprintf("%s/images/%s", serverHost, filename)
	response := map[string]string{
		"status":    "success",
		"prompt":    prompt,
		"image_url": imageURL,
		"filename":  filename,
		"message":   "Image generated successfully and is already displayed in the UI. Do not repeat the image markdown or URL in your response.",
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("failed to encode image response: %w", err)
	}
	return string(responseJSON), nil
}
