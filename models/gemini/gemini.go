package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	models "github.com/Desarso/godantic/models"
	"github.com/Desarso/godantic/stores"
	"github.com/joho/godotenv"
)

// Initialize log file
var (
	logFile *os.File
)

func init() {
	// Load .env file if it exists (not present in production)
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}
	var err error
	logFile, err = os.OpenFile("debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalln("Failed to open log file")
	}
}

type Gemini_Model struct {
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt,omitempty"`
}

func (g *Gemini_Model) Model_Request(request models.Model_Request, tools []models.FunctionDeclaration, conversationHistory []stores.Message) (models.Model_Response, error) {
	// Allow request if either User_Message OR Tool_Results are present
	if request.User_Message == nil && request.Tool_Results == nil {
		return models.Model_Response{}, fmt.Errorf("request must contain either user message or tool results")
	}

	// Handle the case where only Tool_Results are provided
	var msg models.User_Message
	if request.User_Message != nil {
		msg = *request.User_Message
	} else {
		// If User_Message is nil, pass an empty User_Message struct.
		// create_gemini_request needs to handle this correctly.
		msg = models.User_Message{}
	}

	modelToUse := g.Model
	if modelToUse == "" {
		modelToUse = "gemini-2.0-flash"
	}
	geminiResponse, err := g.model_request(modelToUse, msg, tools, request.Tool_Results, conversationHistory)
	if err != nil {
		return models.Model_Response{}, err
	}
	return g.gemini_response_to_model_response(geminiResponse)
}

func (g *Gemini_Model) gemini_response_to_model_response(response Gemini_response) (models.Model_Response, error) {
	modelResponse := models.Model_Response{}
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			var modelPart models.Model_Part
			if part.Text != nil && *part.Text != "" {
				modelPart.Text = part.Text
			}
			if part.FunctionCall != nil {
				modelPart.FunctionCall = &models.FunctionCall{
					Name: part.FunctionCall.Name,
					Args: part.FunctionCall.Args,
				}
			}
			modelResponse.Parts = append(modelResponse.Parts, modelPart)
		}
	}
	return modelResponse, nil
}

func convertStream(g *Gemini_Model, geminiResponseChan <-chan Gemini_response, geminiErrChan <-chan error) (<-chan models.Model_Response, <-chan error) {
	modelResponseChan := make(chan models.Model_Response)
	finalErrChan := make(chan error, 1)

	go func() {
		defer close(modelResponseChan)
		defer close(finalErrChan)

		for {
			select {
			case geminiResp, ok := <-geminiResponseChan:
				if !ok {
					// Gemini response channel closed, we're done
					return
				}
				modelResp, err := g.gemini_response_to_model_response(geminiResp)
				if err != nil {
					finalErrChan <- fmt.Errorf("error converting gemini response: %w", err)
					return
				}
				modelResponseChan <- modelResp

			case geminiErr, ok := <-geminiErrChan:
				if ok && geminiErr != nil {
					finalErrChan <- geminiErr
					return
				}
				if !ok {
					geminiErrChan = nil
				}
			}

			// Check if both channels are closed/nil
			if geminiResponseChan == nil && geminiErrChan == nil {
				return
			}
		}
	}()

	return modelResponseChan, finalErrChan
}

func (g *Gemini_Model) Stream_Model_Request(request models.Model_Request, tools []models.FunctionDeclaration, conversationHistory []stores.Message) (<-chan models.Model_Response, <-chan error) {
	// Allow request if either User_Message OR Tool_Results are present
	if request.User_Message == nil && request.Tool_Results == nil {
		errChan := make(chan error, 1)
		respChan := make(chan models.Model_Response)
		errChan <- fmt.Errorf("request must contain either user message or tool results")
		close(errChan)
		close(respChan) // Also close response channel
		return respChan, errChan
	}

	// Handle the case where only Tool_Results are provided
	var msg models.User_Message
	if request.User_Message != nil {
		msg = *request.User_Message
	} else {
		// If User_Message is nil, pass an empty User_Message struct.
		// create_gemini_request needs to handle this correctly.
		msg = models.User_Message{}
	}

	modelToUse := g.Model
	if modelToUse == "" {
		modelToUse = "gemini-2.0-flash"
	}
	// Pass all parts of the request to stream_model_request
	geminiRespChan, geminiErrChan := g.stream_model_request(modelToUse, msg, tools, request.Tool_Results, conversationHistory)
	return convertStream(g, geminiRespChan, geminiErrChan)
}

func (g *Gemini_Model) model_request(model string, message models.User_Message, tools []models.FunctionDeclaration, toolResults *[]models.Tool_Result, conversationHistory []stores.Message) (Gemini_response, error) {
	request_body, err := create_gemini_request(message, tools, toolResults, conversationHistory, g.SystemPrompt)
	if err != nil {
		return Gemini_response{}, fmt.Errorf("failed to create gemini request: %w", err)
	}
	//save request_body to json
	jsonBytes, err := json.Marshal(request_body)
	if err != nil {
		return Gemini_response{}, fmt.Errorf("failed to marshal request body: %w", err)
	}

	if err := os.WriteFile("request_body.json", jsonBytes, 0644); err != nil {
		return Gemini_response{}, fmt.Errorf("failed to write request body to file: %w", err)
	}

	return make_request(string(jsonBytes), model)
}

func (g *Gemini_Model) stream_model_request(model string, message models.User_Message, tools []models.FunctionDeclaration, toolResults *[]models.Tool_Result, conversationHistory []stores.Message) (<-chan Gemini_response, <-chan error) {
	// create_gemini_request now handles potentially empty 'message' if 'toolResults' is present
	request_body, err := create_gemini_request(message, tools, toolResults, conversationHistory, g.SystemPrompt)
	if err != nil {
		errChan := make(chan error, 1)
		errChan <- fmt.Errorf("failed to create gemini stream request body: %w", err)
		close(errChan)
		// Need a response channel to return even on error
		respChan := make(chan Gemini_response)
		close(respChan)
		return respChan, errChan
	}

	jsonBytes, err := json.MarshalIndent(request_body, "", "  ") // Use MarshalIndent for logging
	if err != nil {
		errChan := make(chan error, 1)
		errChan <- fmt.Errorf("failed to marshal stream request body: %w", err)
		close(errChan)
		respChan := make(chan Gemini_response)
		close(respChan)
		return respChan, errChan
	}

	// Log the request body for debugging
	// Request body logging disabled - enable for debugging if needed
	// log.Printf("Gemini Stream Request Body:\n%s", string(jsonBytes))

	// Optional: Write to file for very detailed debugging
	// if err := os.WriteFile("stream_request_body.json", jsonBytes, 0644); err != nil {
	// 	log.Printf("Warning: failed to write stream request body to file: %v", err)
	// }

	return make_request_stream(string(jsonBytes), model)
}

func make_request(request_body string, model string) (Gemini_response, error) {

	resp, err := http.Post(fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, os.Getenv("GEMINI_API_KEY")), "application/json", strings.NewReader(request_body))
	if err != nil {
		fmt.Println("Error:", err)
		return Gemini_response{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading body:", err)
		return Gemini_response{}, err
	}

	var response Gemini_response
	err = json.Unmarshal(body, &response)
	if err != nil {
		fmt.Println("Error unmarshalling response:", err)
		return Gemini_response{}, err
	}

	return response, nil

}

func make_request_stream(request_body string, model string) (<-chan Gemini_response, <-chan error) {
	resChan := make(chan Gemini_response)
	errChan := make(chan error, 1) // Buffered error channel

	go func() {
		defer close(resChan)
		defer close(errChan)

		resp, err := http.Post(fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?key=%s", model, os.Getenv("GEMINI_API_KEY")), "application/json", strings.NewReader(request_body))
		if err != nil {
			errChan <- fmt.Errorf("error making POST request: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body) // Read body for error details
			errChan <- fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(bodyBytes))
			return
		}

		decoder := json.NewDecoder(resp.Body)

		// Read the opening bracket `[`
		t, err := decoder.Token()
		if err != nil {
			errChan <- fmt.Errorf("error reading opening bracket: %w", err)
			return
		}
		if delim, ok := t.(json.Delim); !ok || delim != '[' {
			// Try to read body for context if it's not the expected array start
			remainingBody, _ := io.ReadAll(io.MultiReader(decoder.Buffered(), resp.Body))
			errChan <- fmt.Errorf("expected '[' at start of stream, got %T: %v. Body: %s", t, t, string(remainingBody))
			return
		}

		// Decode each object in the array
		for decoder.More() { // Check if there is another element in the array
			var response Gemini_response
			if err := decoder.Decode(&response); err != nil {
				// Attempt to read the rest of the body to see if there's more context
				remainingBody, readErr := io.ReadAll(decoder.Buffered())
				errMsg := fmt.Sprintf("error decoding JSON object in stream: %v", err)
				if readErr != nil {
					errMsg += fmt.Sprintf(", and error reading remaining buffer: %v", readErr)
				} else if len(remainingBody) > 0 {
					errMsg += fmt.Sprintf(", remaining buffer: %s", string(remainingBody))
				}
				errChan <- fmt.Errorf("%s", errMsg)
				return // Stop processing on decode error
			}
			// Successfully decoded a chunk
			resChan <- response
		}

		// Read the closing bracket `]` - Optional, decoder.More() handles EOF
		t, err = decoder.Token()
		if err != nil && err != io.EOF { // Allow EOF here
			errChan <- fmt.Errorf("error reading closing bracket: %w", err)
			return
		}
		if err != io.EOF {
			if delim, ok := t.(json.Delim); !ok || delim != ']' {
				errChan <- fmt.Errorf("expected ']' at end of stream, got %T: %v", t, t)
				return
			}
		}

	}() // Start the goroutine

	return resChan, errChan
}

func SimplePrompt(prompt string) (Gemini_response, error) {
	request_body := fmt.Sprintf(`{
		"contents": [
			{
				"parts": [
					{
						"text": "%s"
					}
				]
			}
		]
	}`, prompt)
	return make_request(request_body, "gemini-2.0-flash")
}

func StreamPrompt(prompt string) (<-chan Gemini_response, <-chan error) {
	request_body := fmt.Sprintf(`{
		"contents": [
			{
				"parts": [
					{
						"text": "%s"
					}
				]
			}
		]
	}`, prompt)
	return make_request_stream(request_body, "gemini-2.0-flash")
}

func uploadFileFromURLToGemini(fileURL string) (string, error) {
	// 1. Get API Key from Environment
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY environment variable not set")
	}

	// 2. Download the file from the URL
	log.Printf("Attempting to download file from: %s", fileURL)
	downloadResp, err := http.Get(fileURL)
	if err != nil {
		return "", fmt.Errorf("failed to start download from URL %s: %w", fileURL, err)
	}
	defer downloadResp.Body.Close() // Ensure download response body is closed

	if downloadResp.StatusCode != http.StatusOK {
		// Try to read body for more details, but don't fail if read fails
		bodyBytes, _ := io.ReadAll(io.LimitReader(downloadResp.Body, 1024)) // Limit read size
		log.Printf("Download failed. URL: %s, Status: %s, Body Sample: %s", fileURL, downloadResp.Status, string(bodyBytes))
		return "", fmt.Errorf("failed to download file from %s: status code %s", fileURL, downloadResp.Status)
	}
	log.Printf("Successfully initiated download from: %s", fileURL)

	// 3. Get file details from download response headers
	mimeType := downloadResp.Header.Get("Content-Type")
	if mimeType == "" {
		// You might want to default or try to detect, but Gemini API requires it.
		// For simplicity, we'll error out if not provided by the source URL.
		log.Printf("Warning: Content-Type header missing from download response for %s. Upload might fail.", fileURL)
		// return "", fmt.Errorf("could not determine mime type: Content-Type header missing from %s", fileURL)
		// Let's proceed but log a warning, Gemini might guess it, or fail.
	}

	fileSizeStr := downloadResp.Header.Get("Content-Length")
	if fileSizeStr == "" {
		// Resumable upload *requires* Content-Length for the start request.
		// If the server doesn't provide it (e.g., chunked encoding), this won't work.
		// A workaround would be to download to a temp file/memory first, get size, then upload.
		return "", fmt.Errorf("could not determine file size: Content-Length header missing from %s", fileURL)
	}
	fileSize, err := strconv.ParseInt(fileSizeStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("failed to parse Content-Length header '%s': %w", fileSizeStr, err)
	}
	if fileSize <= 0 {
		return "", fmt.Errorf("invalid Content-Length received: %d", fileSize)
	}

	// Extract a display name from the URL path
	displayName := path.Base(fileURL)
	if displayName == "." || displayName == "/" {
		displayName = "downloaded_file" // Provide a default if extraction fails
	}

	log.Printf("File Details - DisplayName: %s, MimeType: %s, Size: %d bytes", displayName, mimeType, fileSize)

	// 4. Initial Resumable Request (start)
	baseURL := "https://generativelanguage.googleapis.com"
	startURL := fmt.Sprintf("%s/upload/v1beta/files?key=%s", baseURL, apiKey)

	startRequestBody := UploadStartRequest{
		File: FileMetadata{DisplayName: displayName},
	}
	startRequestBodyBytes, err := json.Marshal(startRequestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal start request body: %w", err)
	}

	startReq, err := http.NewRequest("POST", startURL, bytes.NewBuffer(startRequestBodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create start request: %w", err)
	}
	startReq.Header.Set("X-Goog-Upload-Protocol", "resumable")
	startReq.Header.Set("X-Goog-Upload-Command", "start")
	startReq.Header.Set("X-Goog-Upload-Header-Content-Length", fmt.Sprintf("%d", fileSize))
	startReq.Header.Set("X-Goog-Upload-Header-Content-Type", mimeType)
	startReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second} // Add a timeout
	log.Printf("Sending Start request to Gemini: %s", startURL)
	startResp, err := client.Do(startReq)
	if err != nil {
		return "", fmt.Errorf("failed to execute start request: %w", err)
	}
	defer startResp.Body.Close()

	if startResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(startResp.Body)
		log.Printf("Start request failed. Status: %s, Body: %s", startResp.Status, string(bodyBytes))
		return "", fmt.Errorf("start request failed with status: %s", startResp.Status)
	}

	uploadURL := startResp.Header.Get("X-Goog-Upload-Url")
	if uploadURL == "" {
		// Read body for potential error details if URL is missing
		bodyBytes, _ := io.ReadAll(startResp.Body)
		log.Printf("Start request succeeded (Status OK) but did not return upload URL. Body: %s", string(bodyBytes))
		return "", fmt.Errorf("did not receive upload URL from start request")
	}
	log.Printf("Received resumable upload URL: %s", uploadURL)

	// 5. Upload Bytes (upload, finalize)
	// Use the response body from the download GET request directly.
	// No need to re-open or read into memory unless Content-Length was missing earlier.
	uploadReq, err := http.NewRequest("POST", uploadURL, downloadResp.Body) // Use the download response body
	if err != nil {
		return "", fmt.Errorf("failed to create upload request: %w", err)
	}
	// Set required headers for the upload chunk
	uploadReq.Header.Set("X-Goog-Upload-Offset", "0")
	uploadReq.Header.Set("X-Goog-Upload-Command", "upload, finalize")
	// Let the http client set Content-Length based on the body size,
	// but explicitly setting it is also fine if needed (and safer).
	uploadReq.ContentLength = fileSize // Explicitly set ContentLength
	// Note: Content-Type for the *upload* request itself might not be needed here
	// as we set X-Goog-Upload-Header-Content-Type in the start request,
	// and the body is the raw file bytes. The Go client might set a default
	// like application/octet-stream if Content-Type is not set on the request
	// when a body is present. Check Gemini docs if a specific Content-Type
	// is required for this POST request itself. Let's omit it for now.

	// Use a client with a potentially longer timeout for the actual upload
	uploadClient := &http.Client{Timeout: 5 * time.Minute} // Adjust timeout as needed for large files
	log.Printf("Sending Upload request to Gemini: %s", uploadURL)
	uploadResp, err := uploadClient.Do(uploadReq)
	if err != nil {
		return "", fmt.Errorf("failed to execute upload request: %w", err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(uploadResp.Body)
		log.Printf("Upload request failed. Status: %s, Body: %s", uploadResp.Status, string(bodyBytes))
		return "", fmt.Errorf("upload request failed with status: %s", uploadResp.Status)
	}
	log.Printf("Upload request successful (Status: %s)", uploadResp.Status)

	// 6. Parse final response for file URI
	var fileInfoResponse FileInfoResponse
	if err := json.NewDecoder(uploadResp.Body).Decode(&fileInfoResponse); err != nil {
		// It's possible the body was empty on success, or not JSON.
		// Read remaining body for debugging.
		bodyBytes, _ := io.ReadAll(uploadResp.Body) // Try reading again
		log.Printf("Failed to decode JSON response after successful upload. Body: %s", string(bodyBytes))
		return "", fmt.Errorf("failed to decode file info response: %w", err)
	}

	if fileInfoResponse.File.URI == "" {
		log.Printf("Upload response JSON decoded, but File URI is missing. Response: %+v", fileInfoResponse)
		return "", fmt.Errorf("file URI not found in final response")
	}

	log.Printf("Successfully uploaded file. Gemini URI: %s", fileInfoResponse.File.URI)
	return fileInfoResponse.File.URI, nil
}

func lessThan2mb(url string) bool {
	resp, err := http.Head(url) // Use http.Head instead of http.Get
	if err != nil {
		return false
	}
	defer resp.Body.Close() // Close the response body even though it should be empty for HEAD
	return resp.ContentLength > 0 && resp.ContentLength < 2*1024*1024
}

func getInlineData(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error fetching file for inline data from %s: %v", url, err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Error fetching file for inline data from %s: status %s", url, resp.Status)
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body for inline data from %s: %v", url, err)
		return ""
	}

	// Encode the body bytes to base64
	encodedData := base64.StdEncoding.EncodeToString(body)
	return encodedData
}

// functio that turns User_Message to something gemini likes
func create_gemini_request(message models.User_Message, tools []models.FunctionDeclaration, toolResults *[]models.Tool_Result, conversationHistory []stores.Message, systemPrompt string) (Gemini_Request_Body, error) {

	allContents := []Gemini_Content{}

	// 1. Process conversation history
	for _, histMsg := range conversationHistory {
		role := histMsg.Role // Use the role directly from history
		var historyParts []Request_Part

		// Unmarshal PartsJSON based on Role and Type
		if histMsg.PartsJSON != "" && histMsg.PartsJSON != "{}" && histMsg.PartsJSON != "null" {
			// Determine the target type for unmarshalling parts based on role/type
			if role == "user" {
				var userParts []models.User_Part
				if err := json.Unmarshal([]byte(histMsg.PartsJSON), &userParts); err != nil {
					log.Printf("Warning: Failed to unmarshal PartsJSON for user history message %d: %v. Content: %s", histMsg.ID, err, histMsg.PartsJSON)
					// Decide how to handle error - skip message? create error part?
					continue // Skip this history message if unmarshalling fails
				}
				// Convert []models.User_Part to []Request_Part
				historyParts = make([]Request_Part, len(userParts))
				for i, p := range userParts {
					// Manual conversion for InlineData
					var inlineDataPart *InlineData
					if p.InlineData != nil {
						inlineDataPart = &InlineData{
							MimeType: p.InlineData.MimeType,
							Data:     p.InlineData.Data,
						}
					}
					// Manual conversion for FileData
					var fileDataPart *FileData
					if p.FileData != nil {
						fileDataPart = &FileData{
							MimeType: p.FileData.MimeType,
							URI:      "", // URI is generated/used internally by Gemini request logic, not directly from User_Part FileData which has FileUrl/GoogleUri
						}
						// Note: The logic below this history loop handles FileUrl/GoogleUri from the *current* message.
						// History reconstruction primarily needs the MimeType if FileData existed.
					}

					historyParts[i] = Request_Part{
						Text:             p.Text,
						InlineData:       inlineDataPart, // Use converted struct
						FileData:         fileDataPart,   // Use converted struct
						FunctionResponse: p.FunctionResponse,
					}
				}
			} else if role == "model" {
				var modelParts []models.Model_Part
				if err := json.Unmarshal([]byte(histMsg.PartsJSON), &modelParts); err != nil {
					log.Printf("Warning: Failed to unmarshal PartsJSON for model history message %d: %v. Content: %s", histMsg.ID, err, histMsg.PartsJSON)
					continue // Skip this history message
				}
				// Convert []models.Model_Part to []Request_Part
				historyParts = make([]Request_Part, len(modelParts))
				for i, p := range modelParts {
					// Handle potential nil pointer for Text
					var textContent string
					if p.Text != nil {
						textContent = *p.Text
					}
					historyParts[i] = Request_Part{
						Text:         textContent,
						FunctionCall: p.FunctionCall, // Assuming FunctionCall struct matches
					}
				}
			} else {
				log.Printf("Warning: Unknown role '%s' for history message %d. Cannot unmarshal parts.", role, histMsg.ID)
				continue
			}
		} else {
			// Handle cases where PartsJSON might be empty/null but perhaps shouldn't be based on Type?
			log.Printf("Warning: History message %d (Role: %s, Type: %s) has empty/null PartsJSON.", histMsg.ID, role, histMsg.Type)
			// Potentially skip or handle based on Type
			continue
		}

		// Add the content object for this history message if it has parts
		if len(historyParts) > 0 {
			allContents = append(allContents, Gemini_Content{
				Role:  role,
				Parts: historyParts, // Use the unmarshaled and converted parts
			})
		} else {
			log.Printf("Warning: No parts generated after unmarshalling history message %d (Role: %s, Type: %s). Skipping.", histMsg.ID, role, histMsg.Type)
		}
	}

	// 2. Handle tool results provided for the *current* turn
	if toolResults != nil && len(*toolResults) > 0 {
		// Unroll tool results into individual user messages
		for _, tr := range *toolResults {
			var respMap map[string]interface{}
			if err := json.Unmarshal([]byte(tr.Tool_Output), &respMap); err != nil {
				log.Printf("Warning: Failed to unmarshal tool output for %s as JSON: %v. Wrapping as {\"output\": ...}.", tr.Tool_Name, err)
				// Wrap non-JSON output in a standard structure
				respMap = map[string]interface{}{"output": tr.Tool_Output}
			}
			// Create a single part for this function response
			toolResponsePart := Request_Part{FunctionResponse: &models.FunctionResponse{ID: tr.Tool_ID, Name: tr.Tool_Name, Response: respMap}}
			// Create a separate content block for this single part
			allContents = append(allContents, Gemini_Content{
				Role:  "user", // Function responses always get the 'user' role
				Parts: []Request_Part{toolResponsePart},
			})
		}
	} else {
		// 3. Process the current user message ONLY if NO tool results were provided for this turn
		currentUserParts := []Request_Part{}
		if message.Content.Parts != nil {
			for _, part := range message.Content.Parts {
				if part.FunctionResponse != nil {
					log.Printf("Warning: Skipping FunctionResponse found in input User_Message parts; should be handled via toolResults or history.")
					continue
				}

				// Process Text, FileData, InlineData (existing logic)
				if part.Text != "" {
					currentUserParts = append(currentUserParts, Request_Part{Text: part.Text})
				} else if part.FileData != nil {
					var uri string
					var err error
					if part.FileData.GoogleUri == nil || *part.FileData.GoogleUri == "" {
						if lessThan2mb(part.FileData.FileUrl) {
							inline := getInlineData(part.FileData.FileUrl)
							if inline != "" {
								log.Printf("Inlining data for %s (less than 2MB)", part.FileData.FileUrl)
								currentUserParts = append(currentUserParts, Request_Part{InlineData: &InlineData{MimeType: part.FileData.MimeType, Data: inline}})
							} else {
								log.Printf("Failed to get inline data for %s, attempting upload.", part.FileData.FileUrl)
								uri, err = uploadFileFromURLToGemini(part.FileData.FileUrl)
								if err != nil {
									return Gemini_Request_Body{}, fmt.Errorf("failed to upload file %s after failed inline attempt: %w", part.FileData.FileUrl, err)
								}
								log.Printf("Uploaded file %s to %s", part.FileData.FileUrl, uri)
								currentUserParts = append(currentUserParts, Request_Part{FileData: &FileData{URI: uri, MimeType: part.FileData.MimeType}})
							}
						} else {
							uri, err = uploadFileFromURLToGemini(part.FileData.FileUrl)
							if err != nil {
								return Gemini_Request_Body{}, fmt.Errorf("failed to upload file %s to gemini: %w", part.FileData.FileUrl, err)
							}
							log.Printf("Uploaded file %s to %s", part.FileData.FileUrl, uri)
							currentUserParts = append(currentUserParts, Request_Part{FileData: &FileData{URI: uri, MimeType: part.FileData.MimeType}})
						}
					} else {
						uri = *part.FileData.GoogleUri
						log.Printf("Using provided Google URI %s for file %s", uri, part.FileData.FileUrl)
						currentUserParts = append(currentUserParts, Request_Part{FileData: &FileData{URI: uri, MimeType: part.FileData.MimeType}})
					}
				} else if part.InlineData != nil {
					log.Printf("Adding inline data part (MimeType: %s)", part.InlineData.MimeType)
					currentUserParts = append(currentUserParts, Request_Part{InlineData: &InlineData{MimeType: part.InlineData.MimeType, Data: part.InlineData.Data}})
				}
			}
		}

		// Add the user message content ONLY if currentUserParts were generated
		if len(currentUserParts) > 0 {
			allContents = append(allContents, Gemini_Content{
				Role:  "user",
				Parts: currentUserParts,
			})
		}
	}

	// Check if we have anything to send at all (history, tool results, or user message)
	if len(allContents) == 0 {
		return Gemini_Request_Body{}, fmt.Errorf("cannot create Gemini request with no content (history, tool results, or user message)")
	}

	// 4. Prepare tools
	gemini_tools := []Gemini_Tools{}
	if len(tools) > 0 {
		gemini_tools = append(gemini_tools, Gemini_Tools{FunctionDeclarations: tools})
	}

	// 5. Construct the final request body with system instruction
	var systemInstruction *SystemInstruction
	if systemPrompt != "" {
		systemInstruction = &SystemInstruction{
			Parts: []SystemPart{{Text: systemPrompt}},
		}
	}

	request_body := Gemini_Request_Body{
		Contents:          &allContents,
		Tools:             &gemini_tools,
		SystemInstruction: systemInstruction,
	}

	return request_body, nil
}
