package translate

type openAIRequest struct {
	Model          string             `json:"model"`
	Messages       []openAIMessage    `json:"messages"`
	MaxTokens      *int               `json:"max_tokens,omitempty"`
	Temperature    *float64           `json:"temperature,omitempty"`
	TopP           *float64           `json:"top_p,omitempty"`
	Stop           any                `json:"stop,omitempty"`
	Stream         bool               `json:"stream,omitempty"`
	StreamOptions  *openAIStreamOpts  `json:"stream_options,omitempty"`
	Tools          []openAITool       `json:"tools,omitempty"`
	ToolChoice     any                `json:"tool_choice,omitempty"`
}

type openAIStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIMessage struct {
	Role       string          `json:"role"`
	Content    any             `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	Index    int                   `json:"index"`
	ID       string                `json:"id,omitempty"`
	Type     string                `json:"type,omitempty"`
	Function openAIToolCallFunc    `json:"function"`
}

type openAIToolCallFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type anthropicMessage struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMsgItem `json:"messages"`
	System    any                `json:"system,omitempty"`
	Stream    bool               `json:"stream,omitempty"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	ToolChoice any               `json:"tool_choice,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
	TopP     *float64            `json:"top_p,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
}

type anthropicMsgItem struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

type openAIChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage,omitempty"`
}

type openAIChoice struct {
	Index        int         `json:"index"`
	Delta        openAIDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type openAIDelta struct {
	Role             string          `json:"role,omitempty"`
	Content          string          `json:"content,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ToolCalls        []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
