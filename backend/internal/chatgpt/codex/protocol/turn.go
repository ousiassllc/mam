package protocol

// ModelListParams is the params of model/list.
type ModelListParams struct {
	Cursor *string `json:"cursor,omitempty"`
}

// Model is a model the account can use. Model is the name to give to thread/start.
type Model struct {
	ID              string   `json:"id"`
	Model           string   `json:"model"`
	DisplayName     string   `json:"displayName"`
	Hidden          bool     `json:"hidden"`
	IsDefault       bool     `json:"isDefault"`
	InputModalities []string `json:"inputModalities,omitempty"`
}

// ModelListResult is the answer of model/list.
type ModelListResult struct {
	Data       []Model `json:"data"`
	NextCursor *string `json:"nextCursor"`
}

// ThreadStartParams is the params of thread/start: an ephemeral (never written to disk), read-only thread that asks
// for no approval, in an empty working directory. The system prompt travels in BaseInstructions or DeveloperInstructions
// (one is set; which one is the Instructions switch of the codex package).
type ThreadStartParams struct {
	Model                 string  `json:"model,omitempty"`
	Ephemeral             bool    `json:"ephemeral"`
	BaseInstructions      *string `json:"baseInstructions,omitempty"`
	DeveloperInstructions *string `json:"developerInstructions,omitempty"`
	Cwd                   string  `json:"cwd,omitempty"`
	Sandbox               string  `json:"sandbox,omitempty"`
	ApprovalPolicy        string  `json:"approvalPolicy,omitempty"`
}

// ThreadStartResult is the answer of thread/start.
type ThreadStartResult struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
}

// ThreadRef is the params of thread/unsubscribe.
type ThreadRef struct {
	ThreadID string `json:"threadId"`
}

// TextInput is a text item of the input of a turn.
type TextInput struct {
	Type string `json:"type"` // InputText
	Text string `json:"text"`
}

// LocalImageInput is an image item of the input of a turn: a file on this machine.
type LocalImageInput struct {
	Type string `json:"type"` // InputLocalImage
	Path string `json:"path"`
}

// SandboxPolicy is the sandboxPolicy of turn/start.
type SandboxPolicy struct {
	Type string `json:"type"`
}

// TurnStartParams is the params of turn/start. The policies are set again here so that a turn can never be
// more permissive than the thread.
type TurnStartParams struct {
	ThreadID       string         `json:"threadId"`
	Input          []any          `json:"input"` // TextInput and LocalImageInput
	ApprovalPolicy string         `json:"approvalPolicy,omitempty"`
	SandboxPolicy  *SandboxPolicy `json:"sandboxPolicy,omitempty"`
}

// TurnRef is the turn of an answer.
type TurnRef struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// TurnStartResult is the answer of turn/start.
type TurnStartResult struct {
	Turn TurnRef `json:"turn"`
}

// TurnInterruptParams is the params of turn/interrupt.
type TurnInterruptParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

// AgentDelta is the notification item/agentMessage/delta.
type AgentDelta struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

// Item is a thread item: mam reads the agent messages only (the other types are tool use, which it never allows).
type Item struct {
	Type  string  `json:"type"`
	ID    string  `json:"id"`
	Text  string  `json:"text"`
	Phase *string `json:"phase"`
}

// ItemCompleted is the notification item/completed.
type ItemCompleted struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Item     Item   `json:"item"`
}

// TurnError is the error of a failed turn.
type TurnError struct {
	Message        string    `json:"message"`
	CodexErrorInfo ErrorInfo `json:"codexErrorInfo"`
}

// TurnResult is the turn of a turn/completed.
type TurnResult struct {
	ID     string     `json:"id"`
	Status string     `json:"status"`
	Error  *TurnError `json:"error"`
}

// TurnCompletedNotification is the notification turn/completed.
type TurnCompletedNotification struct {
	ThreadID string     `json:"threadId"`
	Turn     TurnResult `json:"turn"`
}

// ErrorNotification is the notification error. WillRetry is true while the server retries by itself.
type ErrorNotification struct {
	ThreadID  string    `json:"threadId"`
	TurnID    string    `json:"turnId"`
	WillRetry bool      `json:"willRetry"`
	Error     TurnError `json:"error"`
}
