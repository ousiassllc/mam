package modelmgr

import (
	"context"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

func registerExtras(r *mcpkit.Registrar, h handlers) {
	mcpkit.Read(r, "get_huggingface_token_status",
		"Hugging Face のトークンが設定されているかと末尾の数文字（値は返さない）/ Whether a Hugging Face token is set, with its last characters. Never returns the token.",
		h.tokenStatus)
	mcpkit.Write(r, "set_huggingface_token",
		"Hugging Face のトークンを保存または置き換える（ログインが要るモデルの取得に使う: カタログの requires_hf_token のモデル（LTX-2.5）と gated のリポジトリ。ほかのカタログのモデルは不要）。OS のセキュアストレージに保存され、応答には含まれない / Store the optional Hugging Face token (never echoed).",
		h.setToken, mcpkit.Idempotent())
	mcpkit.Destroy(r, "clear_huggingface_token",
		"保存した Hugging Face のトークンを削除する（未設定でもエラーにならない）/ Remove the stored token.",
		h.clearToken, mcpkit.Idempotent())
	mcpkit.Read(r, "get_external_inventory",
		"外部の ComfyUI（登録済みの接続先。tailnet 上や ComfyUI Desktop など）が持つモデルファイル（checkpoints・diffusion_models・text_encoders・vae・loras）の一覧と、カタログのモデルに足りないファイル。成人向けのモデルは、年齢確認（set_adult_content）が済むまで含めない。読むだけで、外部の ComfyUI にモデルは導入しない / Read the model files of an external ComfyUI endpoint and what each catalog model lacks there (adult models are left out until the age confirmation).",
		h.externalInventory, mcpkit.OpenWorld())
}

// TokenStatus is the state of the token; the token itself is never in it.
type TokenStatus struct {
	Configured bool   `json:"configured"`
	LastChars  string `json:"last_chars,omitempty" jsonschema:"末尾の数文字（設定済みで、十分に長いときだけ）"`
}

func (h handlers) status(m *modelmanager.Service) (TokenStatus, error) {
	configured, last, err := m.HuggingFaceTokenStatus()
	return TokenStatus{Configured: configured, LastChars: last}, err
}

func (h handlers) tokenStatus(_ context.Context, _ Input) (TokenStatus, error) {
	m, err := h.mm()
	if err != nil {
		return TokenStatus{}, err
	}
	return h.status(m)
}

// TokenInput is the token to store.
type TokenInput struct {
	Token string `json:"token" jsonschema:"Hugging Face のアクセストークン（hf_…）。応答には含まれない"`
}

func (h handlers) setToken(_ context.Context, in TokenInput) (TokenStatus, error) {
	m, err := h.mm()
	if err != nil {
		return TokenStatus{}, err
	}
	if err := m.SetHuggingFaceToken(in.Token); err != nil {
		return TokenStatus{}, MapError(err)
	}
	return h.status(m)
}

func (h handlers) clearToken(_ context.Context, _ Input) (TokenStatus, error) {
	m, err := h.mm()
	if err != nil {
		return TokenStatus{}, err
	}
	if err := m.ClearHuggingFaceToken(); err != nil {
		return TokenStatus{}, MapError(err)
	}
	return h.status(m)
}

// InventoryInput names the external endpoint.
type InventoryInput struct {
	Endpoint string `json:"endpoint" jsonschema:"種類 ComfyUI の接続先の名前（list_local_endpoints）。同梱の bundled-comfyui は対象外"`
}

func (h handlers) externalInventory(ctx context.Context, in InventoryInput) (modelmanager.ExternalInventory, error) {
	m, err := h.mm()
	if err != nil {
		return modelmanager.ExternalInventory{}, err
	}
	inv, err := m.ExternalInventory(ctx, in.Endpoint)
	if kind, _ := mmview.Refuse(err); err != nil && kind == mmview.RefusalNone {
		return inv, &mcpkit.ToolError{Code: mcpkit.CodeUpstream, Message: "ComfyUI に接続できませんでした。URL・起動の状態・API キーを確かめてください", Status: http.StatusBadGateway}
	}
	return inv, MapError(err)
}
