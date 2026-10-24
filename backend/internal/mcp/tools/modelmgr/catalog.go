package modelmgr

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

func registerCatalog(r *mcpkit.Registrar, h handlers) {
	mcpkit.Read(r, "list_model_catalog",
		"導入できるローカルの画像・動画モデルの一覧（種別・大きさ・VRAM の目安・ライセンスと商用利用の可否・同意が要るか・導入済みか・導入中のジョブ）/ The catalog of local image and video models with the state of each.",
		h.listCatalog)
	mcpkit.Read(r, "get_catalog_model",
		"カタログのモデルを 1 件取得する（ファイルの一覧・共有ファイル・ライセンスの要約と URL・地域の制限を含む）。同意が要るモデルは、導入の前にこの内容をユーザーに見せる / One catalog model with its files and license (show it to the user before accepting).",
		h.getCatalogModel)
	mcpkit.Write(r, "accept_model_license",
		"モデルのライセンスに同意したことを記録する。license_id と license_version は get_catalog_model が返した値で、違えば拒否される（版が変わると改めて同意が要る）。ユーザーがライセンスを確認して同意した後にだけ呼ぶ / Record that the user accepted the license of a model (ID and version as in the catalog).",
		h.acceptLicense, mcpkit.Idempotent())
	mcpkit.Write(r, "install_model",
		"カタログのモデルをダウンロードして導入する。job_id を返すので get_job で進捗を見る（SHA-256 を検証し、中断しても再開できる）。同意が要るモデルは、先に accept_model_license しないと conflict で拒否される（導入の前に API が確かめる）。エンジンが未導入・空き容量不足・設定ファイルが固定されているときも始めずに拒否される / Install a catalog model as a job. A model that needs a license acceptance is refused until accepted.",
		h.installModel, mcpkit.OpenWorld())
	mcpkit.Destroy(r, "delete_model",
		"導入したモデルを削除する。画像の用途・Quality Policy に使われていれば拒否される。ほかの導入済みモデルと共有しているファイル（VAE など）は、使うモデルが残る間は消えない / Delete an installed model; shared files stay while another installed model uses them.",
		h.deleteModel)
}

// ListCatalogOutput is the catalog.
type ListCatalogOutput struct {
	Models  []modelmanager.ModelView `json:"models"`
	Blocked string                   `json:"blocked,omitempty" jsonschema:"導入できない理由（エンジン未導入・Windows 以外・設定ファイルの固定）。空なら導入できる"`
}

func (h handlers) listCatalog(ctx context.Context, _ Input) (ListCatalogOutput, error) {
	m, err := h.mm()
	if err != nil {
		return ListCatalogOutput{}, err
	}
	out := ListCatalogOutput{Models: m.Catalog()}
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
		return "同梱の ComfyUI は Windows 版だけで使えます。外部の ComfyUI の URL を設定してください"
	case st.Pinned:
		return "起動オプション（--model-config）で設定ファイルが固定されているため、導入と削除はできません"
	case !st.Engine.Writable:
		return "mam.exe のあるフォルダに書き込めません"
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
