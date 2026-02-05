package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Desarso/godantic/common_tools"
	"github.com/Desarso/godantic/models"
	"github.com/Desarso/godantic/stores"
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

	// Set up history warning callback to send warnings to frontend
	// This is called when the model adapts conversation history and some content is filtered
	warningsSent := false
	as.Agent.SetHistoryWarningCallback(func(warnings []models.HistoryWarning) {
		if len(warnings) > 0 && !warningsSent {
			warningsSent = true
			_ = as.Writer.WriteResponse(map[string]any{
				"type":     "history_warnings",
				"warnings": warnings,
			})
		}
	})

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

		// Save user message or tool results if present BEFORE fetching history
		// This ensures that when we fetch history, it includes the tool results we just saved
		// (which are the responses to function_calls that were saved in the previous iteration)
		if err := as.saveIncomingMessage(currentReq); err != nil {
			as.Logger.Printf("Error saving incoming message: %v", err)
		}

		// Fetch latest history (after saving, so it includes the just-saved messages)
		if err := as.fetchHistory(); err != nil {
			return as.sendError("Failed to fetch history", false)
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
				_ = as.Writer.WriteResponse(map[string]any{
					"type":       "tts_context_started",
					"context_id": as.ttsContextID,
					"format":     as.ttsFormat,
				})
			} else {
				// Voice mode requested, but TTS isn't configured (e.g., missing ELEVENLABS_API_KEY).
				_ = as.Writer.WriteResponse(map[string]any{"type": "tts_unconfigured"})
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

// fetchHistory retrieves the latest conversation history (limited to last 15 messages)
func (as *AgentSession) fetchHistory() error {
	history, err := as.Store.FetchHistory(as.SessionID, 15)
	if err != nil {
		as.Logger.Printf("Error fetching history: %v", err)
		return &AgentError{Message: "Failed to fetch history", Fatal: false}
	}
	as.History = history
	return nil
}

// saveIncomingMessage saves user messages or tool results to the database and memory
func (as *AgentSession) saveIncomingMessage(req models.Model_Request) error {
	if req.User_Message != nil {
		if err := as.saveUserMessage(req.User_Message); err != nil {
			return err
		}
		as.saveToMemoryAsync(as.extractTextFromMessage(req.User_Message), "user")
	} else if req.Tool_Results != nil {
		if err := as.saveToolResults(*req.Tool_Results); err != nil {
			return err
		}
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

	// Log user message flow
	if as.FlowLogger != nil && userText != "" {
		as.FlowLogger.LogUserMessage(as.SessionID, userText)
	}

	return as.Store.SaveMessageWithUser(as.SessionID, as.UserID, "user", "user_message", userPartsToSave, "")
}

// saveToolResults saves tool results to the database
func (as *AgentSession) saveToolResults(toolResults []models.Tool_Result) error {
	toolResponseParts := make([]models.User_Part, 0, len(toolResults))

	for _, toolResult := range toolResults {
		var resultMap map[string]interface{}
		if err := json.Unmarshal([]byte(toolResult.Tool_Output), &resultMap); err != nil {
			// Not JSON - wrap plain text output (normal for Execute_TypeScript)
			resultMap = map[string]interface{}{"output": toolResult.Tool_Output}
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
		return as.Store.SaveMessageWithUser(as.SessionID, as.UserID, "user", "function_response", toolResponseParts, "")
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
		return nil // not configured; caller will notify client in voice mode
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
	as.ttsMu.Lock()
	as.ttsPending.Reset()
	as.ttsMu.Unlock()
	as.ttsVoiceID = voiceID
	as.ensureTTSForwarder()

	// Initialize the single long-lived context once per session.
	_ = as.Writer.WriteResponse(map[string]any{
		"type":       "tts_context_started",
		"context_id": as.ttsContextID,
		"format":     as.ttsFormat,
	})
	// Initialize context using a short-lived independent context so barge-in cancellation
	// doesn't prevent TTS from ever starting.
	initCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return as.ttsClient.InitializeContext(initCtx, as.ttsContextID)
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
	as.ttsMu.Lock()
	as.ttsPending.Reset()
	as.ttsMu.Unlock()
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
	// Build a segment under lock, but do the network call outside the lock.
	var seg string
	as.ttsMu.Lock()
	as.ttsPending.WriteString(delta)

	s := as.ttsPending.String()
	cut := lastBoundaryIndex(s)
	// If no good boundary yet, wait for more (but prevent unbounded buffering).
	if cut < 0 && len(s) < 140 {
		as.ttsMu.Unlock()
		return
	}

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
	as.ttsMu.Unlock()

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
	as.ttsMu.Lock()
	rest := strings.TrimSpace(as.ttsPending.String())
	as.ttsPending.Reset()
	as.ttsMu.Unlock()
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
	finalReasoning := ""

	// Extract function calls, text, and reasoning
	functionCalls := as.extractFunctionCalls(parts, &finalText, &finalReasoning)

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
				toolResult, execErr := as.executeTool(fc)
				if execErr != nil {
					as.Logger.Printf("Error executing tool %s (ID: %s): %v", fc.Name, fc.ID, execErr)
					// Include error message in result so the model can see what went wrong
					toolResult = fmt.Sprintf(`{"error": %q}`, execErr.Error())
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
			if err := as.Store.SaveMessageWithUser(as.SessionID, as.UserID, "model", "function_call", modelPartsToSave, ""); err != nil {
				as.Logger.Printf("Error saving function call message: %v", err)
			}
		}

	} else if finalText != "" || finalReasoning != "" {
		// Log agent message flow
		if as.FlowLogger != nil {
			as.FlowLogger.LogAgentMessage(as.SessionID, finalText)
		}

		// Save text response with reasoning if present
		partsToSave := []models.Model_Part{}
		if finalReasoning != "" {
			partsToSave = append(partsToSave, models.Model_Part{Reasoning: &finalReasoning})
		}
		if finalText != "" {
			partsToSave = append(partsToSave, models.Model_Part{Text: &finalText})
		}
		if err := as.Store.SaveMessageWithUser(as.SessionID, as.UserID, "model", "model_message", partsToSave, ""); err != nil {
			as.Logger.Printf("Error saving text message: %v", err)
		}
		if finalText != "" {
			as.saveToMemoryAsync(finalText, "model")
		}
	}

	return toolResults, executedAny, nil
}

// extractFunctionCalls extracts unique function calls from parts, also accumulates text and reasoning
func (as *AgentSession) extractFunctionCalls(parts []models.Model_Part, finalText *string, finalReasoning *string) []functionCallInfo {
	seenFC := make(map[string]bool)
	functionCalls := []functionCallInfo{}

	for _, part := range parts {
		// Accumulate text
		if part.Text != nil {
			*finalText += *part.Text
		}

		// Accumulate reasoning (chain-of-thought from models like Kimi K2.5, DeepSeek-R1)
		if part.Reasoning != nil {
			*finalReasoning += *part.Reasoning
		}

		// Process function calls
		if part.FunctionCall != nil {
			// Trim whitespace from function name (some models output with leading/trailing spaces)
			funcName := strings.TrimSpace(part.FunctionCall.Name)
			argsBytes, _ := json.Marshal(part.FunctionCall.Args)
			argsJSON := string(argsBytes)
			key := funcName + "|" + argsJSON

			if !seenFC[key] {
				seenFC[key] = true
				id := part.FunctionCall.ID
				if id == "" {
					id = uuid.New().String()
				}

				functionCalls = append(functionCalls, functionCallInfo{
					Name:       funcName,
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
	// Log tool call
	if as.FlowLogger != nil {
		as.FlowLogger.LogToolCall(as.SessionID, fc.Name, fc.Args)
	}

	// Emit start trace for all tools (visible in frontend timeline)
	startTime := time.Now()
	traceID := fmt.Sprintf("tool_%s_%d", fc.ID, startTime.UnixMilli())
	as.emitToolTrace(fc.ID, traceID, fc.Name, "start", getToolStartLabel(fc.Name, fc.Args), nil)

	var result string
	var err error

	// Special handling for Execute_TypeScript to enable detailed internal tracing
	if fc.Name == "Execute_TypeScript" {
		result, err = as.executeTypeScriptWithTracing(fc)
	} else if as.FrontendToolExecutor != nil && as.FrontendToolExecutor.IsFrontendTool(fc.Name) {
		// Check FrontendToolExecutor if it exists and this is a frontend tool
		result, err = as.FrontendToolExecutor.ExecuteFrontendTool(fc.Name, fc.Args)
	} else if as.ToolExecutor != nil {
		// If a custom tool executor is set (for frontend tools), use it
		result, err = as.ToolExecutor(
			fc.Name,
			fc.Args,
			as.Agent,
			as.SessionID,
			as.Writer,
			as.ResponseWaiter,
			as.Logger,
		)
	} else {
		// Otherwise, use the standard agent ExecuteTool
		result, err = as.Agent.ExecuteTool(fc.Name, fc.Args, as.SessionID)
	}

	// Emit end trace
	durationMs := time.Since(startTime).Milliseconds()
	if err != nil {
		as.emitToolTrace(fc.ID, traceID, fc.Name, "error", getToolErrorLabel(fc.Name, err), &durationMs)
	} else {
		as.emitToolTrace(fc.ID, traceID, fc.Name, "end", getToolEndLabel(fc.Name, fc.Args), &durationMs)
	}

	// Log tool result
	if as.FlowLogger != nil {
		preview := result
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		as.FlowLogger.LogToolResult(as.SessionID, fc.Name, preview)
	}

	return result, err
}

// emitToolTrace sends a trace event over WebSocket for real-time visualization
// Note: Regular tool traces are NOT persisted to database since they're 1:1 with
// tool_call/tool_result messages already in chat history. Only TypeScript executor
// internal traces (from wsTraceEmitterAdapter) are persisted.
func (as *AgentSession) emitToolTrace(toolCallID, traceID, toolName, status, label string, durationMs *int64) {
	timestamp := time.Now().UnixMilli()
	tool := getToolCategory(toolName)

	// Send to WebSocket for real-time visualization
	msg := WebSocketTraceMessage{
		Type:       "execution_trace",
		TraceID:    traceID,
		ToolCallID: toolCallID,
		Tool:       tool,
		Operation:  toolName,
		Status:     status,
		Label:      label,
		Timestamp:  timestamp,
	}
	if durationMs != nil {
		msg.DurationMS = *durationMs
	}
	// Ignore errors - traces are non-critical for WebSocket
	_ = as.Writer.WriteResponse(msg)

	// Note: We intentionally do NOT persist regular tool traces to the database.
	// Regular tools (Search, Generate_Image, etc.) have their execution recorded
	// in the chat history as tool_call and tool_result messages. Persisting traces
	// would be redundant. Only TypeScript executor internal operations (web.get,
	// tavily.search, etc.) are persisted via wsTraceEmitterAdapter.
}

// getToolCategory returns the category for a tool (for UI icons)
func getToolCategory(toolName string) string {
	switch toolName {
	case "Search", "Brave_Search":
		return "search"
	case "Execute_TypeScript":
		return "code"
	case "Generate_Image":
		return "image"
	case "List_Skill_Files", "Read_Skill_File", "Edit_Skill_File", "Create_Skill_File", "Delete_Skill_File":
		return "skills"
	case "Browser_Alert", "Browser_Prompt", "Browser_Navigate", "Sandbox_Run", "Confirm_With_User":
		return "browser"
	default:
		if strings.Contains(toolName, "Workflow") {
			return "workflow"
		}
		return "tool"
	}
}

// getToolStartLabel returns a human-readable start label for a tool
func getToolStartLabel(toolName string, args map[string]interface{}) string {
	switch toolName {
	case "Search", "Brave_Search":
		if query, ok := args["query"].(string); ok {
			if len(query) > 50 {
				query = query[:50] + "..."
			}
			return fmt.Sprintf("Searching: \"%s\"", query)
		}
		return "Searching the web"
	case "Execute_TypeScript":
		return "Executing code"
	case "Generate_Image":
		return "Generating image"
	case "List_Skill_Files":
		return "Listing skills"
	case "Read_Skill_File":
		if name, ok := args["name"].(string); ok {
			return fmt.Sprintf("Reading skill: %s", name)
		}
		return "Reading skill"
	case "Browser_Navigate":
		if url, ok := args["url"].(string); ok {
			return fmt.Sprintf("Navigating to %s", url)
		}
		return "Opening browser"
	case "Confirm_With_User":
		return "Waiting for confirmation"
	default:
		// Convert tool name to readable format
		readable := strings.ReplaceAll(toolName, "_", " ")
		return fmt.Sprintf("Running %s", readable)
	}
}

// getToolErrorLabel returns a clean, user-friendly error label for a tool
func getToolErrorLabel(toolName string, err error) string {
	errMsg := err.Error()

	// Extract just the key message for common error types
	switch {
	case strings.Contains(errMsg, "query cannot be empty"):
		return "Failed: empty search query"
	case strings.Contains(errMsg, "API request failed"):
		// Extract just "API error" without the full JSON response
		return "Failed: API error"
	case strings.Contains(errMsg, "timeout"):
		return "Failed: request timed out"
	case strings.Contains(errMsg, "connection"):
		return "Failed: connection error"
	case strings.Contains(errMsg, "not found"):
		return "Failed: not found"
	case strings.Contains(errMsg, "unauthorized") || strings.Contains(errMsg, "401"):
		return "Failed: unauthorized"
	case strings.Contains(errMsg, "forbidden") || strings.Contains(errMsg, "403"):
		return "Failed: access denied"
	default:
		// Truncate long error messages
		if len(errMsg) > 40 {
			errMsg = errMsg[:40] + "..."
		}
		return fmt.Sprintf("Failed: %s", errMsg)
	}
}

// getToolEndLabel returns a human-readable end label for a tool (past tense)
func getToolEndLabel(toolName string, args map[string]interface{}) string {
	switch toolName {
	case "Search", "Brave_Search":
		if query, ok := args["query"].(string); ok {
			if len(query) > 50 {
				query = query[:50] + "..."
			}
			return fmt.Sprintf("Searched: \"%s\"", query)
		}
		return "Searched the web"
	case "Execute_TypeScript":
		return "Executed code"
	case "Generate_Image":
		return "Generated image"
	case "List_Skill_Files":
		return "Listed skills"
	case "Read_Skill_File":
		if name, ok := args["name"].(string); ok {
			return fmt.Sprintf("Read skill: %s", name)
		}
		return "Read skill"
	case "Browser_Navigate":
		if url, ok := args["url"].(string); ok {
			return fmt.Sprintf("Navigated to %s", url)
		}
		return "Opened browser"
	case "Confirm_With_User":
		return "User responded"
	default:
		readable := strings.ReplaceAll(toolName, "_", " ")
		return fmt.Sprintf("Ran %s", readable)
	}
}

// executeTypeScriptWithTracing executes TypeScript code with real-time trace streaming
func (as *AgentSession) executeTypeScriptWithTracing(fc functionCallInfo) (string, error) {
	// Extract the code argument
	code, ok := fc.Args["code"].(string)
	if !ok {
		// Try to get from any single argument
		for _, v := range fc.Args {
			if s, ok := v.(string); ok {
				code = s
				break
			}
		}
	}

	if code == "" {
		return "", nil
	}

	// Create a trace emitter that sends traces over WebSocket and saves to DB
	// The trace emitter is linked to this specific tool call via fc.ID
	traceEmitter := &wsTraceEmitterAdapter{
		emitter: &WebSocketTraceEmitter{
			Writer:     as.Writer,
			ToolCallID: fc.ID,
		},
		traceStore:     as.TraceStore,
		conversationID: as.SessionID,
		toolCallID:     fc.ID,
		logger:         as.Logger,
	}

	return common_tools.Execute_TypeScriptWithTracing(code, traceEmitter)
}

// wsTraceEmitterAdapter adapts WebSocketTraceEmitter to common_tools.TraceEmitter
// Also handles persisting traces to the database
type wsTraceEmitterAdapter struct {
	emitter        *WebSocketTraceEmitter
	traceStore     stores.TraceStore
	conversationID string
	toolCallID     string
	logger         *log.Logger
}

func (a *wsTraceEmitterAdapter) EmitTrace(trace common_tools.TraceEvent) error {
	// Convert common_tools.TraceEvent to sessions.TraceEvent and send via WebSocket
	sessionTrace := TraceEvent{
		TraceID:    trace.TraceID,
		ParentID:   trace.ParentID,
		Tool:       trace.Tool,
		Operation:  trace.Operation,
		Status:     trace.Status,
		Label:      trace.Label,
		Details:    trace.Details,
		Timestamp:  trace.Timestamp,
		DurationMS: trace.DurationMS,
	}
	err := a.emitter.EmitTrace(sessionTrace)

	// Also save to database if trace store is configured
	if a.traceStore != nil {
		dbTrace := &stores.ExecutionTrace{
			ConversationID: a.conversationID,
			ToolCallID:     a.toolCallID,
			TraceID:        trace.TraceID,
			ParentID:       trace.ParentID,
			Tool:           trace.Tool,
			Operation:      trace.Operation,
			Status:         trace.Status,
			Label:          trace.Label,
			Details:        trace.Details,
			Timestamp:      trace.Timestamp,
			DurationMS:     trace.DurationMS,
		}
		// Save async to avoid blocking
		go func() {
			if saveErr := a.traceStore.SaveTrace(dbTrace); saveErr != nil && a.logger != nil {
				a.logger.Printf("Warning: Failed to save TypeScript trace to database: %v", saveErr)
			}
		}()
	}

	return err
}

// sendToolResult sends a tool result to the WebSocket client
func (as *AgentSession) sendToolResult(fc functionCallInfo, toolResultJSON string) error {
	var resultData map[string]interface{}
	if err := json.Unmarshal([]byte(toolResultJSON), &resultData); err != nil {
		// Not JSON - wrap plain text output in a structure
		// This is normal for Execute_TypeScript which returns console.log output
		resultData = map[string]interface{}{"output": toolResultJSON}
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

// saveToMemoryAsync saves content to memory asynchronously (fire-and-forget)
func (as *AgentSession) saveToMemoryAsync(content string, role string) {
	if as.Memory == nil {
		as.Logger.Printf("[SESSION-MEMORY] Memory is nil, skipping save for role=%s", role)
		return
	}

	as.Logger.Printf("[SESSION-MEMORY] Queueing async memory save: role=%s contentLen=%d preview='%.200s'", role, len(content), content)

	go func() {
		contextText := as.buildMemoryContext()
		if contextText != "" {
			as.Logger.Printf("[SESSION-MEMORY] Using conversation context for memory (len=%d): '%.300s'", len(contextText), contextText)
			content = contextText
		} else {
			as.Logger.Printf("[SESSION-MEMORY] No conversation context, using raw content")
		}
		if content == "" {
			as.Logger.Printf("[SESSION-MEMORY] SKIPPED: content is empty after context build")
			return
		}

		metadata := map[string]interface{}{
			"session_id": as.SessionID,
			"role":       role,
			"timestamp":  time.Now().Format(time.RFC3339),
		}
		as.Logger.Printf("[SESSION-MEMORY] Calling AddMemory: role=%s contentLen=%d", role, len(content))
		if err := as.Memory.AddMemory(content, metadata); err != nil {
			as.Logger.Printf("[SESSION-MEMORY] FAILED to save memory: %v", err)
		} else {
			as.Logger.Printf("[SESSION-MEMORY] SUCCESS: saved memory for role=%s", role)
		}
	}()
}

func (as *AgentSession) buildMemoryContext() string {
	if as.Store == nil {
		return ""
	}

	limit := getMemoryContextLimit()
	if limit <= 0 {
		return ""
	}

	history, err := as.Store.FetchHistory(as.SessionID, limit)
	if err != nil || len(history) == 0 {
		return ""
	}

	var b strings.Builder
	for _, msg := range history {
		text := extractTextFromStoredMessage(msg)
		if text == "" {
			continue
		}

		roleLabel := "User"
		if msg.Role == "model" {
			roleLabel = "Assistant"
		}
		b.WriteString(roleLabel)
		b.WriteString(": ")
		b.WriteString(text)
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

func extractTextFromStoredMessage(msg stores.Message) string {
	if msg.PartsJSON == "" || msg.PartsJSON == "{}" || msg.PartsJSON == "null" {
		return ""
	}

	var text strings.Builder
	switch msg.Type {
	case "user_message":
		var userParts []models.User_Part
		if err := json.Unmarshal([]byte(msg.PartsJSON), &userParts); err != nil {
			return ""
		}
		for _, part := range userParts {
			if part.Text != "" {
				text.WriteString(part.Text)
			}
		}
	case "model_message":
		var modelParts []models.Model_Part
		if err := json.Unmarshal([]byte(msg.PartsJSON), &modelParts); err != nil {
			return ""
		}
		for _, part := range modelParts {
			if part.Text != nil && *part.Text != "" {
				text.WriteString(*part.Text)
			}
		}
	default:
		return ""
	}

	return strings.TrimSpace(text.String())
}

func getMemoryContextLimit() int {
	const defaultLimit = 3
	if value := os.Getenv("MEMORY_CONTEXT_MESSAGES"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultLimit
}

// retrieveMemories retrieves relevant memories based on the query
func (as *AgentSession) retrieveMemories(queryText string, mode string) ([]string, error) {
	if as.Memory == nil || queryText == "" {
		return nil, nil
	}

	limit := 10
	if strings.EqualFold(mode, "voice") {
		limit = 5
	}

	return as.Memory.RetrieveMemories(queryText, limit)
}

// extractTextFromMessage extracts text content from a user message
func (as *AgentSession) extractTextFromMessage(userMsg *models.User_Message) string {
	if userMsg == nil || userMsg.Content.Parts == nil {
		return ""
	}

	var text strings.Builder
	for _, part := range userMsg.Content.Parts {
		if part.Text != "" {
			text.WriteString(part.Text)
		}
	}
	return strings.TrimSpace(text.String())
}

// extractTextFromResponse extracts text content from a model response
func (as *AgentSession) extractTextFromResponse(response *models.Model_Response) string {
	if response == nil || response.Parts == nil {
		return ""
	}

	var text strings.Builder
	for _, part := range response.Parts {
		if part.Text != nil && *part.Text != "" {
			text.WriteString(*part.Text)
		}
	}
	return strings.TrimSpace(text.String())
}
