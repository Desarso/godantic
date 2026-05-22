# godantic

[![Docs](https://img.shields.io/badge/docs-live-2ed18f)](https://desarso.github.io/godantic/)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-f8f3e7)](LICENSE)

`godantic` is a small Go framework for building useful LLM agents without turning your app into a pile of provider glue.

Bring a model, a message store, and optional tools. `godantic` gives you the agent loop, streaming, tool calls, history persistence, and HTTP/WebSocket session helpers.

Documentation: [https://desarso.github.io/godantic/](https://desarso.github.io/godantic/)

Current version line: `v0.x`. See [Versioning](https://desarso.github.io/godantic/versioning/) for the release policy.

## Why godantic

Most assistant backends end up solving the same problems: provider adapters, function calling, streaming, tool result feedback, chat history, WebSocket plumbing, and persistence. `godantic` keeps those concerns in one Go module with simple interfaces you can replace when your app needs something custom.

## Features

- One `Model` interface across Gemini, OpenRouter, Groq, Cerebras, Anthropic, and custom providers.
- Request/response, streaming, SSE, and WebSocket session helpers.
- Tool calling with JSON-schema declarations backed by ordinary Go functions.
- SQLite and PostgreSQL conversation stores through a replaceable `MessageStore` interface.
- Built-in tools for search, web fetch, file operations, shell execution, image analysis, workflows, skill files, TypeScript execution, and image generation.
- Optional trace persistence for WebSocket tool execution.

## Docs

- [Getting Started](https://desarso.github.io/godantic/getting-started/)
- [Architecture](https://desarso.github.io/godantic/architecture/)
- [API Guide](https://desarso.github.io/godantic/api/)
- [Models](https://desarso.github.io/godantic/models/)
- [Tools](https://desarso.github.io/godantic/tools/)
- [Sessions](https://desarso.github.io/godantic/sessions/)
- [Storage](https://desarso.github.io/godantic/storage/)
- [Production](https://desarso.github.io/godantic/production/)
- [Versioning](https://desarso.github.io/godantic/versioning/)

## Releases

The version is stored in `VERSION`. Release helpers live in the root `Makefile`:

```bash
make test
make docs-build
make release
```

`make release` validates the tree, creates a `vX.Y.Z` tag, pushes `main` and the tag, and creates a GitHub release using `gh`.

## Install

```bash
go get github.com/Desarso/godantic
```

For local development from a parent app, use a Go replace directive:

```go
replace github.com/Desarso/godantic => ./godantic
```

## Environment

Set the API key for the provider you use:

| Provider | Default env var |
| --- | --- |
| Gemini | `GEMINI_API_KEY` |
| OpenRouter | `OPENROUTER_API_KEY` |
| Groq | `GROQ_API_KEY` |
| Cerebras | `CEREBRAS_API_KEY` |
| Anthropic | `ANTHROPIC_API_KEY` |

Some built-in tools require their own keys:

| Tool | Env var |
| --- | --- |
| `common_tools.Brave_Search` | `BRAVE_API_KEY` |
| `common_tools.Search` | `PERPLEXITY_API_KEY` |

WebSocket TTS support uses optional ElevenLabs env vars such as `ELEVEN_LABS_API_KEY`, `ELEVEN_LABS_TTS_VOICE_ID`, and `ELEVEN_LABS_TTS_MODEL_ID`.

## Quick Start

```go
package main

import (
    "fmt"
    "log"

    "github.com/Desarso/godantic"
    "github.com/Desarso/godantic/models"
    "github.com/Desarso/godantic/stores"
)

func main() {
    store, err := stores.NewSQLiteStoreSimple("chat.sqlite")
    if err != nil {
        log.Fatal(err)
    }

    agent := godantic.Create_Agent(
        godantic.NewGeminiModel("gemini-2.0-flash"),
        nil,
    )

    session := godantic.NewHTTPSession("conversation-1", &agent, store)

    userMessage := models.User_Message{
        Role: "user",
        Content: models.Content{Parts: []models.User_Part{{Text: "Say hello in one sentence."}}},
    }

    response, err := session.RunSingleInteraction(userMessage)
    if err != nil {
        log.Fatal(err)
    }

    for _, part := range response.Parts {
        if part.Text != nil {
            fmt.Println(*part.Text)
        }
    }
}
```

Run it with:

```bash
GEMINI_API_KEY=... go run .
```

## Models

Use the helper constructors for common providers:

```go
agent := godantic.Create_Agent(godantic.NewGeminiModel("gemini-2.0-flash"), tools)
agent := godantic.Create_Agent(godantic.NewOpenRouterModel("openai/gpt-4o-mini"), tools)
agent := godantic.Create_Agent(godantic.NewGroqModel("llama-3.1-70b-versatile"), tools)
agent := godantic.Create_Agent(godantic.NewCerebrasModel("llama-3.3-70b"), tools)
agent := godantic.Create_Agent(godantic.NewAnthropicModel("claude-sonnet-4-20250514"), tools)
```

Provider-specific option helpers are also available:

```go
temp := 0.2
maxTokens := 2048

model := godantic.NewOpenRouterModelWithOptions(
    "anthropic/claude-sonnet-4",
    &temp,
    &maxTokens,
    "https://example.com",
    "Example App",
)
```

OpenRouter, Groq, Cerebras, and Anthropic model structs also support custom `BaseURL` and `APIKeyEnv` fields for compatible gateways.

## Sessions

### HTTP

Use `HTTPSession` for request/response APIs, SSE endpoints, jobs, or tests.

```go
session := godantic.NewHTTPSession("conversation-1", &agent, store)

response, err := session.RunSingleInteraction(userMessage)
stream, errs := session.RunStreamInteraction(userMessage)
history, err := session.GetChatHistory()
```

For clients that send the full request shape, use the `Model_Request` methods:

```go
req := models.Model_Request{User_Message: &userMessage}
response, err := session.RunSingleInteractionWithRequest(req)
```

### SSE

Implement `SSEWriter` and call `RunSSEInteraction`:

```go
type Writer struct{}

func (Writer) WriteSSE(data string) error { return nil }
func (Writer) WriteSSEError(err error) error { return nil }
func (Writer) Flush() {}

err := session.RunSSEInteraction(userMessage, Writer{}, ctx)
```

### WebSocket

Use `AgentSession` when you have a `*websocket.Conn` and want the built-in WebSocket protocol, tool approval flow, frontend tool support, trace streaming, and optional TTS handling.

```go
session := godantic.NewAgentSession(
    "session-1",
    "user-1",
    conn,
    &agent,
    store,
    memoryManager,
)

if traceStore != nil {
    session.SetTraceStore(traceStore)
}

err := session.RunInteraction(req)
```

`memoryManager` can be nil. If provided, it must implement the session memory interface in `sessions/types.go`.

## Stores

SQLite is the easiest default:

```go
store, err := stores.NewSQLiteStoreSimple("chat.sqlite")
```

PostgreSQL is available by DSN or config:

```go
store, err := stores.NewPostgresStoreSimple("host=localhost user=app password=secret dbname=chat port=5432 sslmode=disable")
```

You can provide your own persistence by implementing `stores.MessageStore`.

## Tools

Tools are `models.FunctionDeclaration` values. Each declaration contains:

- `Name`: the tool name exposed to the model.
- `Description`: when the model should call it.
- `Parameters`: JSON schema for arguments.
- `Callable`: a Go function that returns `(string, error)`.

The callable can take no parameters, one parameter, or multiple typed parameters. `Agent.ExecuteTool` maps model-provided arguments into the Go function and returns a JSON string result.

### Built-In Tools

For the standard local-agent tools, use `common_tools.DefaultTools()`:

```go
import "github.com/Desarso/godantic/common_tools"

tools := common_tools.DefaultTools()
agent := godantic.Create_Agent(godantic.NewOpenRouterModel("openai/gpt-4o-mini"), tools)
```

For schema-backed built-ins, use `Create_Tools` with functions that have cached schemas in `schemas/cached_schemas`:

```go
tools, err := godantic.Create_Tools([]interface{}{
    common_tools.Brave_Search,
    common_tools.Web_Fetch,
    common_tools.Execute_TypeScript,
})
if err != nil {
    log.Fatal(err)
}
```

### Custom Tools

For application tools, the most portable path is to construct `models.FunctionDeclaration` directly:

```go
func GetWeather(city string) (string, error) {
    return "Sunny in " + city, nil
}

weatherTool := models.FunctionDeclaration{
    Name:        "get_weather",
    Description: "Get the current weather for a city.",
    Parameters: models.Parameters{
        Type: "object",
        Properties: map[string]interface{}{
            "city": map[string]interface{}{
                "type":        "string",
                "description": "City name",
            },
        },
        Required: []string{"city"},
    },
    Callable: GetWeather,
}

agent := godantic.Create_Agent(godantic.NewGeminiModel("gemini-2.0-flash"), []models.FunctionDeclaration{weatherTool})
```

For multi-argument functions, keep `Required` in the same order as the Go function parameters.

## Configuration Builder

`WSConfig` is a convenience builder for apps that wire controllers from config:

```go
config := godantic.NewWSConfig().
    WithOpenRouter("openai/gpt-4o-mini").
    WithSystemPrompt("You are concise and helpful.").
    WithSQLiteStore("chat.sqlite").
    WithTools([]interface{}{common_tools.Brave_Search}).
    WithTemperature(0.2).
    WithMaxTokens(2048)
```

Then create tools and an agent:

```go
tools, err := godantic.Create_Tools(config.Tools)
if err != nil {
    log.Fatal(err)
}

agent := godantic.Create_Agent_From_Config(config, tools)
```

## Request And Response Shape

A normal user request is a `models.Model_Request` with a `User_Message`:

```go
req := models.Model_Request{
    User_Message: &models.User_Message{
        Role: "user",
        Content: models.Content{Parts: []models.User_Part{{Text: "What can you do?"}}},
    },
}
```

Tool follow-up requests use `Tool_Results`:

```go
req := models.Model_Request{
    Tool_Results: &[]models.Tool_Result{{
        Tool_ID:     "call_123",
        Tool_Name:   "get_weather",
        Tool_Output: `{"result":"Sunny"}`,
    }},
}
```

Responses are `models.Model_Response` values containing parts. A part may be text, a function call, thinking/reasoning content, or provider-specific metadata.

## Testing

Run the module tests with:

```bash
go test ./...
```

## License

Apache-2.0. See `LICENSE`.
