// Package llm is a small, provider-agnostic client for OpenAI-compatible
// chat-completions APIs (Ollama, Groq, OpenAI, OpenRouter, Together, ...).
//
// It deliberately uses only the standard library so it reads as a clear
// reference for how an LLM API call is actually shaped: a JSON POST with
// messages, model, temperature and max_tokens, and a JSON (or SSE) response.
package llm

// Role is a chat message role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a single chat message. For tool results, set Role=RoleTool,
// ToolCallID to the call being answered, and Content to the tool's output.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// Tool describes a function the model may call (OpenAI-compatible).
type Tool struct {
	Type     string       `json:"type"` // always "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction is the function schema exposed to the model.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema of the arguments
}

// ToolCall is a function invocation the model asked for.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string of arguments
	} `json:"function"`
}

// ChatRequest is the input to a chat completion.
type ChatRequest struct {
	Messages    []Message `json:"messages"`
	Model       string    `json:"model,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
	// ResponseFormat, when set, constrains the model's output. Use
	// NewJSONSchemaFormat to force a specific JSON shape (structured outputs).
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	// Tools, when set, are the functions the model may call (Phase 3).
	Tools []Tool `json:"tools,omitempty"`
}

// ResponseFormat controls the shape of the model's reply (OpenAI-compatible).
// Type is "json_object" for any-valid-JSON, or "json_schema" for a schema-
// constrained object.
type ResponseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

// JSONSchema names a schema the output must conform to. Schema is a raw JSON
// Schema object (map). Strict asks the provider to reject non-conforming output.
type JSONSchema struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict,omitempty"`
}

// NewJSONSchemaFormat builds a strict json_schema response format from a raw
// JSON Schema object. Use it to make the model return exactly your shape.
func NewJSONSchemaFormat(name string, schema map[string]any) *ResponseFormat {
	return &ResponseFormat{
		Type:       "json_schema",
		JSONSchema: &JSONSchema{Name: name, Schema: schema, Strict: true},
	}
}

// Usage reports token consumption for a request (when the provider returns it).
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse is the non-streaming result.
type ChatResponse struct {
	Content   string     `json:"content"`
	Model     string     `json:"model"`
	Usage     Usage      `json:"usage"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// --- wire types (OpenAI-compatible JSON shapes) ---

type wireRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	Stream         bool            `json:"stream"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	Tools          []Tool          `json:"tools,omitempty"`
}

type wireChoice struct {
	Message struct {
		Content   string     `json:"content"`
		ToolCalls []ToolCall `json:"tool_calls"`
	} `json:"message"`
	Delta struct {
		Content string `json:"content"`
	} `json:"delta"`
	FinishReason string `json:"finish_reason"`
}

type wireResponse struct {
	Model   string       `json:"model"`
	Choices []wireChoice `json:"choices"`
	Usage   Usage        `json:"usage"`
	Error   *wireError   `json:"error"`
}

type wireError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}
