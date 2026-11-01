package runtimes

import (
	"context"
	"fmt"
	"strings"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/jobview"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	card "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/runtimecard"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/weightlist"
)

var purposeLabels = map[catalog.Purpose]string{catalog.PurposeTTS: "声（音声合成）", catalog.PurposeLipSync: "口パク"}

// Section is the section of the screen: a card for every runtime of the catalog. Nil (the section is left out) when there is no
// service or no runtime.
func Section(ctx context.Context, svc *localruntime.Service) templ.Component {
	cards := items(ctx, svc)
	if len(cards) == 0 {
		return nil
	}
	return card.RuntimesSection(card.Runtimes{Items: cards})
}

func items(ctx context.Context, svc *localruntime.Service) []card.Runtime {
	if svc == nil {
		return nil
	}
	var out []card.Runtime
	for _, v := range svc.List(ctx) {
		out = append(out, runtimeCard(ctx, svc, v, "", false))
	}
	return out
}

// runtimeData is the card of one runtime. job is the install job it reports on ("" for the running one); starting says a start was just
// asked for, which the process status may not show yet.
func runtimeData(ctx context.Context, svc *localruntime.Service, id, job string, starting bool) (card.Runtime, error) {
	v, err := svc.Status(ctx, id)
	if err != nil {
		return card.Runtime{}, err
	}
	return runtimeCard(ctx, svc, v, job, starting), nil
}

func runtimeCard(ctx context.Context, svc *localruntime.Service, v localruntime.View, job string, starting bool) card.Runtime {
	r := card.Runtime{
		ID: v.ID, Name: v.Name, Purpose: purposeLabels[v.Purpose], Description: v.Description, SourceURL: v.SourceURL,
		Supported: v.Support.Supported, Reason: v.Support.Reason, Fallback: v.Support.Fallback, GPU: v.Support.GPU,
		Writable: v.Writable, WritableError: v.WritableError, Dir: v.Dir, Free: jobview.BytesText(int64(min(v.FreeBytes, 1<<62))), //nolint:gosec // bounded above
		VRAM:      jobview.VRAMText(v.VRAM.MinGB, v.VRAM.Source, v.VRAM.Estimate),
		Installed: v.Installed, Broken: v.Broken, Version: v.Version, PinnedVersion: v.PinnedVersion, UpdateAvailable: v.UpdateAvailable,
		State: string(v.Process.State), URL: v.Process.URL, ProcessError: v.Process.Error, AutoStart: v.AutoStart,
		LicenseName: v.License.Name, LicenseURL: v.License.URL, Weights: weightsOf(v.Weights),
	}
	if v.License.CommercialCondition != "" {
		r.LicenseCondition = v.License.CommercialCondition
	}
	// The build the card talks about: the installed one, else the one this GPU gets, else the first (a machine that cannot run it).
	want := string(v.Support.Variant)
	if v.Installed {
		want = v.Variant
	}
	for i, b := range v.Variants {
		if string(b.Variant) == want || (want == "" && i == 0) {
			r.Need, r.VariantLabel = jobview.BytesText(b.NeedBytes), b.Label
		}
	}
	if starting && r.State == "stopped" {
		r.State = "starting"
	}
	switch {
	case v.InstallJob != "":
		r.Install, r.Job = runtimeProgress(ctx, svc, v.InstallJob), v.InstallJob
	case job != "":
		if st, err := svc.JobStatus(ctx, job); err == nil && (st.State == domain.JobQueued || st.State == domain.JobRunning) {
			r.Install, r.Job = runtimeProgress(ctx, svc, job), job // the handler is done, the queue has not recorded the end yet
		} else {
			r.Notice, r.Error = runtimeEnded(ctx, svc, job, v.Name)
		}
	}
	return r
}

func weightsOf(w localruntime.WeightsView) weightlist.Weights {
	out := weightlist.Weights{State: string(w.State), Message: w.Message, NotReady: w.NotReady, Need: jobview.BytesText(w.NeedBytes), Total: jobview.BytesText(w.TotalBytes)}
	for _, a := range w.Assets {
		out.Assets = append(out.Assets, weightlist.Asset{
			Name: a.Name, Description: a.Description, Size: jobview.BytesText(a.Size), State: string(a.State), Required: a.Required,
			SourceName: a.Source.Name, SourceURL: a.Source.URL, SourceNote: a.Source.Note,
			License: weightlist.License{Name: a.License.Name, URL: a.License.URL, Summary: a.License.Summary, Commercial: string(a.License.CommercialUse), Notice: a.License.Notice},
		})
		if a.License.CommercialUse != catalog.CommercialYes {
			out.Unclear = append(out.Unclear, a.Name)
		}
	}
	return out
}

// runtimeProgress is what a running install reports (a placeholder while its status cannot be read).
func runtimeProgress(ctx context.Context, svc *localruntime.Service, jobID string) *view.Progress {
	st, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		return &view.Progress{JobID: jobID, Message: "状態を取得しています"}
	}
	p := &view.Progress{JobID: jobID, Percent: st.Progress.Percent, Message: st.Progress.Message}
	if st.State == domain.JobQueued {
		p.Message = "順番を待っています"
	}
	if p.Message == "" {
		p.Message = "準備しています"
	}
	if t := st.Progress.TotalBytes; t > 0 {
		p.Bytes = jobview.BytesText(st.Progress.DoneBytes) + " / " + jobview.BytesText(t)
	}
	return p
}

// runtimeEnded is how an install job ended, as a notice or an error line; both are empty while it runs (or when it is unknown).
func runtimeEnded(ctx context.Context, svc *localruntime.Service, jobID, name string) (notice, failure string) {
	st, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		return "", ""
	}
	switch st.State {
	case domain.JobSucceeded:
		return name + " を導入しました", ""
	case domain.JobFailed:
		reason := st.Error
		if reason == "" {
			reason = "原因は mam のログを確認してください"
		}
		return "", fmt.Sprintf("%s の導入に失敗しました: %s", name, strings.TrimSpace(reason))
	}
	return "", ""
}
