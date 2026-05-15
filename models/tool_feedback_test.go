package models

import (
	"strings"
	"testing"
)

func TestFormatToolResultForModelConvertsSuccessfulJSONToTOON(t *testing.T) {
	input := `{"ok":true,"status":200,"data":{"value":[{"subject":"hello"}]}}`
	got := FormatToolResultForModel("Execute_TypeScript", "call_1", input)

	for _, want := range []string{
		`tool_feedback_format: "toon"`,
		`tool_status: "succeeded"`,
		`tool_name: "Execute_TypeScript"`,
		`tool_call_id: "call_1"`,
		`tool_output:`,
		`data:`,
		`value[1]:`,
		`subject: "hello"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted output missing %q:\n%s", want, got)
		}
	}
}

func TestFormatToolResultForModelKeepsSuccessfulPlainTextUnchanged(t *testing.T) {
	input := `plain text result`
	got := FormatToolResultForModel("Execute_TypeScript", "call_1", input)
	if got != input {
		t.Fatalf("expected plain text output unchanged, got %q", got)
	}
}

func TestFormatToolResultForModelWrapsFailureAsTOON(t *testing.T) {
	input := `{"error":"Execution error: invalid input"}`
	got := FormatToolResultForModel("Execute_TypeScript", "call_1", input)

	for _, want := range []string{
		`tool_feedback_format: "toon"`,
		`tool_status: "failed"`,
		`tool_name: "Execute_TypeScript"`,
		`tool_call_id: "call_1"`,
		`error: "Execution error: invalid input"`,
		`raw_tool_output_json: "{\"error\":\"Execution error: invalid input\"}"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted output missing %q:\n%s", want, got)
		}
	}
}

func TestFormatToolResultForModelPreservesStructuredErrorText(t *testing.T) {
	input := `{"error":"Expected JSON format: {\"top\": 10}"}`
	got := FormatToolResultForModel("Execute_TypeScript", "call_2", input)

	if !strings.Contains(got, `Expected JSON format: {\"top\": 10}`) {
		t.Fatalf("expected JSON guidance to be preserved, got:\n%s", got)
	}
	if !strings.Contains(got, `raw_tool_output_json`) {
		t.Fatalf("expected raw failed output to be preserved, got:\n%s", got)
	}
}
