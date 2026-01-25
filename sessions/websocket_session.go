package sessions

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Desarso/godantic/models"
	"github.com/google/uuid"

	eleven_tts "github.com/Desarso/godantic/elevenlabs/tts/multi"
)

// functionCallInfo holds information about a function call
type functionCallInfo struct {
	Name       string
	Args       map[string]interface{}
	ID         string
	ArgsJSON   string
	TextInPart *string
}

// RunInteraction handles the complete agent interaction loop.
// Kept for backward compatibility; prefer RunInteractionWithContext so callers can cancel in-flight streams.
func (as *AgentSession) RunInteraction(req models.Model_Request) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	return as.RunInteractionWithContext(ctx, req)
}

// RunInteractionWithContext runs a single interaction that can be cancelled via ctx.
// Important:
// - We keep the ElevenLabs websocket alive across turns (for low overhead),
// - BUT we create a fresh ElevenLabs context_id per turn so every response reliably produces audio.
func (as *AgentSession) RunInteractionWithContext(ctx context.Context, req models.Model_Request) error {

	// Keep the input mode stable across tool follow-ups.
	inputMode := req.Input_Mode
	if inputMode == "" {
		inputMode = "text"
	}
	// Keep the language stable across tool follow-ups (tool requests omit language_code).
	voiceLanguageCode := req.Language_Code

	currentReq := req

	for {
		if currentReq.Input_Mode == "" {
			currentReq.Input_Mode = inputMode
		}
		if currentReq.Language_Code == "" && voiceLanguageCode != "" {
			currentReq.Language_Code = voiceLanguageCode
		} else if currentReq.Language_Code != "" {
			voiceLanguageCode = currentReq.Language_Code
		}

		// Fetch latest history
		if err := as.fetchHistory(); err != nil {
			return as.sendError("Failed to fetch history", false)
		}

		// Save user message or tool results if present
		if err := as.saveIncomingMessage(currentReq); err != nil {
			as.Logger.Printf("Error saving incoming message: %v", err)
		}

		// Enable TTS streaming only for voice interactions.
		if strings.EqualFold(inputMode, "voice") {
			if err := as.ensureTTS(ctx, voiceLanguageCode); err != nil {
				as.Logger.Printf("TTS init error: %v", err)
				_ = as.Writer.WriteResponse(map[string]any{"type": "tts_error", "error": err.Error()})
				// Do not fail the interaction if TTS fails; continue with text-only.
				as.shutdownTTS(ctx)
			} else if as.ttsClient != nil && as.ttsContextID != "" {
				// IMPORTANT: ElevenLabs enforces a max number of contexts per websocket connection.
				// Keep ONE context per session and reuse it across turns; just reset our local buffer and
				// let flush at end-of-turn trigger audio for that turn.
				as.ttsPending.Reset()
				_ = as.Writer.WriteResponse(map[string]any{
					"type":       "tts_context_started",
					"context_id": as.ttsContextID,
					"format":     as.ttsFormat,
				})
			}
		}

		// Run agent stream - now we can pass history directly since types match
		resChan, errChan := as.Agent.Run_Stream(currentReq, as.History)

		// Process stream and accumulate parts
		accumulatedParts, err := as.processStream(ctx, resChan, errChan)
		if err != nil {
			return err
		}

		// Process accumulated parts for tools and text
		toolResults, executed, err := as.processAccumulatedParts(accumulatedParts)
		if err != nil {
			return err
		}

		if !executed {
			// No tools executed, interaction complete
			break
		}

		// Prepare for next iteration with tool results
		currentReq = models.Model_Request{
			User_Message: nil,
			Tool_Results: &toolResults,
		}
	}

	// If the client cancelled, don't send "done" (and don't flush TTS).
	if ctx.Err() != nil {
		return nil
	}

	// Important UX detail:
	// Send "done" immediately so the frontend stops showing the typing indicator,
	// and let the long-lived TTS forwarder deliver audio chunks asynchronously.
	if err := as.Writer.WriteDone(); err != nil {
		return err
	}

	// Critical: ElevenLabs won't necessarily emit audio until we flush the context.
	// Previously this happened inline (and blocked the next turn). Now we flush async
	// so the chat loop can accept the next request immediately, while audio continues streaming.
	if as.ttsClient != nil && as.ttsContextID != "" {
		as.flushTTSAsync(as.ttsContextID)
	}

	return nil
}

// fetchHistory retrieves the latest conversation history
func (as *AgentSession) fetchHistory() error {
	history, err := as.Store.FetchHistory(as.SessionID)
	if err != nil {
		as.Logger.Printf("Error fetching history: %v", err)
		return &AgentError{Message: "Failed to fetch history", Fatal: false}
	}
	as.History = history
	return nil
}

// saveIncomingMessage saves user messages or tool results to the database
func (as *AgentSession) saveIncomingMessage(req models.Model_Request) error {
	if req.User_Message != nil {
		return as.saveUserMessage(req.User_Message)
	} else if req.Tool_Results != nil {
		return as.saveToolResults(*req.Tool_Results)
	}
	return nil
}

// saveUserMessage saves a user message to the database
func (as *AgentSession) saveUserMessage(userMsg *models.User_Message) error {
	userPartsToSave := make([]models.User_Part, 0)
	userText := ""

	if userMsg.Content.Parts != nil {
		for _, part := range userMsg.Content.Parts {
			userPartsToSave = append(userPartsToSave, part)
			if part.Text != "" {
				userText += part.Text
			}
		}
	}

	// Legacy support: if no parts but text exists, create text part
	if len(userPartsToSave) == 0 && userText != "" {
		as.Logger.Printf("Warning: User message had text but no parts structure; creating text part.")
		userPartsToSave = append(userPartsToSave, models.User_Part{Text: userText})
	}

	return as.Store.SaveMessage(as.SessionID, "user", "user_message", userPartsToSave, "")
}

// saveToolResults saves tool results to the database
func (as *AgentSession) saveToolResults(toolResults []models.Tool_Result) error {
	toolResponseParts := make([]models.User_Part, 0, len(toolResults))

	for _, toolResult := range toolResults {
		var resultMap map[string]interface{}
		if err := json.Unmarshal([]byte(toolResult.Tool_Output), &resultMap); err != nil {
			as.Logger.Printf("Failed to unmarshal tool output for DB saving (%s), storing raw: %v", toolResult.Tool_Name, err)
			resultMap = map[string]interface{}{"raw_output": toolResult.Tool_Output}
		}

		part := models.User_Part{
			FunctionResponse: &models.FunctionResponse{
				ID:       toolResult.Tool_ID,
				Name:     toolResult.Tool_Name,
				Response: resultMap,
			},
		}
		toolResponseParts = append(toolResponseParts, part)
	}

	if len(toolResponseParts) > 0 {
		return as.Store.SaveMessage(as.SessionID, "user", "function_response", toolResponseParts, "")
	}
	return nil
}

// processStream handles the agent stream processing
func (as *AgentSession) processStream(ctx context.Context, resChan <-chan models.Model_Response, errChan <-chan error) ([]models.Model_Part, error) {
	var accumulated []models.Model_Part

	for {
		// Priority: if a model chunk is ready, handle it first so text stays responsive.
		select {
		case <-ctx.Done():
			return accumulated, nil
		case chunk, ok := <-resChan:
			if !ok {
				as.Logger.Printf("Stream finished normally")
				return accumulated, nil
			}
			accumulated = append(accumulated, chunk.Parts...)
			if err := as.Writer.WriteResponse(chunk); err != nil {
				as.Logger.Printf("Error writing stream chunk: %v", err)
				return nil, &AgentError{Message: "Error writing stream chunk", Fatal: true}
			}

			if as.ttsClient != nil {
				for _, p := range chunk.Parts {
					if p.Text != nil && *p.Text != "" {
						as.ttsHandleDelta(ctx, *p.Text)
					}
				}
			}
			continue
		default:
		}

		select {
		case <-ctx.Done():
			return accumulated, nil
		case chunk, ok := <-resChan:
			if !ok {
				as.Logger.Printf("Stream finished normally")
				return accumulated, nil
			}
			accumulated = append(accumulated, chunk.Parts...)
			if err := as.Writer.WriteResponse(chunk); err != nil {
				as.Logger.Printf("Error writing stream chunk: %v", err)
				return nil, &AgentError{Message: "Error writing stream chunk", Fatal: true}
			}

			if as.ttsClient != nil {
				for _, p := range chunk.Parts {
					if p.Text != nil && *p.Text != "" {
						as.ttsHandleDelta(ctx, *p.Text)
					}
				}
			}

		case streamErr, ok := <-errChan:
			if ok && streamErr != nil {
				as.Logger.Printf("Stream error: %v", streamErr)
				as.Writer.WriteError("Agent stream error: " + streamErr.Error())
				return nil, &AgentError{Message: "Agent stream error", Fatal: false}
			}
			if !ok {
				errChan = nil
			}

		}

		if resChan == nil && errChan == nil {
			as.Logger.Printf("Both agent stream channels closed unexpectedly")
			return accumulated, nil
		}
	}
}

func (as *AgentSession) ensureTTS(ctx context.Context, languageCode string) error {
	lang := strings.ToLower(strings.TrimSpace(languageCode))

	// Pick voice based on language. Defaults:
	// - EN: ZoiZ8fuDWInAcwPXaVeq
	// - ES: p5EUznrYaWnafKvUkNiR
	voiceID := ""
	if strings.HasPrefix(lang, "es") {
		voiceID = os.Getenv("ELEVEN_LABS_TTS_VOICE_ID_ES")
		if voiceID == "" {
			voiceID = os.Getenv("ELEVENLABS_TTS_VOICE_ID_ES")
		}
		if voiceID == "" {
			voiceID = "p5EUznrYaWnafKvUkNiR"
		}
	} else {
		voiceID = os.Getenv("ELEVEN_LABS_TTS_VOICE_ID")
		if voiceID == "" {
			voiceID = os.Getenv("ELEVENLABS_TTS_VOICE_ID")
		}
		if voiceID == "" {
			voiceID = "ZoiZ8fuDWInAcwPXaVeq"
		}
	}

	apiKey := os.Getenv("ELEVEN_LABS_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("ELEVENLABS_API_KEY")
	}
	if apiKey == "" {
		return nil // not configured; silently skip
	}

	// If TTS is already initialized with a different voice, restart it so we can switch languages mid-session.
	if as.ttsClient != nil {
		if as.ttsVoiceID == voiceID {
			// Ensure forwarder is running.
			as.ensureTTSForwarder()
			return nil
		}
		as.shutdownTTS(ctx)
	}

	modelID := os.Getenv("ELEVEN_LABS_TTS_MODEL_ID")
	if modelID == "" {
		modelID = os.Getenv("ELEVENLABS_TTS_MODEL_ID")
	}
	if modelID == "" {
		modelID = "eleven_flash_v2_5"
	}

	outputFormat := os.Getenv("ELEVEN_LABS_TTS_OUTPUT_FORMAT")
	if outputFormat == "" {
		outputFormat = os.Getenv("ELEVENLABS_TTS_OUTPUT_FORMAT")
	}
	if outputFormat == "" {
		outputFormat = "mp3_44100_128"
	}
	as.ttsFormat = outputFormat

	baseURL := os.Getenv("ELEVEN_LABS_BASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("ELEVENLABS_BASE_URL")
	}

	cfg := eleven_tts.ConnectConfig{
		BaseURL:      baseURL,
		VoiceID:      voiceID,
		APIKey:       apiKey,
		ModelID:      modelID,
		OutputFormat: outputFormat,
	}

	// IMPORTANT: Dial ElevenLabs with a session-lifetime context.
	// The per-interaction ctx is cancelled right after the turn finishes (and on barge-in),
	// which would kill the TTS client's read/write loops and result in 0 audio chunks.
	if as.ttsConnCtx == nil {
		as.ttsConnCtx, as.ttsConnCancel = context.WithCancel(context.Background())
	}
	c, err := eleven_tts.Dial(as.ttsConnCtx, cfg, http.Header{})
	if err != nil {
		return err
	}
	as.ttsClient = c
	as.ttsContextID = uuid.NewString()
	as.ttsPending.Reset()
	as.ttsVoiceID = voiceID
	as.ensureTTSForwarder()

	// Initialize the single long-lived context once per session.
	_ = as.Writer.WriteResponse(map[string]any{
		"type":       "tts_context_started",
		"context_id": as.ttsContextID,
		"format":     as.ttsFormat,
	})
	return as.ttsClient.InitializeContext(ctx, as.ttsContextID)
}

func (as *AgentSession) shutdownTTS(ctx context.Context) {
	if as.ttsClient == nil {
		return
	}
	// Stop forwarder (if running).
	if as.ttsForwarderStop != nil {
		select {
		case <-as.ttsForwarderStop:
		default:
			close(as.ttsForwarderStop)
		}
		as.ttsForwarderStop = nil
		// allow future ensureTTSForwarder()
		as.ttsForwarderOnce = sync.Once{}
	}
	_ = as.ttsClient.Close()
	as.ttsClient = nil
	as.ttsContextID = ""
	as.ttsPending.Reset()
	as.ttsFormat = ""
	as.ttsVoiceID = ""
}

// CloseTTS shuts down the ElevenLabs TTS websocket (best-effort). Safe to call multiple times.
func (as *AgentSession) CloseTTS() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	as.shutdownTTS(ctx)
	// End the session-lifetime TTS context to stop any lingering goroutines.
	if as.ttsConnCancel != nil {
		as.ttsConnCancel()
		as.ttsConnCancel = nil
		as.ttsConnCtx = nil
	}
}

func (as *AgentSession) ttsHandleDelta(ctx context.Context, delta string) {
	if as.ttsClient == nil || as.ttsContextID == "" {
		return
	}
	as.ttsPending.WriteString(delta)

	s := as.ttsPending.String()
	cut := lastBoundaryIndex(s)
	// If no good boundary yet, wait for more (but prevent unbounded buffering).
	if cut < 0 && len(s) < 140 {
		return
	}

	var seg string
	var rest string
	if cut >= 0 {
		seg = s[:cut+1]
		rest = s[cut+1:]
	} else {
		seg = s
		rest = ""
	}

	as.ttsPending.Reset()
	as.ttsPending.WriteString(rest)

	seg = strings.TrimSpace(seg)
	if seg == "" {
		return
	}
	// ElevenLabs expects a trailing space.
	if !strings.HasSuffix(seg, " ") {
		seg += " "
	}
	_ = as.ttsClient.SendText(ctx, as.ttsContextID, seg, false)
}

func (as *AgentSession) ttsFlushPending(ctx context.Context) {
	if as.ttsClient == nil || as.ttsContextID == "" {
		return
	}
	rest := strings.TrimSpace(as.ttsPending.String())
	as.ttsPending.Reset()
	if rest != "" {
		if !strings.HasSuffix(rest, " ") {
			rest += " "
		}
		_ = as.ttsClient.SendText(ctx, as.ttsContextID, rest, false)
	}
	_ = as.ttsClient.Flush(ctx, as.ttsContextID, "")
}

func (as *AgentSession) forwardTTSEvent(ev eleven_tts.IncomingMessage) {
	switch ev.Kind {
	case "audio":
		if ev.AudioB64 == "" {
			return
		}
		_ = as.Writer.WriteResponse(map[string]any{
			"type":       "tts_audio_chunk",
			"context_id": ev.ContextID,
			"audio":      ev.AudioB64,
			"format":     as.ttsFormat,
		})
	case "final":
		_ = as.Writer.WriteResponse(map[string]any{
			"type":       "tts_context_final",
			"context_id": ev.ContextID,
		})
	default:
		// ignore unknown
	}
}

func (as *AgentSession) flushTTSAsync(contextID string) {
	// Do not block the request loop; also don't rely on the interaction ctx, which is cancelled
	// immediately after the handler returns.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		// Best-effort: only flush if we're still on the same context.
		if as.ttsClient == nil || as.ttsContextID == "" || as.ttsContextID != contextID {
			return
		}
		as.ttsFlushPending(ctx)
	}()
}

func (as *AgentSession) ensureTTSForwarder() {
	as.ttsForwarderOnce.Do(func() {
		if as.ttsClient == nil {
			return
		}
		as.ttsForwarderStop = make(chan struct{})

		events := as.ttsClient.Events()
		errs := as.ttsClient.Errors()

		go func() {
			for {
				select {
				case <-as.ttsForwarderStop:
					return
				case ev, ok := <-events:
					if !ok {
						return
					}
					as.forwardTTSEvent(ev)
				case err, ok := <-errs:
					if ok && err != nil {
						_ = as.Writer.WriteResponse(map[string]any{"type": "tts_error", "error": err.Error()})
					}
				}
			}
		}()
	})
}

func lastBoundaryIndex(s string) int {
	if s == "" {
		return -1
	}
	// Prefer splitting on whitespace or punctuation to avoid "Hel lo" artifacts.
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c == ' ' || c == '\n' || c == '\t' || c == '.' || c == '!' || c == '?' || c == ',' || c == ';' || c == ':' {
			return i
		}
	}
	return -1
}

// processAccumulatedParts processes accumulated parts for function calls and text
func (as *AgentSession) processAccumulatedParts(parts []models.Model_Part) ([]models.Tool_Result, bool, error) {
	if len(parts) == 0 {
		return nil, false, nil
	}

	toolResults := []models.Tool_Result{}
	executedAny := false
	finalText := ""

	// Extract function calls and text
	functionCalls := as.extractFunctionCalls(parts, &finalText)

	if len(functionCalls) > 0 {
		// Process function calls
		modelPartsToSave := make([]models.Model_Part, 0, len(functionCalls))

		for _, fc := range functionCalls {
			// Create model part for saving
			part := models.Model_Part{
				FunctionCall: &models.FunctionCall{
					ID:   fc.ID,
					Name: fc.Name,
					Args: fc.Args,
				},
				Text: fc.TextInPart,
			}
			modelPartsToSave = append(modelPartsToSave, part)

			// Check approval and execute if auto-approved
			if approved, err := as.checkAndExecuteTool(fc); err != nil {
				as.Logger.Printf("Error checking tool approval for %s (ID: %s): %v", fc.Name, fc.ID, err)
				continue
			} else if approved {
				toolResult, err := as.executeTool(fc)
				if err != nil {
					as.Logger.Printf("Error executing tool %s (ID: %s): %v", fc.Name, fc.ID, err)
				}

				// Send tool result to client
				if err := as.sendToolResult(fc, toolResult); err != nil {
					as.Logger.Printf("Error sending tool result: %v", err)
				}

				// Add to results for next iteration
				toolResults = append(toolResults, models.Tool_Result{
					Tool_ID:     fc.ID,
					Tool_Name:   fc.Name,
					Tool_Output: toolResult,
				})
				executedAny = true
			}
		}

		// Save function calls to database
		if len(modelPartsToSave) > 0 {
			if err := as.Store.SaveMessage(as.SessionID, "model", "function_call", modelPartsToSave, ""); err != nil {
				as.Logger.Printf("Error saving function call message: %v", err)
			}
		}

	} else if finalText != "" {
		// Save text-only response
		textPart := models.Model_Part{Text: &finalText}
		if err := as.Store.SaveMessage(as.SessionID, "model", "model_message", []models.Model_Part{textPart}, ""); err != nil {
			as.Logger.Printf("Error saving text message: %v", err)
		}
	}

	return toolResults, executedAny, nil
}

// extractFunctionCalls extracts unique function calls from parts
func (as *AgentSession) extractFunctionCalls(parts []models.Model_Part, finalText *string) []functionCallInfo {
	seenFC := make(map[string]bool)
	functionCalls := []functionCallInfo{}

	for _, part := range parts {
		// Accumulate text
		if part.Text != nil {
			*finalText += *part.Text
		}

		// Process function calls
		if part.FunctionCall != nil {
			argsBytes, _ := json.Marshal(part.FunctionCall.Args)
			argsJSON := string(argsBytes)
			key := part.FunctionCall.Name + "|" + argsJSON

			if !seenFC[key] {
				seenFC[key] = true
				id := part.FunctionCall.ID
				if id == "" {
					id = uuid.New().String()
				}

				functionCalls = append(functionCalls, functionCallInfo{
					Name:       part.FunctionCall.Name,
					Args:       part.FunctionCall.Args,
					ID:         id,
					ArgsJSON:   argsJSON,
					TextInPart: part.Text,
				})
			}
		}
	}

	return functionCalls
}

// checkAndExecuteTool checks if a tool should be auto-approved
func (as *AgentSession) checkAndExecuteTool(fc functionCallInfo) (bool, error) {
	return as.Agent.ApproveTool(fc.Name, fc.Args)
}

// executeTool executes a tool and returns the result
func (as *AgentSession) executeTool(fc functionCallInfo) (string, error) {
	// Check FrontendToolExecutor first if it exists and this is a frontend tool
	if as.FrontendToolExecutor != nil && as.FrontendToolExecutor.IsFrontendTool(fc.Name) {
		return as.FrontendToolExecutor.ExecuteFrontendTool(fc.Name, fc.Args)
	}

	// If a custom tool executor is set (for frontend tools), use it
	if as.ToolExecutor != nil {
		return as.ToolExecutor(
			fc.Name,
			fc.Args,
			as.Agent,
			as.SessionID,
			as.Writer,
			as.ResponseWaiter,
			as.Logger,
		)
	}

	// Otherwise, use the standard agent ExecuteTool
	return as.Agent.ExecuteTool(fc.Name, fc.Args, as.SessionID)
}

// sendToolResult sends a tool result to the WebSocket client
func (as *AgentSession) sendToolResult(fc functionCallInfo, toolResultJSON string) error {
	var resultData map[string]interface{}
	if err := json.Unmarshal([]byte(toolResultJSON), &resultData); err != nil {
		as.Logger.Printf("Failed to unmarshal tool result JSON for structured sending (tool: %s, ID: %s): %v. Sending raw JSON string.", fc.Name, fc.ID, err)
		resultData = nil
	}

	toolMsg := WebSocketToolResultMessage{
		Type:         "tool_result",
		FunctionName: fc.Name,
		FunctionID:   fc.ID,
		Result:       resultData,
		ResultJSON:   toolResultJSON,
	}

	return as.Writer.WriteResponse(toolMsg)
}

// sendError sends an error message and returns an AgentError
func (as *AgentSession) sendError(message string, fatal bool) error {
	as.Logger.Printf("Error: %s (fatal: %v)", message, fatal)
	as.Writer.WriteError(message)
	return &AgentError{Message: message, Fatal: fatal}
}
