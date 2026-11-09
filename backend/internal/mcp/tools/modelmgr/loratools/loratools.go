// Package loratools holds the MCP side of "LoRA を追加" of the model manager (Issue #1119): the upload purpose model_lora_import (create_upload_ticket, then
// one multipart POST of the file, like every other file the tools take in) and the tools list_user_loras, update_user_lora and delete_user_lora
// (spec/api/mcp-tools-model-manager.md, spec/architecture/model-manager/model-manager.md §14). It is a package of its own because the modelmgr package is at
// its size limit. The tools call the same modelmanager.Service (LoRAs) as the screen, so the same checks hold: a .safetensors file only, the header and the
// tensors of a LoRA, the size limit, a name that is taken is refused and never overwritten.
package loratools

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/lorafamily"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

// Purpose is the upload purpose of a LoRA file.
const Purpose = "model_lora_import"

// Register adds the tools and the upload purpose. They answer "unavailable" when the app has no model manager (svc.ModelManager is nil).
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc}
	r.Files.RegisterUpload(mcpkit.UploadSpec{
		Purpose: Purpose,
		Description: "手元の LoRA（.safetensors）を、モデルマネージャーの管理する loras フォルダへ取り込む（ローカルへのコピー。ネットへは送らない。取り込んだだけでは生成は始まらない）。" +
			"形式（ヘッダー・LoRA のテンソル）と大きさを確かめ、同じ名前のファイルは上書きせず validation で断る（別の name を付けてやり直す）。" +
			"結果は list_user_loras と同じ 1 件。対応するベースモデル・強さ・トリガー語・ライセンスは、与えた値だけを記録する（ファイルの情報を取り込むのは adopt_metadata=true のときのベースモデルとトリガー語だけ）。" +
			"ライセンスを与えなければ「不明」で、商用OKにはならない / Add a LoRA .safetensors from this PC to the loras folder.",
		MaxBytes: lorafile.MaxBytes,
		Params: []mcpkit.UploadParam{
			{Name: "name", Description: "loras フォルダでのファイル名（英数字と . _ + - の .safetensors）。省略でアップロードしたファイル名（使えない文字は _ に置き換える）"},
			{Name: "base_models", Description: "対応するベースモデル（カンマ区切り）: " + familyKeys() + "。省略は未設定"},
			{Name: "strength", Description: "推奨の強さ（0 より大きく 1 以下）。省略は未設定"},
			{Name: "trigger_words", Description: "トリガー語（カンマ区切り）。省略は未設定"},
			{Name: "license_name", Description: "ライセンスの名前（任意）。省略は不明"},
			{Name: "commercial", Description: "商用利用の答え: conditional（配布元で商用可と確認した自己申告）/ no（商用NG）。省略は不明。商用OK（yes）にはできない"},
			{Name: "adopt_metadata", Description: "true: ファイルの __metadata__ が書くベースモデルとトリガー語を、上で与えなかったときに記録する"},
		},
		Handle: h.upload,
	})
	mcpkit.Read(r, "list_user_loras",
		"利用者が手元から追加した LoRA の一覧（ファイル名・パス・SHA-256・大きさ・対応するベースモデル・推奨の強さ・トリガー語・ライセンスと商用利用の答え・追加日時・ファイルが今あるか）。"+
			"未設定の項目は空で、ライセンスを入れていなければ commercial は unknown。生成テストで使うときは、この name を list_generation_models の loras と同じ名前で lora.name に渡す / The LoRAs the user added from their own PC.",
		h.list)
	mcpkit.Write(r, "update_user_lora",
		"追加した LoRA の、対応するベースモデル・推奨の強さ・トリガー語・ライセンスを置き換える（ファイルには触れない。渡さない項目は未設定になる）。path は list_user_loras の path。ユーザーが指示したときだけ呼ぶ / Replace what is recorded of an added LoRA.",
		h.update, mcpkit.Idempotent())
	mcpkit.Destroy(r, "delete_user_lora",
		"追加した LoRA のファイルと記録を削除する（path は list_user_loras の path。手でフォルダに置いたファイルなど、記録のないものは触らず not_found）/ Delete an added LoRA.",
		h.delete)
}

func familyKeys() string {
	var keys []string
	for _, o := range lorafamily.All() {
		keys = append(keys, o.Key)
	}
	return strings.Join(keys, " / ")
}

type handlers struct{ svc *service.Services }

func (h handlers) mm() (*modelmanager.Service, error) {
	if h.svc.ModelManager == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "モデルマネージャーはこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return h.svc.ModelManager, nil
}

// MapError turns the refusals of the importer into the errors the tools map: a file or value that cannot be accepted is a validation error, a state that
// does not allow it a conflict, a LoRA that is not recorded not found.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, loraimport.ErrNotFound) {
		return &mcpkit.ToolError{Code: mcpkit.CodeNotFound, Message: "その LoRA は追加した記録にありません（list_user_loras の path を指定してください）", Status: http.StatusNotFound}
	}
	switch kind, msg := mmview.Refuse(err); kind {
	case mmview.RefusalInvalid:
		return &service.ValidationError{Message: msg}
	case mmview.RefusalConflict:
		return &service.ConflictError{Message: msg}
	case mmview.RefusalNotFound:
		return &mcpkit.ToolError{Code: mcpkit.CodeNotFound, Message: msg, Status: http.StatusNotFound}
	}
	return err
}

// LoRA is an added LoRA as the tools say it.
type LoRA struct {
	loraimport.Model
	// Families are the words of the base models (the keys are in base_models).
	Families []string `json:"base_model_labels,omitempty"`
	// License is the answer to commercial use as the user confirmed it (unknown when they gave none: never yes).
	License modellicense.Info `json:"license"`
}

func viewOf(m loraimport.Model) LoRA {
	return LoRA{Model: m, Families: lorafamily.Labels(m.BaseModels), License: modellicense.FromUser(m.LicenseName, modellicense.Commercial(m.Commercial))}
}

// Fields are the values a call gives for what is known of a LoRA.
type Fields struct {
	BaseModels   []string `json:"base_models,omitempty" jsonschema:"対応するベースモデル: sdxl / sd1 / sd3 / flux / chroma / qwen-image / wan。省略は未設定"`
	Strength     *float64 `json:"strength,omitempty" jsonschema:"推奨の強さ（0 より大きく 1 以下）。省略は未設定"`
	TriggerWords []string `json:"trigger_words,omitempty" jsonschema:"トリガー語。省略は未設定"`
	LicenseName  string   `json:"license_name,omitempty" jsonschema:"ライセンスの名前（任意）。省略は不明"`
	Commercial   string   `json:"commercial,omitempty" jsonschema:"conditional（配布元で商用可と確認した自己申告）/ no（商用NG）。省略は不明。yes にはできない"`
}

func (f Fields) meta() loraimport.Meta {
	return loraimport.Meta{BaseModels: f.BaseModels, Strength: f.Strength, TriggerWords: strings.Join(f.TriggerWords, ","), LicenseName: f.LicenseName, Commercial: f.Commercial}
}

func (h handlers) upload(ctx context.Context, in mcpkit.UploadInput) (any, error) {
	m, err := h.mm()
	if err != nil {
		return nil, err
	}
	f := Fields{
		BaseModels: splitList(in.Params["base_models"]), TriggerWords: splitList(in.Params["trigger_words"]),
		LicenseName: in.Params["license_name"], Commercial: in.Params["commercial"],
	}
	if raw := strings.TrimSpace(in.Params["strength"]); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, &service.ValidationError{Field: "strength", Message: "strength は数字で指定してください（0 より大きく 1 以下）"}
		}
		f.Strength = &v
	}
	name := in.Header.Filename
	if n := strings.TrimSpace(in.Params["name"]); n != "" {
		name = n
	}
	file, err := in.Header.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	model, err := m.LoRAs.Import(ctx, name, in.Header.Size, file, loraimport.Input{Meta: f.meta()}, strings.EqualFold(in.Params["adopt_metadata"], "true"))
	if err != nil {
		return nil, MapError(err)
	}
	return viewOf(model), nil
}

func splitList(s string) []string {
	var out []string
	for _, w := range strings.Split(s, ",") {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// ListOutput is the added LoRAs.
type ListOutput struct {
	LoRAs []LoRA `json:"loras"`
}

func (h handlers) list(_ context.Context, _ struct{}) (ListOutput, error) {
	m, err := h.mm()
	if err != nil {
		return ListOutput{}, err
	}
	out := ListOutput{LoRAs: []LoRA{}}
	for _, l := range m.LoRAs.List() {
		out.LoRAs = append(out.LoRAs, viewOf(l))
	}
	return out, nil
}

// UpdateInput names a LoRA and gives the values that replace what is recorded.
type UpdateInput struct {
	Path string `json:"path" jsonschema:"list_user_loras の path（models/loras/<name>）"`
	Fields
}

func (h handlers) update(ctx context.Context, in UpdateInput) (LoRA, error) {
	m, err := h.mm()
	if err != nil {
		return LoRA{}, err
	}
	model, err := m.LoRAs.Update(ctx, in.Path, in.meta())
	if err != nil {
		return LoRA{}, MapError(err)
	}
	return viewOf(model), nil
}

// DeleteInput names a LoRA by its path.
type DeleteInput struct {
	Path string `json:"path" jsonschema:"list_user_loras の path（models/loras/<name>）"`
}

// Deleted says the LoRA was deleted.
type Deleted struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
}

func (h handlers) delete(ctx context.Context, in DeleteInput) (Deleted, error) {
	m, err := h.mm()
	if err != nil {
		return Deleted{}, err
	}
	if err := m.LoRAs.Delete(ctx, in.Path); err != nil {
		return Deleted{}, MapError(err)
	}
	return Deleted{Path: in.Path, Deleted: true}, nil
}
