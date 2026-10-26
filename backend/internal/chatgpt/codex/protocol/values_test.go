package protocol_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// enums collects every enum string below a schema file (resolved definitions included: the whole file).
func enums(node any, out map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		if e, ok := v["enum"].([]any); ok {
			for _, x := range e {
				if s, ok := x.(string); ok {
					out[s] = true
				}
			}
		}
		for k, x := range v {
			if k != "enum" {
				enums(x, out)
			}
		}
	case []any:
		for _, x := range v {
			enums(x, out)
		}
	}
}

// TestValuesMamSendsAndReadsExistInTheSchema: the constants of the protocol (login types, policies, statuses, error
// kinds, item types) are values the pinned schema lists.
func TestValuesMamSendsAndReadsExistInTheSchema(t *testing.T) {
	s := loadSnapshot(t)
	cases := []struct {
		file   string
		values []string
	}{
		{"LoginAccountParams", []string{protocol.LoginBrowser, protocol.LoginDeviceCode}},
		{"LoginAccountResponse", []string{protocol.LoginBrowser, protocol.LoginDeviceCode}},
		{"GetAccountResponse", []string{protocol.AccountTypeChatGPT}},
		{"ThreadStartParams", []string{protocol.ApprovalNever, protocol.SandboxReadOnly}},
		{"TurnStartParams", []string{protocol.ApprovalNever, protocol.PolicyReadOnly, protocol.InputText, protocol.InputLocalImage}},
		{"TurnCompletedNotification", []string{
			protocol.TurnCompleted, protocol.TurnFailed, protocol.TurnInterrupted,
			protocol.ErrContextWindowExceeded, protocol.ErrUsageLimitExceeded, protocol.ErrUnauthorized, protocol.ErrBadRequest,
			protocol.ErrSandboxError, protocol.ErrInternalServerError, protocol.ErrOther, protocol.ErrServerOverloaded,
			protocol.ErrRateLimitExceeded,
		}},
		{"ItemCompletedNotification", []string{protocol.ItemAgentMessage, protocol.PhaseFinalAnswer}},
		{"ModelListResponse", []string{protocol.ModalityImage}},
	}
	for _, c := range cases {
		all := map[string]bool{}
		enums(s.Schemas[c.file], all)
		for _, v := range c.values {
			if !all[v] {
				t.Errorf("%s does not list %q", c.file, v)
			}
		}
	}
	// The error kinds that carry data are keys of an object, not enum values.
	_, objs := find(t, s, "TurnCompletedNotification", "turn", "error", "codexErrorInfo")
	keys := map[string]bool{}
	for _, o := range objs {
		props, _ := properties([]map[string]any{o})
		for k := range props {
			keys[k] = true
		}
	}
	for _, k := range []string{protocol.ErrHTTPConnectionFailed, protocol.ErrResponseStreamConnectFailed, protocol.ErrResponseStreamDisconnected, protocol.ErrResponseTooManyFailedAttempt} {
		if !keys[k] {
			t.Errorf("codexErrorInfo has no %q variant", k)
		}
	}
}

// TestMethodsExistInTheSchema: every request mam sends is a method of the client requests, every notification it reads
// one of the server notifications, and every request it answers one of the server requests.
func TestMethodsExistInTheSchema(t *testing.T) {
	s := loadSnapshot(t)
	for _, m := range []string{
		protocol.MethodInitialize, protocol.MethodAccountRead, protocol.MethodLoginStart, protocol.MethodLoginCancel,
		protocol.MethodLogout, protocol.MethodRateLimitsRead, protocol.MethodModelList, protocol.MethodThreadStart,
		protocol.MethodThreadUnsubscribe, protocol.MethodTurnStart, protocol.MethodTurnInterrupt,
	} {
		if !slices.Contains(s.ClientRequests, m) {
			t.Errorf("%q is not a client request", m)
		}
	}
	for _, m := range []string{
		protocol.NotifLoginCompleted, protocol.NotifAccountUpdated, protocol.NotifRateLimits, protocol.NotifAgentDelta,
		protocol.NotifItemCompleted, protocol.NotifTurnCompleted, protocol.NotifError,
	} {
		if !slices.Contains(s.ServerNotifications, m) {
			t.Errorf("%q is not a server notification", m)
		}
	}
	for _, m := range []string{
		protocol.ReqCommandApproval, protocol.ReqFileChangeApproval, protocol.ReqPermissionsApproval, protocol.ReqToolUserInput,
		protocol.ReqMCPElicitation, protocol.ReqDynamicToolCall, protocol.ReqLegacyPatchApproval, protocol.ReqLegacyExecApproval,
		protocol.ReqAuthTokensRefresh, protocol.ReqAttestation,
	} {
		if !slices.Contains(s.ServerRequests, m) {
			t.Errorf("%q is not a server request", m)
		}
	}
}

// TestNoMessageTypeCanCarryAToken: no JSON field of the protocol types is named like a token, so a token the server
// sends is never decoded and one can never be sent back.
func TestNoMessageTypeCanCarryAToken(t *testing.T) {
	for _, v := range []any{
		protocol.InitializeParams{}, protocol.InitializeResult{}, protocol.GetAccountParams{}, protocol.GetAccountResult{},
		protocol.Account{}, protocol.LoginParams{}, protocol.LoginResult{}, protocol.LoginCompleted{}, protocol.AccountUpdated{},
		protocol.RateLimitsResult{}, protocol.Model{}, protocol.ThreadStartParams{}, protocol.TurnStartParams{},
		protocol.Item{}, protocol.TurnError{},
	} {
		typ := reflect.TypeOf(v)
		for i, f := range fieldsOf(typ) {
			if typ.Field(i).Type.Kind() == reflect.Bool {
				continue // GetAccountParams.refreshToken is a flag, always false
			}
			if n := strings.ToLower(f.name); strings.Contains(n, "token") || strings.Contains(n, "secret") || strings.Contains(n, "apikey") {
				t.Errorf("%T has the field %q", v, f.name)
			}
		}
	}
}

// TestConfigKeysAndValuesExistInTheConfigSchema: the -c settings mam passes are keys of the config schema, with values it lists.
func TestConfigKeysAndValuesExistInTheConfigSchema(t *testing.T) {
	s := loadSnapshot(t)
	keys, _ := s.Config["keys"].([]any)
	for _, k := range []string{protocol.ConfigCredentialsStore, protocol.ConfigWebSearch} {
		if !slices.Contains(keys, any(k)) {
			t.Errorf("config schema has no key %q", k)
		}
	}
	for mode, values := range map[string][]string{
		"AuthCredentialsStoreMode": {protocol.CredentialsKeyring, protocol.CredentialsAuto},
		"WebSearchMode":            {protocol.WebSearchDisabled},
	} {
		all := map[string]bool{}
		enums(s.Config[mode], all)
		for _, v := range values {
			if !all[v] {
				t.Errorf("%s does not list %q", mode, v)
			}
		}
	}
}

// TestToolSettingsAreKnownConfigKeys: every setting of ToolSettings names a key of the config schema (a misspelled or
// removed one would be ignored, or refused, by the app-server).
func TestToolSettingsAreKnownConfigKeys(t *testing.T) {
	s := loadSnapshot(t)
	features, _ := s.Config["features"].([]any)
	for _, setting := range protocol.ToolSettings {
		key, _, _ := strings.Cut(setting, "=")
		if name, ok := strings.CutPrefix(key, "features."); ok {
			if !slices.Contains(features, any(name)) {
				t.Errorf("the config schema has no feature %q", name)
			}
		} else if keys, _ := s.Config["keys"].([]any); !slices.Contains(keys, any(key)) {
			t.Errorf("the config schema has no key %q", key)
		}
	}
	if !slices.Contains(protocol.ToolSettings, `web_search="disabled"`) {
		t.Error("web search must be switched off")
	}
}
