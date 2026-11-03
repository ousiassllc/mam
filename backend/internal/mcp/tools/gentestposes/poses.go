// Package gentestposes holds the MCP tools of the pose library of the generation test (Issue #994, spec/api/mcp-tools-generation-test.md): see
// whether the library (Shisei) is installed and search its poses, install it (with the agreement the screen asks for) and delete it. The
// pictures are given to a model as {source: pose_library, id, role: pose} references of run_generation_test; they are not downloaded
// through a tool.
package gentestposes

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"
)

// Register adds the tools; they answer "unavailable" when the app has no pose library.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc}
	mcpkit.Read(r, "get_pose_library",
		"生成テストのポーズ集（Shisei。マネキンの線画）の状態と、導入済みならポーズの検索結果。installed=false のときは、取得元・ダウンロードの大きさ・ライセンス（商用利用の可否つき）を返す。導入済みなら query（名前・分類・番号 P001・英語。すべての語を含むもの）・series（std / upper）・class（分類名）で絞った poses[] を返す。画像 ID（images の値。std_P001_front の形）を run_generation_test の references に {source: pose_library, id, role: pose} として渡す / The state of the pose library and, when installed, its poses.",
		h.get)
	mcpkit.Write(r, "install_pose_library",
		"ポーズ集（Shisei）をダウンロードして導入する。accepted は、get_pose_library が返したライセンス（画像は CC0 1.0）と取得元を利用者が確認して同意したときだけ true にする（false だと何も取得せず拒否する。画面の同意チェックと同じ）。固定したリリースをファイルごとに SHA-256 で検証する。終わるまで応答しない（約 20 MB）。導入済みなら何もしない / Download and install the pose library; accepted must be true once the user agreed to the licenses and the source.",
		h.install, mcpkit.OpenWorld(), mcpkit.Idempotent())
	mcpkit.Destroy(r, "remove_pose_library",
		"導入したポーズ集（画像と目録）を削除する。導入していなければ何もしない。導入中は conflict。履歴の結果は残る（結果は入力画像を自分で持っている）。もう一度使うときは install_pose_library / Delete the installed pose library.",
		h.remove)
}

type handlers struct{ svc *service.Services }

func (h handlers) lib() (*poses.Library, error) {
	if h.svc.Gentest == nil || h.svc.Gentest.Poses == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "ポーズ集はこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return h.svc.Gentest.Poses, nil
}

// mapError turns the refusals of the library into the codes a tool answers with.
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
	case errors.Is(err, poses.ErrNotInstalled):
		return &mcpkit.ToolError{Code: mcpkit.CodeValidation, Message: "ポーズ集が導入されていません（install_pose_library）", Status: http.StatusUnprocessableEntity}
	}
	return mcpkit.MapError(err)
}

// License is a license of the library with the answer of whether it may be used commercially.
type License struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Summary    string `json:"summary"`
	Commercial string `json:"commercial" jsonschema:"yes 商用OK / conditional 条件つき / no 商用NG / unknown 不明"`
	Notice     string `json:"notice,omitempty"`
}

// Pose is a pose of the installed library.
type Pose struct {
	ID     string            `json:"id" jsonschema:"ポーズ番号（P001）"`
	Series string            `json:"series" jsonschema:"std / upper（上半身）"`
	Class  string            `json:"class"`
	Name   string            `json:"name"`
	NameEN string            `json:"name_en"`
	Images map[string]string `json:"images" jsonschema:"アングル（front / diag / side / above / below）→ 画像 ID。references の id に渡す"`
}

// Library is the state of the library.
type Library struct {
	Installed bool   `json:"installed"`
	Source    string `json:"source" jsonschema:"取得元のリポジトリ"`
	// PoseCount and ImageCount are what the library holds once installed; Download is how much the install downloads.
	PoseCount  int       `json:"pose_count"`
	ImageCount int       `json:"image_count"`
	Download   string    `json:"download" jsonschema:"導入でダウンロードする大きさ（例: 19.7 MiB）"`
	Licenses   []License `json:"licenses"`
	Poses      []Pose    `json:"poses,omitempty" jsonschema:"導入済みのときだけ。絞り込みに合うポーズ"`
}

// Input is the filter of get_pose_library.
type Input struct {
	Query  string `json:"query,omitempty" jsonschema:"名前・分類・番号（P001）・英語の語。すべての語を含むもの"`
	Series string `json:"series,omitempty" jsonschema:"std / upper"`
	Class  string `json:"class,omitempty" jsonschema:"分類名（get_pose_library の poses[].class）"`
}

func libraryOf(l *poses.Library, in Input) (Library, error) {
	st := l.Status()
	out := Library{
		Installed: st.Installed, Source: st.Source, PoseCount: st.Poses, ImageCount: st.Images, Download: fetch.HumanBytes(uint64(max(st.DownloadBytes, 0))), //nolint:gosec // clamped
		Licenses: []License{},
	}
	for _, lic := range st.Licenses {
		out.Licenses = append(out.Licenses, License{Name: lic.Name, URL: lic.URL, Summary: lic.Summary, Commercial: string(lic.Commercial), Notice: lic.Notice})
	}
	if !st.Installed {
		return out, nil
	}
	all, err := l.Poses()
	if err != nil {
		return Library{}, err
	}
	for _, p := range poses.Filter(all, poses.Query{Text: strings.TrimSpace(in.Query), Series: in.Series, Class: in.Class}) {
		views := map[string]string{}
		for a, id := range p.Views {
			views[string(a)] = id
		}
		out.Poses = append(out.Poses, Pose{ID: p.ID, Series: p.Series, Class: p.Class, Name: p.Name, NameEN: p.NameEN, Images: views})
	}
	return out, nil
}

func (h handlers) get(_ context.Context, in Input) (Library, error) {
	l, err := h.lib()
	if err != nil {
		return Library{}, err
	}
	out, err := libraryOf(l, in)
	return out, mapError(err)
}

// InstallInput is the agreement of the user.
type InstallInput struct {
	Accepted bool `json:"accepted" jsonschema:"利用者がライセンス（画像は CC0 1.0）と取得元を確認し、ダウンロードに同意したときだけ true"`
}

func (h handlers) install(ctx context.Context, in InstallInput) (Library, error) {
	l, err := h.lib()
	if err != nil {
		return Library{}, err
	}
	if err := l.Install(ctx, in.Accepted, nil); err != nil {
		return Library{}, mapError(err)
	}
	out, err := libraryOf(l, Input{})
	return out, mapError(err)
}

// Empty has no field.
type Empty struct{}

func (h handlers) remove(_ context.Context, _ Empty) (Library, error) {
	l, err := h.lib()
	if err != nil {
		return Library{}, err
	}
	if err := l.Remove(); err != nil {
		return Library{}, mapError(err)
	}
	out, err := libraryOf(l, Input{})
	return out, mapError(err)
}
