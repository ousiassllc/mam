package protocol_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/catalog"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// These tests compare the messages mam uses with the JSON schema of the Codex version the catalog pins
// (testdata/schema.json, made by scripts/codex-schema-snapshot.sh): the app-server's JSON-RPC changes with the version, so
// mam uses only a version it was checked against. Bumping the catalog without refreshing the snapshot fails the first
// test; a field mam uses that the new schema dropped or renamed fails the others.

type snapshot struct {
	Tag                 string                    `json:"tag"`
	ClientRequests      []string                  `json:"clientRequests"`
	ServerRequests      []string                  `json:"serverRequests"`
	ServerNotifications []string                  `json:"serverNotifications"`
	Config              map[string]any            `json:"config"`
	Schemas             map[string]map[string]any `json:"schemas"`
}

func loadSnapshot(t *testing.T) snapshot {
	t.Helper()
	b, err := os.ReadFile("testdata/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var s snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSnapshotIsOfThePinnedVersion(t *testing.T) {
	if got, want := loadSnapshot(t).Tag, catalog.Default().Tag; got != want {
		t.Fatalf("the schema snapshot is of %s but the catalog pins %s: run scripts/codex-schema-snapshot.sh %s, then fix what the schema test reports",
			got, want, catalog.Default().Version)
	}
}

// resolve follows a $ref into the definitions of the schema file; a definition the snapshot dropped is empty.
func resolve(schema map[string]any, node map[string]any) map[string]any {
	for {
		ref, ok := node["$ref"].(string)
		if !ok {
			return node
		}
		defs, _ := schema["definitions"].(map[string]any)
		next, _ := defs[ref[strings.LastIndex(ref, "/")+1:]].(map[string]any)
		if next == nil {
			return map[string]any{}
		}
		node = next
	}
}

// variants are the object shapes a node can take: the node itself and, through oneOf / anyOf / allOf, its alternatives.
func variants(schema, node map[string]any) []map[string]any {
	node = resolve(schema, node)
	out := []map[string]any{node}
	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		subs, _ := node[key].([]any)
		for _, s := range subs {
			if m, ok := s.(map[string]any); ok {
				out = append(out, variants(schema, m)...)
			}
		}
	}
	return out
}

// find walks a path from the top of a schema file. A step is a property name, "[]" for the items of an array, or
// "type=<v>" to pick the variant whose "type" property is the enum <v>. It returns the variants of the node reached.
func find(t *testing.T, s snapshot, file string, path ...string) (schema map[string]any, objs []map[string]any) {
	t.Helper()
	schema = s.Schemas[file]
	if schema == nil {
		t.Fatalf("the snapshot has no %s", file)
	}
	objs = variants(schema, schema)
	for _, step := range path {
		var next []map[string]any
		for _, o := range objs {
			switch {
			case step == "[]":
				if items, ok := o["items"].(map[string]any); ok {
					next = append(next, variants(schema, items)...)
				}
			case strings.HasPrefix(step, "type="):
				if props, _ := o["properties"].(map[string]any); props != nil {
					typ, _ := props["type"].(map[string]any)
					if enum, _ := resolve(schema, typ)["enum"].([]any); len(enum) == 1 && enum[0] == strings.TrimPrefix(step, "type=") {
						next = append(next, o)
					}
				}
			default:
				if props, _ := o["properties"].(map[string]any); props != nil {
					if p, ok := props[step].(map[string]any); ok {
						next = append(next, variants(schema, p)...)
					}
				}
			}
		}
		objs = next
		if len(objs) == 0 {
			t.Fatalf("%s: nothing at %v", file, path)
		}
	}
	return schema, objs
}

func properties(objs []map[string]any) (props map[string]bool, required map[string]bool) {
	props, required = map[string]bool{}, map[string]bool{}
	for _, o := range objs {
		if p, _ := o["properties"].(map[string]any); p != nil {
			for k := range p {
				props[k] = true
			}
		}
		if r, _ := o["required"].([]any); r != nil {
			for _, k := range r {
				required[k.(string)] = true
			}
		}
	}
	return props, required
}

type jsonField struct {
	name      string
	omitEmpty bool
}

// fieldsOf are the JSON fields of a struct type (embedded and inline types are not used by the protocol structs).
func fieldsOf(typ reflect.Type) []jsonField {
	var out []jsonField
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		out = append(out, jsonField{name: name, omitEmpty: strings.Contains(opts, "omitempty")})
	}
	return out
}

// TestMessageFieldsExistInTheSchema: every JSON field of a message type is a property of the schema object it stands for,
// and a request type sends every property the schema requires.
func TestMessageFieldsExistInTheSchema(t *testing.T) {
	s := loadSnapshot(t)
	cases := []struct {
		name string
		typ  any
		file string
		path []string
		sent bool // mam sends it: it must always set what the schema requires
	}{
		{"InitializeParams", protocol.InitializeParams{}, "InitializeParams", nil, true},
		{"ClientInfo", protocol.ClientInfo{}, "InitializeParams", []string{"clientInfo"}, true},
		{"InitializeResult", protocol.InitializeResult{}, "InitializeResponse", nil, false},
		{"GetAccountParams", protocol.GetAccountParams{}, "GetAccountParams", nil, true},
		{"GetAccountResult", protocol.GetAccountResult{}, "GetAccountResponse", nil, false},
		{"Account", protocol.Account{}, "GetAccountResponse", []string{"account", "type=chatgpt"}, false},
		{"LoginParams(chatgpt)", protocol.LoginParams{}, "LoginAccountParams", []string{"type=chatgpt"}, true},
		{"LoginParams(device)", protocol.LoginParams{}, "LoginAccountParams", []string{"type=chatgptDeviceCode"}, true},
		{"LoginResult", protocol.LoginResult{}, "LoginAccountResponse", nil, false}, // the union of the browser and device answers
		{"CancelLoginParams", protocol.CancelLoginParams{}, "CancelLoginAccountParams", nil, true},
		{"LoginCompleted", protocol.LoginCompleted{}, "AccountLoginCompletedNotification", nil, false},
		{"AccountUpdated", protocol.AccountUpdated{}, "AccountUpdatedNotification", nil, false},
		{"RateLimitsResult", protocol.RateLimitsResult{}, "GetAccountRateLimitsResponse", nil, false},
		{"RateLimitsUpdated", protocol.RateLimitsUpdated{}, "AccountRateLimitsUpdatedNotification", nil, false},
		{"RateLimitSnapshot", protocol.RateLimitSnapshot{}, "GetAccountRateLimitsResponse", []string{"rateLimits"}, false},
		{"RateLimitWindow", protocol.RateLimitWindow{}, "GetAccountRateLimitsResponse", []string{"rateLimits", "primary"}, false},
		{"ModelListParams", protocol.ModelListParams{}, "ModelListParams", nil, true},
		{"ModelListResult", protocol.ModelListResult{}, "ModelListResponse", nil, false},
		{"Model", protocol.Model{}, "ModelListResponse", []string{"data", "[]"}, false},
		{"ThreadStartParams", protocol.ThreadStartParams{}, "ThreadStartParams", nil, true},
		{"ThreadStartResult", protocol.ThreadStartResult{}, "ThreadStartResponse", nil, false},
		{"ThreadStartResult.thread", protocol.ThreadStartResult{}.Thread, "ThreadStartResponse", []string{"thread"}, false},
		{"ThreadRef", protocol.ThreadRef{}, "ThreadUnsubscribeParams", nil, true},
		{"TextInput", protocol.TextInput{}, "TurnStartParams", []string{"input", "[]", "type=text"}, true},
		{"LocalImageInput", protocol.LocalImageInput{}, "TurnStartParams", []string{"input", "[]", "type=localImage"}, true},
		{"SandboxPolicy", protocol.SandboxPolicy{}, "TurnStartParams", []string{"sandboxPolicy", "type=readOnly"}, true},
		{"TurnStartParams", protocol.TurnStartParams{}, "TurnStartParams", nil, true},
		{"TurnInterruptParams", protocol.TurnInterruptParams{}, "TurnInterruptParams", nil, true},
		{"AgentDelta", protocol.AgentDelta{}, "AgentMessageDeltaNotification", nil, false},
		{"ItemCompleted", protocol.ItemCompleted{}, "ItemCompletedNotification", nil, false},
		{"Item", protocol.Item{}, "ItemCompletedNotification", []string{"item", "type=agentMessage"}, false},
		{"TurnCompletedNotification", protocol.TurnCompletedNotification{}, "TurnCompletedNotification", nil, false},
		{"TurnResult", protocol.TurnResult{}, "TurnCompletedNotification", []string{"turn"}, false},
		{"TurnError", protocol.TurnError{}, "TurnCompletedNotification", []string{"turn", "error"}, false},
		{"ErrorNotification", protocol.ErrorNotification{}, "ErrorNotification", nil, false},
		{"ErrorNotification.error", protocol.TurnError{}, "ErrorNotification", []string{"error"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			typ := reflect.TypeOf(c.typ)
			_, objs := find(t, s, c.file, c.path...)
			props, required := properties(objs)
			sent := map[string]jsonField{}
			for _, f := range fieldsOf(typ) {
				sent[f.name] = f
				if !props[f.name] {
					t.Errorf("%s has the field %q, which %s %v does not", typ.Name(), f.name, c.file, c.path)
				}
			}
			if !c.sent {
				return
			}
			for name := range required {
				if f, ok := sent[name]; !ok || f.omitEmpty {
					t.Errorf("%s %v requires %q, which %s does not always send", c.file, c.path, name, typ.Name())
				}
			}
		})
	}
}
