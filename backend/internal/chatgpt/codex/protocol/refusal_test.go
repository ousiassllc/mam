package protocol_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// TestRefusalsAreValidAnswersAndAllowNothing: every answer mam gives to a request of the server has fields and values the
// pinned schema of that answer lists, and none of them approves anything.
func TestRefusalsAreValidAnswersAndAllowNothing(t *testing.T) {
	s := loadSnapshot(t)
	cases := []struct {
		method string
		file   string
		typ    any
	}{
		{protocol.ReqCommandApproval, "CommandExecutionRequestApprovalResponse", protocol.DecisionResponse{}},
		{protocol.ReqFileChangeApproval, "FileChangeRequestApprovalResponse", protocol.DecisionResponse{}},
		{protocol.ReqLegacyPatchApproval, "ApplyPatchApprovalResponse", protocol.DecisionResponse{}},
		{protocol.ReqLegacyExecApproval, "ExecCommandApprovalResponse", protocol.DecisionResponse{}},
		{protocol.ReqPermissionsApproval, "PermissionsRequestApprovalResponse", protocol.PermissionsResponse{}},
		{protocol.ReqToolUserInput, "ToolRequestUserInputResponse", protocol.AnswersResponse{}},
		{protocol.ReqMCPElicitation, "McpServerElicitationRequestResponse", protocol.ActionResponse{}},
		{protocol.ReqDynamicToolCall, "DynamicToolCallResponse", protocol.DynamicToolResponse{}},
	}
	approving := []string{"accept", "acceptForSession", "approved", "approved_for_session", "acceptWithExecpolicyAmendment"}
	for _, c := range cases {
		t.Run(c.method, func(t *testing.T) {
			answer, ok := protocol.Refusal(c.method)
			if !ok {
				t.Fatalf("no refusal for %s", c.method)
			}
			if reflect.TypeOf(answer) != reflect.TypeOf(c.typ) {
				t.Fatalf("answer is a %T, want %T", answer, c.typ)
			}
			_, objs := find(t, s, c.file)
			props, required := properties(objs)
			sent := map[string]bool{}
			for _, f := range fieldsOf(reflect.TypeOf(c.typ)) {
				sent[f.name] = true
				if !props[f.name] {
					t.Errorf("%T has the field %q, which %s does not", c.typ, f.name, c.file)
				}
			}
			for name := range required {
				if !sent[name] {
					t.Errorf("%s requires %q", c.file, name)
				}
			}
			all := map[string]bool{}
			enums(s.Schemas[c.file], all)
			b, _ := json.Marshal(answer)
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			for _, key := range []string{"decision", "action"} {
				v, has := m[key].(string)
				if !has {
					continue
				}
				if !all[v] {
					t.Errorf("%s does not list %s %q", c.file, key, v)
				}
				if slices.Contains(approving, v) {
					t.Errorf("%s %q approves", key, v)
				}
			}
		})
	}
	for _, m := range []string{protocol.ReqAuthTokensRefresh, protocol.ReqAttestation, "item/unknown"} {
		if _, ok := protocol.Refusal(m); ok {
			t.Errorf("%s must be failed with an error, not answered", m)
		}
	}
}

func TestPlainItemsAreTheTextOnesAndToolsAreNot(t *testing.T) {
	for _, plain := range []string{"agentMessage", "reasoning", "userMessage", "plan", "contextCompaction"} {
		if !protocol.IsPlainItem(plain) {
			t.Errorf("%s is plain", plain)
		}
	}
	for _, tool := range []string{"commandExecution", "fileChange", "mcpToolCall", "webSearch", "dynamicToolCall", "imageGeneration", "somethingNew"} {
		if protocol.IsPlainItem(tool) {
			t.Errorf("%s is a tool", tool)
		}
	}
}
