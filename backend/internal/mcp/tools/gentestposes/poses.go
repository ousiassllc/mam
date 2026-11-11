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
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"
)

// Register adds the tools; they answer "unavailable" when the app has no pose library.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc}
	mcpkit.Read(r, "get_pose_library",
		"単発生成のポーズ集（Shisei。マネキンの線画と、ControlNet 用の深度・法線マップ）の状態と、導入済みならポーズの検索結果。installed=false のときは、取得元・ダウンロードの大きさ・ライセンス（商用利用の可否つき）を返す。導入済みなら query（名前・分類・番号 P001・英語。すべての語を含むもの）・series（std / upper）・class（分類名）で絞った poses[] を返す。画像 ID（images の値。std_P001_front の形）を run_generation_test の references に {source: pose_library, id, role: pose} として渡す。ControlNet（深度・法線のモデル）には depth_images・normal_images の値（std_P001_front.depth の形）を渡す。ある角度だけ載る / The state of the pose library and, when installed, its poses.",
		h.get)
	mcpkit.Write(r, "install_pose_library",
		"ポーズ集（Shisei）をダウンロードして導入する。accepted は、get_pose_library が返したライセンス（画像は CC0 1.0）と取得元を利用者が確認して同意したときだけ true にする（false だと何も取得せず拒否する。画面の同意チェックと同じ）。固定したリリースをファイルごとに SHA-256 で検証する。終わるまで応答しない（約 43 MB。以前の導入にファイルが足りないときは、足りない分だけ。get_pose_library の download_remaining）。導入済みなら何もしない。adult=true は成人向けシリーズ（get_pose_library の adult が返るときだけ。年齢確認がオンのときに限る）の導入 / Download and install the pose library; accepted must be true once the user agreed to the licenses and the source.",
		h.install, mcpkit.OpenWorld(), mcpkit.Idempotent())
	mcpkit.Destroy(r, "remove_pose_library",
		"導入したポーズ集（画像と目録）を削除する。成人向けシリーズも一緒に消える（adult=true なら成人向けシリーズだけ）。導入していなければ何もしない。導入中は conflict。履歴の結果は残る（結果は入力画像を自分で持っている）。もう一度使うときは install_pose_library / Delete the installed pose library.",
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
	Images map[string]string `json:"images" jsonschema:"アングル（front / diag / side / above / below）→ 画像 ID（線画）。references の id に渡す"`
	// DepthImages and NormalImages are the ControlNet control images of the same pose; only the angles that have one are listed.
	DepthImages  map[string]string `json:"depth_images,omitempty" jsonschema:"アングル → 深度マップの画像 ID（ControlNet 深度用。ある角度だけ。std_P001_front.depth の形）。references の id に渡す"`
	NormalImages map[string]string `json:"normal_images,omitempty" jsonschema:"アングル → 法線マップの画像 ID（ControlNet 法線用。ある角度だけ。std_P001_front.normal の形）。references の id に渡す"`
}

// Library is the state of the library.
type Library struct {
	Installed bool   `json:"installed"`
	Source    string `json:"source" jsonschema:"取得元のリポジトリ"`
	// PoseCount and ImageCount are what the library holds once installed; Download is how much the install downloads.
	PoseCount  int       `json:"pose_count"`
	ImageCount int       `json:"image_count"`
	Download   string    `json:"download" jsonschema:"導入でダウンロードする大きさ（例: 19.7 MiB）"`
	Remaining  string    `json:"download_remaining,omitempty" jsonschema:"今回ダウンロードする残りの大きさ。導入済みのファイルは取り直さないので、download より小さいときだけ（例: 以前の導入に深度・法線マップが無い）"`
	Licenses   []License `json:"licenses"`
	Poses      []Pose    `json:"poses,omitempty" jsonschema:"導入済みのときだけ。絞り込みに合うポーズ"`
	// Adult is the suggestive series; nil (left out) while the age confirmation is off: nothing of them is said then.
	Adult *AdultLibrary `json:"adult,omitempty" jsonschema:"成人向けシリーズ（sexy・duo）の状態。年齢確認（set_adult_content）がオンのときだけ現れる"`
}

// AdultLibrary is the state of the suggestive series (sexy, duo).
type AdultLibrary struct {
	Installed  bool      `json:"installed"`
	PoseCount  int       `json:"pose_count"`
	ImageCount int       `json:"image_count"`
	Download   string    `json:"download"`
	Remaining  string    `json:"download_remaining,omitempty"`
	Licenses   []License `json:"licenses"`
}

// Input is the filter of get_pose_library.
type Input struct {
	Query  string `json:"query,omitempty" jsonschema:"名前・分類・番号（P001）・英語の語。すべての語を含むもの"`
	Series string `json:"series,omitempty" jsonschema:"std / upper（成人向けの sexy / duo は、導入済みで年齢確認がオンのときだけ）"`
	Class  string `json:"class,omitempty" jsonschema:"分類名（get_pose_library の poses[].class）"`
}

func licensesOf(ls []catalog.ResourceLicense) []License {
	out := []License{}
	for _, lic := range ls {
		out = append(out, License{Name: lic.Name, URL: lic.URL, Summary: lic.Summary, Commercial: string(lic.Commercial), Notice: lic.Notice})
	}
	return out
}

// sizeText is a size of the download ("19.7 MiB").
func sizeText(n int64) string { return fetch.HumanBytes(uint64(max(n, 0))) } //nolint:gosec // clamped

// remaining is how much of the download is still to fetch: "" unless some of the files are in place already.
func remaining(in poses.Info) string {
	if in.RemainBytes >= in.DownloadBytes {
		return ""
	}
	return sizeText(in.RemainBytes)
}

func libraryOf(l *poses.Library, in Input) (Library, error) {
	st := l.Status()
	out := Library{
		Installed: st.Installed, Source: st.Source, PoseCount: st.Poses, ImageCount: st.Images, Download: sizeText(st.DownloadBytes), Remaining: remaining(st.Info),
		Licenses: licensesOf(st.Licenses),
	}
	if a := st.Adult; a != nil {
		out.Adult = &AdultLibrary{Installed: a.Installed, PoseCount: a.Poses, ImageCount: a.Images, Download: sizeText(a.DownloadBytes), Remaining: remaining(a.Info), Licenses: licensesOf(a.Licenses)}
	}
	if !st.Installed {
		return out, nil
	}
	all, err := l.Poses()
	if err != nil {
		return Library{}, err
	}
	for _, p := range poses.Filter(all, poses.Query{Text: strings.TrimSpace(in.Query), Series: in.Series, Class: in.Class}) {
		out.Poses = append(out.Poses, Pose{
			ID: p.ID, Series: p.Series, Class: p.Class, Name: p.Name, NameEN: p.NameEN,
			Images: byAngle(p.Views), DepthImages: byAngle(p.Maps[poses.Depth]), NormalImages: byAngle(p.Maps[poses.Normal]),
		})
	}
	return out, nil
}

// byAngle is the pictures with the angles as strings (nil for none, so that the field is left out).
func byAngle(m map[poses.Angle]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for a, id := range m {
		out[string(a)] = id
	}
	return out
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
	Adult    bool `json:"adult,omitempty" jsonschema:"true で成人向けシリーズ（sexy・duo）を導入する。年齢確認（set_adult_content）がオンで、ポーズ集（全年齢向け）が導入済みのときだけ。オフの間は何も取得せず拒否する"`
}

func (h handlers) install(ctx context.Context, in InstallInput) (Library, error) {
	l, err := h.lib()
	if err != nil {
		return Library{}, err
	}
	if in.Adult {
		err = l.InstallAdult(ctx, in.Accepted, nil)
	} else {
		err = l.Install(ctx, in.Accepted, nil)
	}
	if err != nil {
		return Library{}, mapError(err)
	}
	out, err := libraryOf(l, Input{})
	return out, mapError(err)
}

// RemoveInput says what to delete.
type RemoveInput struct {
	Adult bool `json:"adult,omitempty" jsonschema:"true で成人向けシリーズ（sexy・duo）だけを削除する（全年齢向けのポーズ集は残る）。省略すると全部を削除する"`
}

func (h handlers) remove(_ context.Context, in RemoveInput) (Library, error) {
	l, err := h.lib()
	if err != nil {
		return Library{}, err
	}
	if in.Adult {
		err = l.RemoveAdult()
	} else {
		err = l.Remove()
	}
	if err != nil {
		return Library{}, mapError(err)
	}
	out, err := libraryOf(l, Input{})
	return out, mapError(err)
}
