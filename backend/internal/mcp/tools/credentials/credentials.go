// Package credentials holds the MCP tools of the API keys (the API キー section of the settings screen).
// No tool returns a key: a read gives whether a key is set and the last characters of it.
package credentials

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc.Credentials, keys: r.Credentials}
	mcpkit.Read(r, "list_credentials",
		"外部モデルの API キーの登録状況（設定済みか・末尾数文字）。キーの値は返さない / Which providers have an API key, with the last characters only. Never returns a key.",
		h.list)
	mcpkit.Write(r, "set_credential",
		"API キーを登録または置き換える。OS のセキュアストレージに保存され、再起動なしで効く / Store or replace the API key of a provider (OS secure storage, effective at once).",
		h.set, mcpkit.Idempotent())
	mcpkit.Destroy(r, "delete_credential",
		"API キーを削除する（未設定でもエラーにならない）/ Delete the API key of a provider (a missing key is not an error).",
		h.remove, mcpkit.Idempotent())
}

type handlers struct {
	svc  *service.CredentialService
	keys credential.Store
}

// ListInput has no field.
type ListInput struct{}

// Credential is the state of one provider.
type Credential struct {
	Provider   string `json:"provider" jsonschema:"set_credential に渡すプロバイダー ID"`
	Label      string `json:"label"`
	Configured bool   `json:"configured"`
	LastChars  string `json:"last_chars,omitempty" jsonschema:"キー末尾の数文字（設定済みのときだけ。短いキーでは空）"`
}

// ListOutput is every provider the settings screen manages.
type ListOutput struct {
	Credentials []Credential `json:"credentials"`
}

func (h handlers) list(_ context.Context, _ ListInput) (ListOutput, error) {
	statuses, err := h.svc.Status()
	if err != nil {
		return ListOutput{}, err
	}
	out := ListOutput{Credentials: make([]Credential, 0, len(statuses))}
	for _, s := range statuses {
		c := Credential{Provider: s.Provider, Label: s.Label, Configured: s.Configured}
		if s.Configured {
			if c.LastChars, err = credential.Hint(h.keys, s.Provider); err != nil {
				return ListOutput{}, err
			}
		}
		out.Credentials = append(out.Credentials, c)
	}
	return out, nil
}

// SetInput is the key to store.
type SetInput struct {
	Provider string `json:"provider" jsonschema:"list_credentials が返す provider（anthropic, openai, kling, ...）"`
	APIKey   string `json:"api_key" jsonschema:"保存する API キー。応答には含まれない"`
}

// SetOutput is the provider after the change. The key is not echoed.
type SetOutput struct {
	Credential
}

func (h handlers) set(ctx context.Context, in SetInput) (SetOutput, error) {
	if err := h.svc.Save(in.Provider, in.APIKey); err != nil {
		return SetOutput{}, err
	}
	return h.one(ctx, in.Provider)
}

// DeleteInput names the provider.
type DeleteInput struct {
	Provider string `json:"provider" jsonschema:"list_credentials が返す provider"`
}

func (h handlers) remove(ctx context.Context, in DeleteInput) (SetOutput, error) {
	if err := h.svc.Remove(in.Provider); err != nil {
		return SetOutput{}, err
	}
	return h.one(ctx, in.Provider)
}

func (h handlers) one(ctx context.Context, provider string) (SetOutput, error) {
	all, err := h.list(ctx, ListInput{})
	if err != nil {
		return SetOutput{}, err
	}
	for _, c := range all.Credentials {
		if c.Provider == provider {
			return SetOutput{c}, nil
		}
	}
	return SetOutput{}, service.ErrNotFound
}
