package facemodels

import (
	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// ToolName is the MCP tool that installs the models (the words of the status point at it).
const ToolName = "install_face_models"

// View is the state of the face check models as the MCP tools say it (list_generation_models face_similarity.models, install_face_models, remove_face_models):
// whether the files are installed, where, how large the download is and the license of each, and whether the engine that reads them works (the
// ONNX Runtime library is a separate matter: installing the models does not fix a missing one).
type View struct {
	State         State         `json:"state" jsonschema:"installed（2 つのファイルが揃い、SHA-256 を確認済み）/ unverified（揃っているが SHA-256 は未確認。install_face_models で確認する）/ missing（足りない）"`
	Installed     bool          `json:"installed" jsonschema:"state が installed"`
	ModelDir      string        `json:"model_dir" jsonschema:"導入先（顔モデルを読むフォルダ。起動オプション --face-model-dir で変えられる）"`
	Files         []FileView    `json:"files"`
	DownloadBytes int64         `json:"download_bytes" jsonschema:"導入でダウンロードする大きさ（全部）"`
	RemainBytes   int64         `json:"remain_bytes" jsonschema:"まだ取得が要る大きさ（揃っているファイルの分は引く）"`
	Licenses      []LicenseView `json:"licenses" jsonschema:"ライセンス。導入の前に利用者に見せ、同意を得てから install_face_models(accepted=true) を呼ぶ"`
	EngineReady   bool          `json:"engine_ready" jsonschema:"顔の類似度を今測れる（モデルが読め、ONNX Runtime も使える）。再起動は要らない"`
	EngineOff     string        `json:"engine_off,omitempty" jsonschema:"測れないときの理由: models_missing（モデルが無い。install_face_models で入れる）/ runtime_unavailable（モデルは有るが ONNX Runtime が使えない。モデルを入れ直しても直らない）"`
	InstallTool   string        `json:"install_tool,omitempty" jsonschema:"models_missing / state が missing のとき、導入のツール名"`
}

// FileView is one model file.
type FileView struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	SizeBytes   int64  `json:"size_bytes"`
	State       State  `json:"state" jsonschema:"installed / unverified / missing"`
	SourceName  string `json:"source_name"`
	SourceURL   string `json:"source_url"`
}

// LicenseView is a license of the models with the answer of whether it may be used commercially.
type LicenseView struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Summary    string `json:"summary"`
	Commercial string `json:"commercial" jsonschema:"yes 商用OK / conditional 条件つき / no 商用NG / unknown 不明"`
	Notice     string `json:"notice,omitempty"`
}

// View is the status in the words of the tools.
func (s Status) View() View {
	v := View{
		State: s.State, Installed: s.Installed(), ModelDir: s.ModelDir, Files: []FileView{}, DownloadBytes: s.DownloadBytes, RemainBytes: s.RemainBytes,
		Licenses: []LicenseView{}, EngineReady: s.Off == nil,
	}
	for _, f := range s.Files {
		v.Files = append(v.Files, FileView{Name: f.Name, Title: f.Title, Description: f.Description, SizeBytes: f.Size, State: f.State, SourceName: f.SourceName, SourceURL: f.SourceURL})
	}
	for _, l := range s.Licenses {
		v.Licenses = append(v.Licenses, LicenseView{Name: l.Name, URL: l.URL, Summary: l.Summary, Commercial: string(l.Commercial), Notice: l.Notice})
	}
	if s.Off != nil {
		v.EngineOff = s.Off.Reason
	}
	if s.State == StateMissing || (s.Off != nil && s.Off.Reason == facesim.ReasonModelsMissing) {
		v.InstallTool = ToolName
	}
	return v
}
