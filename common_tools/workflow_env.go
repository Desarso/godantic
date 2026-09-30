package common_tools

import (
	"sort"
	"strings"
)

// workflowBaseEnvKeys are always forwarded to workflow processes (when set).
// They are needed for pnpm/node to start and behave sanely.
var workflowBaseEnvKeys = []string{
	"PATH", "HOME", "NODE_ENV", "TZ", "LANG", "LC_ALL", "TMPDIR", "USER",
	// Needed by the runtime to decide which helpers to expose.
	"TS_RUNTIME_TOOLS",
}

// workflowToolEnvKeys lists, per TypeScript runtime tool, the env vars that the
// helper in helpers/typescript_runtime/tools/<tool>.ts reads. Only the vars of
// tools enabled via TS_RUNTIME_TOOLS are forwarded. Per-request delegated
// tokens (GRAPH_/FLOW_DELEGATED_ACCESS_TOKEN) are intentionally NOT forwarded:
// workflows run in the background long after the chat request that created them.
var workflowToolEnvKeys = map[string][]string{
	"web":         {},
	"math":        {},
	"tavily":      {"TAVILY_API_KEY"},
	"graph":       {"MS_TENANT_ID", "MS_APP_ID", "MS_SECRET", "TENANT_ID", "APP_ID", "SECRET", "GRAPH_SEARCH_REGION"},
	"m365":        {},
	"skills":      {"CLIENT_ID"},
	"flow":        {"FLOW_API_BASE_URL", "FLOW_API_VERSION"},
	"automations": {"FRONTEND_URL"},
	"workspace":   {"AGENT_WORKSPACE_ROOT", "OPENROUTER_API_KEY", "OPENROUTER_PDF_ENGINE", "WORKSPACE_OPENROUTER_MODEL", "WORKSPACE_OPENROUTER_OCR_ENGINE", "WORKSPACE_OPENROUTER_PDF_ENGINE"},
	"sandbox":     {"AGENT_WORKSPACE_ROOT", "AGENT_SANDBOX_CPUS", "AGENT_SANDBOX_IMAGE", "AGENT_SANDBOX_MEMORY", "AGENT_SANDBOX_PIDS", "AGENT_SANDBOX_TOKEN", "AGENT_SANDBOX_URL"},
	"image":       {"GEMINI_API_KEY", "SERVER_HOST"},
}

const defaultTSRuntimeTools = "web,tavily,math"

// BuildWorkflowEnv returns the allowlisted environment for a workflow process.
// getenv is normally os.Getenv; it is a parameter so it can be unit-tested.
// extra entries (already validated by the caller) are appended last and win.
func BuildWorkflowEnv(getenv func(string) string, extra map[string]string) []string {
	values := map[string]string{}
	add := func(key string) {
		if v := getenv(key); v != "" {
			values[key] = v
		}
	}

	for _, key := range workflowBaseEnvKeys {
		add(key)
	}

	toolsList := getenv("TS_RUNTIME_TOOLS")
	if toolsList == "" {
		toolsList = defaultTSRuntimeTools
	}
	for _, tool := range strings.Split(toolsList, ",") {
		for _, key := range workflowToolEnvKeys[strings.TrimSpace(tool)] {
			add(key)
		}
	}

	// Mirror the workspace ids that Execute_TypeScript receives per request.
	globalWorkspace := getenv("AGENT_GLOBAL_WORKSPACE_ID")
	if globalWorkspace == "" {
		globalWorkspace = getenv("CLIENT_ID")
	}
	if globalWorkspace == "" {
		globalWorkspace = "default"
	}
	values["AGENT_GLOBAL_WORKSPACE_ID"] = globalWorkspace

	for k, v := range extra {
		if k != "" && v != "" {
			values[k] = v
		}
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+values[k])
	}
	return env
}
