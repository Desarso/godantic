package stores

import (
	"log"
)

// SanitizeHistory ensures the message history has valid turn structure for LLM APIs.
// It handles two main issues:
// 1. Truncation breaking tool cycles - ensures we don't start with orphaned function_response
// 2. Corrupted history - removes orphaned function_calls without matching function_responses
//
// Valid turn patterns:
// - user_message -> model_message
// - user_message -> function_call -> function_response -> model_message (or more tool cycles)
//
// The function ensures:
// - History always starts with a user_message (not function_response or function_call)
// - Every function_call has a matching function_response after it
// - No orphaned function_responses without preceding function_calls
func SanitizeHistory(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Step 1: Find a valid starting point
	// We need to start with a user_message (not function_response)
	startIdx := findValidStartIndex(msgs)
	if startIdx == -1 {
		// No valid starting point found - try to find ANY user_message in history
		// to preserve at least some context
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Type == "user_message" {
				log.Printf("[HISTORY_SANITIZER] No valid start, but found user_message at index %d, using as fallback", i)
				return []Message{msgs[i]}
			}
		}
		log.Printf("[HISTORY_SANITIZER] No valid starting point found, returning empty history")
		return []Message{}
	}

	if startIdx > 0 {
		log.Printf("[HISTORY_SANITIZER] Skipping first %d messages to find valid start (was type: %s)", startIdx, msgs[0].Type)
		msgs = msgs[startIdx:]
	}

	// Step 2: Validate and fix tool call cycles
	// Walk through and ensure every function_call has a matching function_response
	sanitized := sanitizeToolCycles(msgs)

	if len(sanitized) != len(msgs) {
		log.Printf("[HISTORY_SANITIZER] Removed %d messages with broken tool cycles", len(msgs)-len(sanitized))
	}

	return sanitized
}

// findValidStartIndex finds the first message that is a valid conversation start.
// A valid start is either:
// - A user_message
// - A model_message (though unusual, it's valid)
// We skip function_response and function_call at the beginning as they're orphaned.
func findValidStartIndex(msgs []Message) int {
	for i, msg := range msgs {
		switch msg.Type {
		case "user_message", "model_message":
			return i
		case "function_call":
			// A function_call at the start means we truncated in the middle of a cycle
			// Skip it and any following function_responses until we find a clean start
			continue
		case "function_response":
			// Orphaned function_response - skip it
			continue
		default:
			// Unknown type, try to use it
			return i
		}
	}
	return -1
}

// sanitizeToolCycles walks through messages and ensures tool call cycles are complete.
// If a function_call doesn't have a matching function_response, we remove the incomplete cycle.
// If a function_response appears without a preceding function_call, we remove it.
func sanitizeToolCycles(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}

	result := make([]Message, 0, len(msgs))
	i := 0

	for i < len(msgs) {
		msg := msgs[i]

		switch msg.Type {
		case "user_message":
			// User messages are always valid
			result = append(result, msg)
			i++

		case "model_message":
			// Model messages are always valid
			result = append(result, msg)
			i++

		case "function_call":
			// A function_call must be followed by a function_response (possibly after more function_calls)
			// Collect all function_calls and their responses as a batch
			cycleStart := i
			cycleMessages, nextIdx, valid := collectCompleteCycle(msgs, i)

			if valid {
				// Complete cycle - add all messages
				result = append(result, cycleMessages...)
				i = nextIdx
			} else {
				// Incomplete cycle - but if it's at the END of history, keep the function_calls
				// because the function_response might be coming in the current request (tool results turn)
				if nextIdx >= len(msgs) {
					// Trailing function_calls at end of history - keep them
					// The response will come in the current Model_Request.Tool_Results
					log.Printf("[HISTORY_SANITIZER] Keeping trailing function_call(s) at end of history (index %d-%d) - response expected in current turn", cycleStart, nextIdx-1)
					result = append(result, cycleMessages...)
					i = nextIdx
				} else {
					// Incomplete cycle in the middle of history - this shouldn't happen in normal operation
					// Skip these orphaned function_calls
					log.Printf("[HISTORY_SANITIZER] Removing incomplete tool cycle in middle of history at index %d (function_call without response)", cycleStart)
					i = nextIdx
				}
			}

		case "function_response":
			// Orphaned function_response without preceding function_call
			// This shouldn't happen if we started correctly, but handle it
			log.Printf("[HISTORY_SANITIZER] Removing orphaned function_response at index %d", i)
			i++

		default:
			// Unknown message type - include it but log
			log.Printf("[HISTORY_SANITIZER] Unknown message type '%s' at index %d, including anyway", msg.Type, i)
			result = append(result, msg)
			i++
		}
	}

	return result
}

// collectCompleteCycle collects a complete tool call cycle starting from a function_call.
// A cycle consists of:
// - One or more function_calls (from the model)
// - Followed by matching function_responses (from the user)
// - Optionally followed by a model_message or another cycle
//
// Returns:
// - cycleMessages: the messages in the complete cycle
// - nextIdx: the index to continue from
// - valid: whether the cycle is complete
func collectCompleteCycle(msgs []Message, startIdx int) ([]Message, int, bool) {
	cycleMessages := []Message{}
	functionCallCount := 0
	functionResponseCount := 0
	i := startIdx

	// Phase 1: Collect function_calls
	for i < len(msgs) && msgs[i].Type == "function_call" {
		cycleMessages = append(cycleMessages, msgs[i])
		functionCallCount++
		i++
	}

	// Phase 2: Collect function_responses
	for i < len(msgs) && msgs[i].Type == "function_response" {
		cycleMessages = append(cycleMessages, msgs[i])
		functionResponseCount++
		i++
	}

	// Validate: we need at least one response for the calls
	// Note: The number of responses might not exactly match calls if multiple tool results
	// are batched together, but we need at least one response
	if functionResponseCount == 0 {
		// No responses found - incomplete cycle
		return nil, i, false
	}

	// Cycle is valid
	return cycleMessages, i, true
}

// DetectCorruptedHistory checks if the history has any issues that would cause API errors.
// Returns a list of issues found (empty if history is clean).
func DetectCorruptedHistory(msgs []Message) []string {
	issues := []string{}

	if len(msgs) == 0 {
		return issues
	}

	// Check 1: Does history start with a valid message?
	if msgs[0].Type == "function_response" {
		issues = append(issues, "History starts with function_response (orphaned)")
	}
	if msgs[0].Type == "function_call" {
		issues = append(issues, "History starts with function_call (truncated mid-cycle)")
	}

	// Check 2: Are there any orphaned function_calls at the end?
	pendingCalls := 0
	for _, msg := range msgs {
		switch msg.Type {
		case "function_call":
			pendingCalls++
		case "function_response":
			if pendingCalls > 0 {
				pendingCalls--
			} else {
				issues = append(issues, "function_response without preceding function_call")
			}
		}
	}

	if pendingCalls > 0 {
		issues = append(issues, "Orphaned function_call(s) without responses at end of history")
	}

	// Check 3: Consecutive messages of same role that shouldn't be consecutive
	for i := 1; i < len(msgs); i++ {
		prev := msgs[i-1]
		curr := msgs[i]

		// Two user_messages in a row is invalid (except if one is function_response)
		if prev.Role == "user" && curr.Role == "user" &&
			prev.Type == "user_message" && curr.Type == "user_message" {
			issues = append(issues, "Two consecutive user_messages")
		}
	}

	return issues
}
