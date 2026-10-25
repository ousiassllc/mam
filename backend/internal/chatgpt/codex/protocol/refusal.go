package protocol

// The answers mam gives to the requests of the server. mam allows nothing: no command, no file change, no extra
// permission, no tool, no question to the user, no token (mam never supplies one, so a refresh request is an error).

// DecisionResponse answers an approval request (commands, file changes, the legacy patch and exec approvals).
type DecisionResponse struct {
	Decision string `json:"decision"`
}

// ActionResponse answers an MCP elicitation.
type ActionResponse struct {
	Action string `json:"action"`
}

// PermissionsResponse answers a request for more permissions: an empty grant.
type PermissionsResponse struct {
	Permissions struct{} `json:"permissions"`
	Scope       string   `json:"scope"`
}

// AnswersResponse answers a question of a tool to the user: no answer.
type AnswersResponse struct {
	Answers map[string]any `json:"answers"`
}

// DynamicToolResponse answers a call of a tool that mam never registered: a failure with no output.
type DynamicToolResponse struct {
	ContentItems []any `json:"contentItems"`
	Success      bool  `json:"success"`
}

// Decline values.
const (
	DecisionDecline     = "decline" // item/commandExecution and item/fileChange
	DecisionAbort       = "abort"   // the legacy applyPatchApproval and execCommandApproval (ReviewDecision)
	ActionDecline       = "decline"
	PermissionScopeTurn = "turn"
)

// Refusal is the answer to the request method of the server. ok is false for a request mam does not know (and for
// the ones it can only fail: account/chatgptAuthTokens/refresh, attestation/generate), which are answered with an error.
func Refusal(method string) (answer any, ok bool) {
	switch method {
	case ReqCommandApproval, ReqFileChangeApproval:
		return DecisionResponse{Decision: DecisionDecline}, true
	case ReqLegacyPatchApproval, ReqLegacyExecApproval:
		return DecisionResponse{Decision: DecisionAbort}, true
	case ReqPermissionsApproval:
		return PermissionsResponse{Scope: PermissionScopeTurn}, true
	case ReqToolUserInput:
		return AnswersResponse{Answers: map[string]any{}}, true
	case ReqMCPElicitation:
		return ActionResponse{Action: ActionDecline}, true
	case ReqDynamicToolCall:
		return DynamicToolResponse{ContentItems: []any{}}, true
	}
	return nil, false
}

// IsPlainItem reports whether a thread item type is part of a plain text turn: the echo of the user's message, the text
// and the reasoning of the model, its plan, and the bookkeeping of the context. Every other type (and any type a later
// version adds) is the use of a tool, which a text turn must not have.
func IsPlainItem(itemType string) bool {
	switch itemType {
	case "userMessage", "agentMessage", "reasoning", "plan", "hookPrompt", "contextCompaction":
		return true
	}
	return false
}
