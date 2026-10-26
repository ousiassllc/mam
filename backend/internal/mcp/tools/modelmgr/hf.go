package modelmgr

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

func registerHF(r *mcpkit.Registrar, h handlers) {
	mcpkit.Read(r, "search_hf_models",
		"Hugging Face のモデルをキーワードで検索する（ダウンロード数の多い順）。結果にダウンロード数・いいね・gated・NFAA を付ける。kind は text-to-image / text-to-video / image-to-video（省略で全部）。NFAA（not-for-all-audiences）のリポジトリは、年齢確認（set_adult_content）が済むまで結果に出ない / Search Hugging Face models; NFAA repositories are left out until the age confirmation.",
		h.searchHF, mcpkit.OpenWorld())
	mcpkit.Read(r, "get_hf_model_files",
		"Hugging Face のリポジトリ（owner/repo、または huggingface.co の URL: /tree/…・/blob/…・/resolve/…）をコミットに固定して、ファイルの一覧を返す。各ファイルのサイズ・SHA-256・形式・導入できるか（できない理由）・セキュリティのスキャン・推奨するフォルダ（パスまたはヘッダーから）・VRAM の目安、リポジトリのライセンス・gated・NFAA を含む。導入（install_hf_file）の前に、ライセンスと terms をユーザーに見せる。.safetensors だけ導入できる（.gguf は選べず、pickle 形式は拒否）。NFAA のリポジトリは年齢確認の前は conflict / List the files of a Hugging Face repository pinned to a commit, with what can be installed and why not.",
		h.getHFFiles, mcpkit.OpenWorld())
	mcpkit.Write(r, "install_hf_file",
		"Hugging Face のリポジトリの .safetensors を 1 つ、同梱の ComfyUI に導入する。get_hf_model_files の repo・commit・path を渡す（コミット固定の URL で取得し、LFS の SHA-256 とサイズで検証。合わなければファイルは残らない）。folder・name でフォルダとファイル名を上書きできる（省略でパス・ヘッダーから決める。決められなければ validation）。同名で中身の違うファイルは上書きされず validation（別の name を提案する）。ライセンスと terms をユーザーに見せ、ユーザーが同意した後にだけ accept_terms=true と terms_version（get_hf_model_files の値）を付ける。同意はリポジトリ+コミットごとに記録され、記録が無ければ conflict。gated はトークンの設定とアクセスの承認が要る。NFAA は年齢確認の前は conflict。SDXL 系の一体型チェックポイントは、画像モデルとして試し生成で選べるようになる / Install one .safetensors file of a Hugging Face repository as a job.",
		h.installHF, mcpkit.OpenWorld())
	mcpkit.Read(r, "list_user_models",
		"ユーザーが Hugging Face から追加したファイルの一覧（リポジトリ・コミット・パス・SHA-256・サイズ・フォルダ・ライセンスのスナップショット・取得日時・作ったモデルの識別子・カタログのモデルと共有しているか）。NFAA のリポジトリのファイルは、年齢確認が済むまで出ない / The files the user added from Hugging Face.",
		h.listUserModels)
	mcpkit.Destroy(r, "delete_user_model",
		"ユーザーが Hugging Face から追加したファイルを削除する（path は list_user_models の path）。作ったモデルも消える。画像の用途・Quality Policy に使われていれば validation。カタログのモデルと共有しているファイルは、そのモデルが残る間は消えない / Delete a file the user added from Hugging Face.",
		h.deleteUserModel)
}

// SearchHFInput is a search.
type SearchHFInput struct {
	Q     string `json:"q" jsonschema:"キーワード（1〜200 文字）"`
	Kind  string `json:"kind,omitempty" jsonschema:"text-to-image / text-to-video / image-to-video。省略で全部"`
	Limit int    `json:"limit,omitempty" jsonschema:"件数（既定 20、最大 50）"`
}

// SearchHFOutput is the hits.
type SearchHFOutput struct {
	Hits []hf.SearchHit `json:"hits"`
}

func (h handlers) searchHF(ctx context.Context, in SearchHFInput) (SearchHFOutput, error) {
	m, err := h.mm()
	if err != nil {
		return SearchHFOutput{}, err
	}
	hits, err := m.SearchHF(ctx, in.Q, in.Kind, in.Limit)
	if hits == nil {
		hits = []hf.SearchHit{}
	}
	return SearchHFOutput{Hits: hits}, MapError(err)
}

// HFFilesInput names a repository.
type HFFilesInput struct {
	Repo string `json:"repo" jsonschema:"owner/repo、または https://huggingface.co/… の URL（/tree/<rev>/…・/blob/<rev>/…・/resolve/<rev>/…）"`
}

func (h handlers) getHFFiles(ctx context.Context, in HFFilesInput) (modelmanager.HFRepoView, error) {
	m, err := h.mm()
	if err != nil {
		return modelmanager.HFRepoView{}, err
	}
	v, err := m.HFRepo(ctx, in.Repo)
	return v, MapError(err)
}

// InstallHFInput is one file to install.
type InstallHFInput struct {
	Repo         string `json:"repo" jsonschema:"get_hf_model_files の repo.id（owner/repo）"`
	Commit       string `json:"commit" jsonschema:"get_hf_model_files の repo.commit（40 桁のハッシュ）"`
	Path         string `json:"path" jsonschema:"リポジトリ内のファイルのパス（files[].path）"`
	Folder       string `json:"folder,omitempty" jsonschema:"導入先のフォルダ: checkpoints / diffusion_models / text_encoders / vae / loras。省略でパス・ヘッダーから決める"`
	Name         string `json:"name,omitempty" jsonschema:"導入するファイル名（英数字と . _ + - の .safetensors）。省略でリポジトリのファイル名"`
	AcceptTerms  bool   `json:"accept_terms,omitempty" jsonschema:"ユーザーがライセンスと terms を見て同意したとき true。リポジトリ+コミットごとに一度。エージェントが代わりに true にしてはならない"`
	TermsVersion string `json:"terms_version,omitempty" jsonschema:"ユーザーに見せた terms の版（get_hf_model_files の terms_version）。accept_terms と一緒に渡す"`
}

func (h handlers) installHF(ctx context.Context, in InstallHFInput) (JobOutput, error) {
	m, err := h.mm()
	if err != nil {
		return JobOutput{}, err
	}
	target := "hf:" + in.Repo + "@" + in.Commit + "/" + in.Path
	return startInstall(ctx, h.registrar, target, func(ctx context.Context) (modelmanager.JobRef, error) {
		return m.InstallHFFile(ctx, modelmanager.HFInstallInput{
			Repo: in.Repo, Commit: in.Commit, Path: in.Path, Folder: in.Folder, Name: in.Name, Accept: in.AcceptTerms, TermsVersion: in.TermsVersion,
		})
	})
}

// UserModelsOutput is the files the user added.
type UserModelsOutput struct {
	Models []modelmanager.UserModelView `json:"models"`
}

func (h handlers) listUserModels(_ context.Context, _ Input) (UserModelsOutput, error) {
	m, err := h.mm()
	if err != nil {
		return UserModelsOutput{}, err
	}
	return UserModelsOutput{Models: m.UserModels()}, nil
}

// UserModelInput names a user model by its path.
type UserModelInput struct {
	Path string `json:"path" jsonschema:"list_user_models の path（models/<folder>/<name>）"`
}

// UserModelDeleted says the file was deleted.
type UserModelDeleted struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
}

func (h handlers) deleteUserModel(ctx context.Context, in UserModelInput) (UserModelDeleted, error) {
	m, err := h.mm()
	if err != nil {
		return UserModelDeleted{}, err
	}
	if err := m.DeleteUserModel(ctx, in.Path); err != nil {
		return UserModelDeleted{}, MapError(err)
	}
	return UserModelDeleted{Path: in.Path, Deleted: true}, nil
}
