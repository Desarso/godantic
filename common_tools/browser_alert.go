package common_tools

import (
	"encoding/json"
	"fmt"
)

//go:generate ../../gen_schema -func=Browser_Alert -file=browser_alert.go -out=../schemas/cached_schemas

// Browser_Alert triggers an alert in the user's browser with the specified message
func Browser_Alert(message string) (string, error) {
	if message == "" {
		message = "Alert from your AI assistant!"
	}

	// Return a special JSON format that the frontend will recognize
	result := map[string]interface{}{
		"action":  "browser_alert",
		"message": message,
		"success": true,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal alert result: %w", err)
	}

	return string(resultJSON), nil
}
