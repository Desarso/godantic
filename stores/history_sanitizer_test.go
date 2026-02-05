package stores

import (
	"testing"
)

func TestSanitizeHistory_EmptyHistory(t *testing.T) {
	msgs := []Message{}
	result := SanitizeHistory(msgs)
	if len(result) != 0 {
		t.Errorf("Expected empty result, got %d messages", len(result))
	}
}

func TestSanitizeHistory_ValidHistory(t *testing.T) {
	msgs := []Message{
		{Type: "user_message", Role: "user"},
		{Type: "model_message", Role: "model"},
		{Type: "user_message", Role: "user"},
		{Type: "function_call", Role: "model"},
		{Type: "function_response", Role: "user"},
		{Type: "model_message", Role: "model"},
	}
	result := SanitizeHistory(msgs)
	if len(result) != 6 {
		t.Errorf("Expected 6 messages, got %d", len(result))
	}
}

func TestSanitizeHistory_OrphanedFunctionResponseAtStart(t *testing.T) {
	msgs := []Message{
		{Type: "function_response", Role: "user"}, // orphaned - should be skipped
		{Type: "model_message", Role: "model"},
		{Type: "user_message", Role: "user"},
		{Type: "model_message", Role: "model"},
	}
	result := SanitizeHistory(msgs)
	if len(result) != 3 {
		t.Errorf("Expected 3 messages (skipping orphaned function_response), got %d", len(result))
	}
	if result[0].Type != "model_message" {
		t.Errorf("Expected first message to be model_message, got %s", result[0].Type)
	}
}

func TestSanitizeHistory_TruncatedMidToolCycle(t *testing.T) {
	// Simulates truncation that starts in the middle of a tool cycle
	msgs := []Message{
		{Type: "function_call", Role: "model"},    // orphaned - should be skipped
		{Type: "function_response", Role: "user"}, // orphaned - should be skipped
		{Type: "user_message", Role: "user"},      // valid start
		{Type: "model_message", Role: "model"},
	}
	result := SanitizeHistory(msgs)
	if len(result) != 2 {
		t.Errorf("Expected 2 messages (skipping orphaned tool cycle), got %d", len(result))
	}
	if result[0].Type != "user_message" {
		t.Errorf("Expected first message to be user_message, got %s", result[0].Type)
	}
}

func TestSanitizeHistory_IncompleteCycleAtEnd(t *testing.T) {
	// Simulates LLM timeout - function_call saved but no response
	msgs := []Message{
		{Type: "user_message", Role: "user"},
		{Type: "model_message", Role: "model"},
		{Type: "user_message", Role: "user"},
		{Type: "function_call", Role: "model"}, // incomplete - should be removed
	}
	result := SanitizeHistory(msgs)
	if len(result) != 3 {
		t.Errorf("Expected 3 messages (removing incomplete cycle), got %d", len(result))
	}
	// Last message should be user_message, not the orphaned function_call
	if result[len(result)-1].Type != "user_message" {
		t.Errorf("Expected last message to be user_message, got %s", result[len(result)-1].Type)
	}
}

func TestSanitizeHistory_MultipleFunctionCallsInCycle(t *testing.T) {
	// Model makes multiple function calls in one turn
	msgs := []Message{
		{Type: "user_message", Role: "user"},
		{Type: "function_call", Role: "model"},
		{Type: "function_call", Role: "model"},
		{Type: "function_response", Role: "user"},
		{Type: "function_response", Role: "user"},
		{Type: "model_message", Role: "model"},
	}
	result := SanitizeHistory(msgs)
	if len(result) != 6 {
		t.Errorf("Expected 6 messages, got %d", len(result))
	}
}

func TestSanitizeHistory_PartialResponsesForCalls(t *testing.T) {
	// Model makes multiple function calls but only some responses came back
	// This can happen with batched tool results
	msgs := []Message{
		{Type: "user_message", Role: "user"},
		{Type: "function_call", Role: "model"},
		{Type: "function_call", Role: "model"},
		{Type: "function_response", Role: "user"}, // only one response - still valid
		{Type: "model_message", Role: "model"},
	}
	result := SanitizeHistory(msgs)
	if len(result) != 5 {
		t.Errorf("Expected 5 messages, got %d", len(result))
	}
}

func TestSanitizeHistory_OnlyOrphanedMessages(t *testing.T) {
	// Entire history is corrupted
	msgs := []Message{
		{Type: "function_response", Role: "user"},
		{Type: "function_call", Role: "model"},
	}
	result := SanitizeHistory(msgs)
	if len(result) != 0 {
		t.Errorf("Expected empty result for fully corrupted history, got %d messages", len(result))
	}
}

func TestSanitizeHistory_NestedToolCycles(t *testing.T) {
	// Tool result triggers another tool call
	msgs := []Message{
		{Type: "user_message", Role: "user"},
		{Type: "function_call", Role: "model"},
		{Type: "function_response", Role: "user"},
		{Type: "function_call", Role: "model"}, // second cycle
		{Type: "function_response", Role: "user"},
		{Type: "model_message", Role: "model"},
	}
	result := SanitizeHistory(msgs)
	if len(result) != 6 {
		t.Errorf("Expected 6 messages, got %d", len(result))
	}
}

func TestDetectCorruptedHistory_Clean(t *testing.T) {
	msgs := []Message{
		{Type: "user_message", Role: "user"},
		{Type: "model_message", Role: "model"},
	}
	issues := DetectCorruptedHistory(msgs)
	if len(issues) != 0 {
		t.Errorf("Expected no issues for clean history, got: %v", issues)
	}
}

func TestDetectCorruptedHistory_OrphanedStart(t *testing.T) {
	msgs := []Message{
		{Type: "function_response", Role: "user"},
		{Type: "model_message", Role: "model"},
	}
	issues := DetectCorruptedHistory(msgs)
	if len(issues) == 0 {
		t.Error("Expected issues for orphaned function_response at start")
	}
}

func TestDetectCorruptedHistory_OrphanedCallAtEnd(t *testing.T) {
	msgs := []Message{
		{Type: "user_message", Role: "user"},
		{Type: "function_call", Role: "model"},
	}
	issues := DetectCorruptedHistory(msgs)
	if len(issues) == 0 {
		t.Error("Expected issues for orphaned function_call at end")
	}
}
