package gentest

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/projectlink"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

var negativeModeText = map[string]string{"field": "モデルの欄に送った", "folded": "プロンプトに書き足した"}

// detail builds the page of one result around the import form f.
func (h Handlers) detail(ctx context.Context, d gentest.ResultDetail, f view.ImportForm) view.Detail {
	v := view.Detail{
		Card: cardOf(d.ResultView, false), SentPrompt: d.SentPrompt, Negative: d.NegativePrompt, NegMode: negativeModeText[d.NegativeMode],
		Params: params(d),
	}
	if d.Media == gentest.MediaImage && d.HasFile {
		v.UseURL = paths.GenerationTest() + "?ref=" + refValue{Source: gentest.FromHistory, ID: d.ID}.String()
	}
	for _, in := range d.Inputs {
		v.Inputs = append(v.Inputs, view.InputCard{Role: roleText[in.Role], Origin: originText[in.Origin.Kind], URL: paths.GenerationTestPart("inputs", in.ID, "file")})
	}
	names := h.projectNames(ctx)
	for _, im := range d.Imports {
		v.Imports = append(v.Imports, view.ImportRow{
			ProjectName: orDefault(names[im.ProjectID], "（削除されたプロジェクト）"), ProjectURL: paths.Project(im.ProjectID), What: importText(im.Target), At: timeText(im.CreatedAt),
		})
	}
	f.ResultID, f.Image = d.ID, d.Media == gentest.MediaImage
	if list, err := h.Svc.ProjectList(ctx); err == nil {
		for _, p := range list {
			f.Projects = append(f.Projects, view.ProjectChoice{ID: p.ID, Name: p.Name})
		}
	}
	v.Import = f
	if !d.HasFile {
		v.Import.Projects = nil
	}
	return v
}

var roleText = map[string]string{gentest.RoleReference: "参照", gentest.RoleFirstFrame: "最初のフレーム", gentest.RoleLastFrame: "最後のフレーム"}

var originText = map[string]string{
	gentest.FromStaged: "アップロード", gentest.FromProjectAsset: "プロジェクトのアセット", gentest.FromReferenceItem: "プロジェクトの参照画像", gentest.FromHistory: "履歴",
}

func (h Handlers) projectNames(ctx context.Context) map[string]string {
	out := map[string]string{}
	if list, err := h.Svc.ProjectList(ctx); err == nil {
		for _, p := range list {
			out[p.ID] = p.Name
		}
	}
	return out
}

func importText(t projectlink.ImportTarget) string {
	what := "アセットとして取り込みました"
	if t.As == projectlink.AsReference {
		what = "参照画像（" + t.Name + "）として取り込みました"
		if t.Assign != nil {
			what += fmt.Sprintf("・%s %s の %s に割り当て", t.Assign.Target, t.Assign.ID, t.Assign.Slot)
		}
	}
	return what
}

func params(d gentest.ResultDetail) []view.KV {
	kv := []view.KV{{Key: "モデル", Value: d.ModelLabel}, {Key: "API のモデル名", Value: d.APIModel}, {Key: "縦横比", Value: d.AspectRatio}}
	if d.Resolution != "" {
		kv = append(kv, view.KV{Key: "解像度", Value: d.Resolution})
	}
	if d.DurationSeconds > 0 {
		kv = append(kv, view.KV{Key: "長さ", Value: seconds(d.DurationSeconds) + " 秒"})
	}
	if d.SeedSent && d.Seed != nil {
		kv = append(kv, view.KV{Key: "シード", Value: fmt.Sprint(*d.Seed)})
	} else {
		kv = append(kv, view.KV{Key: "シード", Value: "送っていません（このモデルは受け付けません）"})
	}
	kv = append(kv, view.KV{Key: "入力したプロンプト", Value: d.Prompt}, view.KV{Key: "見積り", Value: costText(d)})
	if d.ActualUSD != nil {
		kv = append(kv, view.KV{Key: "実費（提供元の報告）", Value: usd(*d.ActualUSD)})
	}
	return append(kv, view.KV{Key: "作成", Value: timeText(d.CreatedAt)})
}

func costText(d gentest.ResultDetail) string {
	switch d.CostBasis {
	case modelcatalog.BasisFree:
		return "無料（ローカル）"
	case modelcatalog.BasisPriced:
		return usdPtr(d.EstimatedUSD)
	}
	return "単価なし"
}

// Import answers POST /generation-test/results/:id/import (project_id, as, kind, name, tags, assign_target / assign_id /
// assign_slot): the file is copied into the Project and, when asked, the Library image is put on a slot. A slot that is locked
// is 409 and the image stays imported.
func (h Handlers) Import(c *gin.Context) {
	ctx := c.Request.Context()
	req := projectlink.ImportRequest{
		ResultID: c.Param("id"), ProjectID: c.PostForm("project_id"), As: c.PostForm("as"), Kind: c.PostForm("kind"),
		Name: strings.TrimSpace(c.PostForm("name")), Tags: c.PostForm("tags"),
	}
	if t := c.PostForm("assign_target"); t != "" {
		req.Assign = &projectlink.AssignTarget{Target: t, ID: strings.TrimSpace(c.PostForm("assign_id")), Slot: strings.TrimSpace(c.PostForm("assign_slot"))}
	}
	out, err := h.Svc.Import(ctx, req)
	d, derr := h.Svc.Result(ctx, c.Param("id"))
	if derr != nil {
		status, msg := refuse(c, derr)
		render(c, status, view.ImportBox(view.ImportForm{Message: view.Message{Error: msg}}))
		return
	}
	f := view.ImportForm{}
	status := http.StatusOK
	if err != nil {
		status, f.Error = refuse(c, err)
	} else {
		f.Done = importedText(out)
	}
	render(c, status, view.ImportBox(h.detail(ctx, d, f).Import))
}

func importedText(o projectlink.ImportOutcome) string {
	switch {
	case o.Assigned:
		return "参照画像として取り込み、スロットに割り当てました"
	case o.ReferenceItemID != "":
		return "参照画像として取り込みました"
	}
	return "アセットとして取り込みました"
}
