package modelmgr

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

func registerCatalog(r *mcpkit.Registrar, h handlers) {
	mcpkit.Read(r, "list_model_catalog",
		"導入できるローカルの画像・動画モデルの一覧（種別・成人向けか（content_rating）・LoRA のトリガー語・大きさ・VRAM の目安（vram.min_gb と推奨 recommended_gb。vram_fit がこの PC の GPU から見た判定で、limited は「動くが制約あり」）・ライセンス（license.commercial: yes 商用OK / conditional 条件つき / no 商用NG / unknown 不明、条件、出典 URL、確認日。部品ごとのライセンスが違うモデルは厳しい方）・同意が要るか・導入済みか・導入中のジョブ）。成人向けモデルは、年齢確認（set_adult_content）が済むまで一覧に出ない。commercial 引数（all / ok / ok_or_conditional）で商用利用により絞り込める（省くと設定「商用OKのモデルだけを使う」に従う） / The catalog of local image and video models with the state of each and its commercial-use license. Adult models are listed only after the age confirmation. The commercial argument filters by commercial use.",
		h.listCatalog)
	mcpkit.Read(r, "get_catalog_model",
		"カタログのモデルを 1 件取得する（ファイルの一覧・共有ファイル・ミラーの原本の場所（source_note）・ライセンスの要約と URL・地域の制限・成人向けの注意書きを含む）。同意が要るモデルは、導入の前にこの内容をユーザーに見せる。成人向けモデルは、年齢確認の前は conflict / One catalog model with its files and license (show it to the user before accepting). An adult model is refused before the age confirmation.",
		h.getCatalogModel)
	mcpkit.Write(r, "accept_model_license",
		"モデルのライセンスに同意したことを記録する。license_id と license_version は get_catalog_model が返した値で、違えば拒否される（版が変わると改めて同意が要る）。ユーザーがライセンスを確認して同意した後にだけ呼ぶ / Record that the user accepted the license of a model (ID and version as in the catalog).",
		h.acceptLicense, mcpkit.Idempotent())
	mcpkit.Write(r, "install_model",
		"カタログのモデルをダウンロードして導入する。job_id を返すので get_job で進捗を見る（SHA-256 を検証し、中断しても再開できる）。同意が要るモデルは、先に accept_model_license しないと conflict で拒否される（導入の前に API が確かめる）。成人向けモデルは、年齢確認（set_adult_content）が済むまで conflict で拒否される。エンジンが未導入・空き容量不足・設定ファイルが固定されているときも始めずに拒否される。requires_hf_token のモデル（LTX-2.5 など。Hugging Face の gated）は、トークン未設定（set_huggingface_token）・トークンが無効・モデルのページでアクセスが承認されていないとき、ジョブを作らずに validation で拒否され、メッセージが次にすることを案内する（アクセスの申請は mam からはできない） / Install a catalog model as a job. A model that needs a license acceptance is refused until accepted. A model behind a Hugging Face login (requires_hf_token) is refused before any job without a token, with a token that is not accepted, or without the approved access, with guidance.",
		h.installModel, mcpkit.OpenWorld())
	mcpkit.Destroy(r, "delete_model",
		"導入したモデルを削除する。画像の用途・Quality Policy に使われていれば拒否される。ほかの導入済みモデルと共有しているファイル（VAE など）は、使うモデルが残る間は消えない / Delete an installed model; shared files stay while another installed model uses them.",
		h.deleteModel)
}

// ListCatalogOutput is the catalog.
type ListCatalogOutput struct {
	Models []modelmanager.ModelView `json:"models"`
	// CommercialFilter is the filter that narrowed Models: what commercial asked for, or the setting's default.
	CommercialFilter string `json:"commercial_filter" jsonschema:"models に適用した商用利用の絞り込み（all / ok / ok_or_conditional）"`
	Blocked          string `json:"blocked,omitempty" jsonschema:"導入できない理由（エンジン未導入・Windows 以外・設定ファイルの固定）。空なら導入できる"`
	// VRAMFit is how this PC's GPU stands to each model's VRAM guideline (vram.min_gb / recommended_gb), by model id; absent while the GPU memory is not known.
	VRAMFit map[string]mmview.ModelFit `json:"vram_fit,omitempty" jsonschema:"この PC の GPU から見た各モデルの VRAM の判定（モデル ID ごと）。fit が limited は「推奨には届かないが動くが制約あり」で、text に制約の中身が入る。GPU のメモリが分からないときは省かれる"`
	// PlatformWarnings is what this PC's OS / GPU generation says about each model whose catalog columns (requires, unsupported) it is outside of, by
	// model id (Issue #979). It only warns: install_model does not refuse for it.
	PlatformWarnings map[string]string `json:"platform_warnings,omitempty" jsonschema:"この PC の OS・GPU の世代が、モデルの対象（requires）の外、または動かないと分かっている環境（unsupported）のとき、その警告（モデル ID ごと）。警告だけで install_model は拒否しない。対象外のモデルが無ければ省かれる"`
}

// ListCatalogInput narrows the catalog by commercial use.
type ListCatalogInput struct {
	Commercial string `json:"commercial,omitempty" jsonschema:"商用利用で絞り込む。all（すべて）/ ok（商用OKだけ）/ ok_or_conditional（商用OKと条件つき）。省くと設定「商用OKのモデルだけを使う」に従う（オフなら all）"`
}

func (h handlers) listCatalog(ctx context.Context, in ListCatalogInput) (ListCatalogOutput, error) {
	m, err := h.mm()
	if err != nil {
		return ListCatalogOutput{}, err
	}
	filter := m.DefaultFilter()
	if in.Commercial != "" {
		parsed, ok := modellicense.ParseFilter(in.Commercial)
		if !ok {
			return ListCatalogOutput{}, &service.ValidationError{Message: "commercial は all / ok / ok_or_conditional のどれかです"}
		}
		filter = parsed
	}
	out := ListCatalogOutput{Models: []modelmanager.ModelView{}, CommercialFilter: string(filter)}
	gpuVRAM := mmview.GPUVRAM(ctx, m)
	for _, v := range m.Catalog() {
		if !filter.Allows(v.License.Commercial) {
			continue
		}
		out.Models = append(out.Models, v)
		if w := mmview.PlatformWarning(ctx, m, v); w != "" {
			if out.PlatformWarnings == nil {
				out.PlatformWarnings = map[string]string{}
			}
			out.PlatformWarnings[v.ID] = w
		}
		if fit, ok := mmview.FitOf(v.VRAM, gpuVRAM); ok {
			if out.VRAMFit == nil {
				out.VRAMFit = map[string]mmview.ModelFit{}
			}
			out.VRAMFit[v.ID] = fit
		}
	}
	st, err := m.Status(ctx)
	if err != nil {
		return out, MapError(err)
	}
	out.Blocked = Blocked(st)
	return out, nil
}

// Blocked says why no model can be installed now ("" when one can).
func Blocked(st modelmanager.Status) string {
	switch {
	case !st.Engine.Supported:
		return "同梱の ComfyUI は Windows 版と Apple Silicon の Mac（macOS 14 以降）版だけで使えます。外部の ComfyUI の URL を設定してください"
	case st.Pinned:
		return "起動オプション（--model-config）で設定ファイルが固定されているため、導入と削除はできません"
	case !st.Engine.Writable:
		return "導入先のフォルダ（Windows は mam.exe のあるフォルダ、macOS は mam のデータフォルダ）に書き込めません"
	case !st.Engine.Installed || st.Engine.Broken:
		return "先に ComfyUI エンジンを導入してください（install_model_engine）"
	}
	return ""
}

// ModelInput names a catalog model.
type ModelInput struct {
	ModelID string `json:"model_id" jsonschema:"list_model_catalog の id"`
}

func (h handlers) getCatalogModel(_ context.Context, in ModelInput) (modelmanager.ModelView, error) {
	m, err := h.mm()
	if err != nil {
		return modelmanager.ModelView{}, err
	}
	v, err := m.Model(in.ModelID)
	return v, MapError(err)
}

// AcceptInput is the license the user accepted.
type AcceptInput struct {
	ModelID        string `json:"model_id"`
	LicenseID      string `json:"license_id" jsonschema:"get_catalog_model の license.id"`
	LicenseVersion string `json:"license_version" jsonschema:"get_catalog_model の license.version"`
}

// AcceptOutput is the model after the acceptance.
type AcceptOutput struct {
	ModelID  string `json:"model_id"`
	Accepted bool   `json:"accepted"`
}

func (h handlers) acceptLicense(_ context.Context, in AcceptInput) (AcceptOutput, error) {
	m, err := h.mm()
	if err != nil {
		return AcceptOutput{}, err
	}
	if err := m.AcceptLicense(in.ModelID, modelmanager.LicenseRef{ID: in.LicenseID, Version: in.LicenseVersion}); err != nil {
		return AcceptOutput{}, MapError(err)
	}
	v, err := m.Model(in.ModelID)
	return AcceptOutput{ModelID: in.ModelID, Accepted: v.Accepted}, MapError(err)
}

// InstallInput is the model to install, with the acceptance of its license when it needs one.
type InstallInput struct {
	ModelID        string `json:"model_id"`
	LicenseID      string `json:"license_id,omitempty" jsonschema:"同意が要るモデルで、まだ accept_model_license していないとき: ユーザーが同意したライセンスの ID（license_version と一緒に）"`
	LicenseVersion string `json:"license_version,omitempty"`
}

func (h handlers) installModel(ctx context.Context, in InstallInput) (JobOutput, error) {
	m, err := h.mm()
	if err != nil {
		return JobOutput{}, err
	}
	var accept *modelmanager.LicenseRef
	if in.LicenseID != "" || in.LicenseVersion != "" {
		accept = &modelmanager.LicenseRef{ID: in.LicenseID, Version: in.LicenseVersion}
	}
	return startInstall(ctx, h.registrar, "model:"+in.ModelID, func(ctx context.Context) (modelmanager.JobRef, error) {
		return m.InstallModel(ctx, in.ModelID, accept)
	})
}

// DeletedOutput says the model was deleted.
type DeletedOutput struct {
	ModelID string `json:"model_id"`
	Deleted bool   `json:"deleted"`
}

func (h handlers) deleteModel(ctx context.Context, in ModelInput) (DeletedOutput, error) {
	m, err := h.mm()
	if err != nil {
		return DeletedOutput{}, err
	}
	if err := m.DeleteModel(ctx, in.ModelID); err != nil {
		return DeletedOutput{}, MapError(err)
	}
	return DeletedOutput{ModelID: in.ModelID, Deleted: true}, nil
}
