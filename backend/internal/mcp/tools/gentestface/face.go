// Package gentestface holds the MCP tools of the faces of the generation test (Issue #995, spec/api/mcp-tools-generation-test.md): the state of
// FaceFusion (an external tool the user installed, as ffmpeg is; its paths are set on the settings screen only, as the paths of ffmpeg are: an agent does not
// choose the program that is started later) and "swap the face" of a result. The face similarity of a result is
// not a tool: it is the field face_similarity of get_generation_result; the models it needs are installed and removed with install_face_models and remove_face_models
// (Issue #1026; their state is list_generation_models face_similarity.models). It is a package of its own because tools/gentest is at its line limit.
package gentestface

import (
	"context"
	"errors"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/facefusionsettings"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/face"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// Register adds the tools: FaceFusion, an external tool the user installed, is looked up as ffmpeg is, and a swap is started like an
// upscale: as a result of its own in the history. They answer "unavailable" when the app has no generation test or no FaceFusion settings.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc, registrar: r}
	registerModels(r, h)
	mcpkit.Read(r, "get_facefusion_status",
		"顔の入れ替え（FaceFusion。mam には同梱しない別ソフト）の状態。見つかったか（Python とフォルダ）・保存したパス・FaceFusion のバージョンと自己申告のライセンス・探した場所、選べるモデル（入れ替え・顔検出・復元）とそれぞれのライセンス（class: open 商用の制限なし / conditional 条件つき / noncommercial 商用NG / research 研究用のみ / unknown 商用不明）と既定、すべての入れ替えに付く注意（notes。ArcFace が非商用・成人向けの画像は断られる・本体は OpenRAIL-AS・モデルは FaceFusion が自分で取得する）、ライセンスの一次情報の URL。ready が true なら swap_generation_face を呼べる / The state of FaceFusion, the external face swapper, its models and their licenses.",
		h.facefusionStatus)
	mcpkit.Write(r, "swap_generation_face",
		"画像の顔を、別の画像の顔に入れ替える（FaceFusion の `facefusion.py headless-run`。別プロセスで起動する）。target は顔を入れ替えられる側の画像（生成テストの成功した画像の結果 {source: history, id: result_id} など）、face は入れ替える顔の画像（アップロードした {source: upload, id: reference_id} など。いちばん大きい顔 1 つが使われる）。モデルを省くと既定（入れ替え ghost_2_256・顔検出 yunet・復元 gfpgan_1.4）。復元を使わないなら enhancer に none。結果は元とは別の結果（model が facefusion）として履歴に残り、get_generation_result の face_swap に元の result_id・モデル・ライセンスの注意、face_similarity に顔の類似度（SFace）が入る。ローカルの処理なので見積り・確認は要らず、すぐ始まる。非商用・研究用・商用不明のモデルを選ぶと warnings に注意が返る（実行はする）。FaceFusion は成人向け・ヌードの画像を断る（無効にできない）ので、mam の成人向けモデルの結果には使えない。FaceFusion が見つからないときは validation（get_facefusion_status の ready が false）。16GB VRAM の実機での動作は未確認。job_id・batch_id・result_id を返す。進みは get_job / Swap the face of one picture into another with FaceFusion as a result of its own.",
		h.swapFace)
}

// FaceFusionStatus is the state of FaceFusion as the settings screen shows it.
type FaceFusionStatus struct {
	Ready  bool           `json:"ready" jsonschema:"Python とフォルダの両方が見つかり、顔を入れ替えられる"`
	Python FaceFusionPart `json:"python"`
	Path   FaceFusionPart `json:"path"`
	// Version and License are what the found FaceFusion says about itself (facefusion/metadata.py).
	Version       string               `json:"version,omitempty"`
	License       string               `json:"license,omitempty"`
	SearchDirs    []string             `json:"search_dirs,omitempty" jsonschema:"フォルダを指定していないときに探す場所（この順）"`
	Defaults      facefusion.Selection `json:"defaults"`
	Swappers      []facefusion.Model   `json:"swappers"`
	Detectors     []facefusion.Model   `json:"detectors"`
	Enhancers     []facefusion.Model   `json:"enhancers" jsonschema:"顔の復元のモデル。none（復元しない）も選べる"`
	Notes         []string             `json:"notes" jsonschema:"すべての入れ替えに付く注意"`
	ArcFace       string               `json:"arcface_note"`
	LicensesURL   string               `json:"licenses_url"`
	DisclaimerURL string               `json:"disclaimer_url"`
}

// FaceFusionPart is the Python or the folder: what the user set, what was found and why nothing was.
type FaceFusionPart struct {
	Configured string `json:"configured,omitempty"`
	Found      string `json:"found,omitempty"`
	Problem    string `json:"problem,omitempty"`
}

type handlers struct {
	svc       *service.Services
	registrar *mcpkit.Registrar
}

// Input is a tool without arguments.
type Input struct{}

func (h handlers) gt() (*gentest.Service, error) {
	if h.svc.Gentest == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "生成テストはこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return h.svc.Gentest, nil
}

// mapError turns the refusals of the generation test into the codes a tool answers with.
func mapError(err error) error {
	var (
		invalid  *gterr.InvalidError
		conflict *gterr.ConflictError
	)
	switch {
	case err == nil:
		return nil
	case errors.As(err, &invalid):
		return &mcpkit.ToolError{Code: mcpkit.CodeValidation, Message: invalid.Message, Status: http.StatusUnprocessableEntity}
	case errors.As(err, &conflict):
		return &mcpkit.ToolError{Code: mcpkit.CodeConflict, Message: conflict.Message, Status: http.StatusConflict}
	case errors.Is(err, gterr.ErrNotFound):
		return &mcpkit.ToolError{Code: mcpkit.CodeNotFound, Message: "対象が見つかりません", Status: http.StatusNotFound}
	case errors.Is(err, gterr.ErrUnavailable):
		return &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "生成テストはこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return mcpkit.MapError(err)
}

func part(p facefusionsettings.Part) FaceFusionPart {
	out := FaceFusionPart{Configured: p.Configured, Found: p.Found}
	if p.Err != nil {
		out.Problem = p.Err.Error()
	}
	return out
}

func (h handlers) faceFusion() (*facefusionsettings.Service, error) {
	if s := h.registrar.Settings.FaceFusion; s != nil {
		return s, nil
	}
	return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "FaceFusion の設定はこのアプリでは使えません", Status: http.StatusServiceUnavailable}
}

func (h handlers) facefusionStatus(_ context.Context, _ Input) (FaceFusionStatus, error) {
	s, err := h.faceFusion()
	if err != nil {
		return FaceFusionStatus{}, err
	}
	return facefusionStatusOf(s)
}

func facefusionStatusOf(s *facefusionsettings.Service) (FaceFusionStatus, error) {
	v, err := s.View()
	if err != nil {
		return FaceFusionStatus{}, err
	}
	return FaceFusionStatus{
		Ready: v.Ready(), Python: part(v.Python), Path: part(v.Path), Version: v.Version, License: v.License, SearchDirs: v.SearchDirs,
		Defaults: facefusion.Selection{}.WithDefaults(), Swappers: facefusion.Swappers, Detectors: facefusion.Detectors,
		Enhancers: append(append([]facefusion.Model{}, facefusion.Enhancers...), facefusion.Model{
			ID: facefusion.EnhancerNone, Label: "復元しない", License: "-", Class: facefusion.ClassOpen,
		}),
		Notes: []string{facefusion.LicenseNote, facefusion.ArcFaceNote, facefusion.AdultNote, facefusion.FirstRunNote}, ArcFace: facefusion.ArcFaceNote,
		LicensesURL: facefusion.LicensesURL, DisclaimerURL: facefusion.DisclaimerURL,
	}, nil
}

// SwapFaceInput is one swap.
type SwapFaceInput struct {
	Target   gentest.RefInput `json:"target" jsonschema:"顔を入れ替えられる側の画像（{source, id}。source は history / upload / project_asset / reference_item / pose_library）"`
	Face     gentest.RefInput `json:"face" jsonschema:"入れ替える顔の画像（{source, id}。いちばん大きい顔が使われる）"`
	Swapper  string           `json:"swapper,omitempty" jsonschema:"入れ替えモデル（get_facefusion_status の swappers。省くと ghost_2_256）"`
	Detector string           `json:"detector,omitempty" jsonschema:"顔検出モデル（detectors。省くと yunet）"`
	Enhancer string           `json:"enhancer,omitempty" jsonschema:"顔の復元モデル（enhancers。省くと gfpgan_1.4。none で復元しない）"`
}

// SwapFaceOutput is a started swap.
type SwapFaceOutput struct {
	mcpkit.JobRef
	BatchID  string   `json:"batch_id"`
	ResultID string   `json:"result_id" jsonschema:"顔を入れ替えた結果の result_id"`
	Warnings []string `json:"warnings,omitempty" jsonschema:"選んだモデルのライセンス上の注意（商用NG・研究用・商用不明）。無ければ空"`
}

func (h handlers) swapFace(ctx context.Context, in SwapFaceInput) (SwapFaceOutput, error) {
	g, err := h.gt()
	if err != nil {
		return SwapFaceOutput{}, err
	}
	var ref face.SwapRef
	jr, err := h.registrar.Jobs.Run(ctx, mcpgentest.KindGentest, "", func(ctx context.Context) (string, error) {
		var err error
		ref, err = face.Swap(ctx, g, face.SwapRequest{
			Target: in.Target, Face: in.Face, Selection: facefusion.Selection{Swapper: in.Swapper, Detector: in.Detector, Enhancer: in.Enhancer},
		})
		return ref.JobID, mapError(err)
	})
	return SwapFaceOutput{JobRef: jr, BatchID: ref.BatchID, ResultID: ref.ResultID, Warnings: ref.Warnings}, err
}
