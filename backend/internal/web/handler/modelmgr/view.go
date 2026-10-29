package modelmgr

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/hfguide"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/jobview"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
)

// bytesText, progressOf and ended are shared with the child packages (jobview).
var (
	bytesText  = jobview.BytesText
	progressOf = jobview.Progress
	ended      = jobview.Ended
)

// engineData is the card of the engine. job is the install job the card reports on ("" for the running one);
// starting says a start was just asked for, which the process status may not show yet.
func engineData(ctx context.Context, svc *modelmanager.Service, job string, starting bool) view.Engine {
	ev := svc.Engine()
	e := view.Engine{
		Supported: ev.Supported, Writable: ev.Writable, WritableError: ev.WritableError, Dir: ev.Dir, Free: bytesText(int64(min(ev.FreeBytes, 1<<62))), //nolint:gosec // bounded above
		Installed: ev.Installed, Version: ev.Version, PinnedVersion: ev.PinnedVersion, Variant: ev.Variant, CPU: ev.CPU,
		UpdateAvailable: ev.UpdateAvailable, Broken: ev.Broken, State: string(ev.Process.State), URL: ev.Process.URL,
		ProcessError: ev.Process.Error, AutoStart: ev.AutoStart, SourceURL: ev.SourceURL, LicenseName: ev.License.Name, LicenseURL: ev.License.URL,
	}
	e.VariantLabel = ev.Variant
	for _, v := range ev.Variants {
		if string(v.Variant) == ev.Variant {
			e.VariantLabel = v.Label
		}
	}
	if starting && e.State == "stopped" {
		e.State = "starting"
	}
	if ev.InstallJob != "" {
		e.Install, e.Job = progressOf(ctx, svc, ev.InstallJob), ev.InstallJob
	} else if job != "" {
		if st, err := svc.JobStatus(ctx, job); err == nil && (st.State == domain.JobQueued || st.State == domain.JobRunning) {
			e.Install, e.Job = progressOf(ctx, svc, job), job // the handler is done, the queue has not recorded the end yet
		} else {
			e.Notice, e.Error = ended(ctx, svc, job, "ComfyUI エンジン")
		}
	}
	return e
}

// gpuData is the GPU detection with the builds to choose from.
func gpuData(ctx context.Context, svc *modelmanager.Service) view.GPU {
	rep := svc.DetectGPU(ctx)
	ev := svc.Engine()
	g := view.GPU{
		Variant: string(rep.Recommendation.Variant), Reason: rep.Recommendation.Reason, Confidence: rep.Recommendation.Confidence,
		Guidance: rep.Recommendation.Guidance, CPUAvailable: rep.Recommendation.CPUAvailable || rep.Recommendation.Variant == "",
		Free: bytesText(int64(min(ev.FreeBytes, 1<<62))), DetectError: rep.Error, //nolint:gosec // bounded above
	}
	for _, a := range rep.Adapters {
		if !a.Software {
			g.Adapters = append(g.Adapters, view.Adapter{Name: a.Name, VRAM: bytesText(int64(min(a.VRAMBytes, 1<<62)))}) //nolint:gosec // bounded above
		}
	}
	for _, v := range ev.Variants {
		g.Variants = append(g.Variants, view.Variant{
			Value: string(v.Variant), Label: v.Label, Size: bytesText(v.Size), Need: bytesText(v.NeedBytes), Recommended: string(v.Variant) == g.Variant,
		})
	}
	return g
}

var regionNames = map[string]string{"EU": "EU", "GB": "英国", "KR": "韓国"}

func licenseOf(l catalog.License) view.License {
	out := view.License{ID: l.ID, Version: l.Version, Name: l.Name, URL: l.URL, Summary: l.Summary, Notice: l.Notice}
	switch {
	case l.CommercialUse && l.CommercialCondition != "":
		out.Commercial = "可（" + l.CommercialCondition + "）"
	case l.CommercialUse:
		out.Commercial = "可"
	case l.CommercialCondition != "":
		out.Commercial = "不可（" + l.CommercialCondition + "）"
	default:
		out.Commercial = "不可（非商用）"
	}
	if len(l.ExcludedRegions) > 0 {
		names := make([]string, len(l.ExcludedRegions))
		for i, r := range l.ExcludedRegions {
			names[i] = regionNames[r]
			if names[i] == "" {
				names[i] = r
			}
		}
		out.Regions = "次の地域では、このライセンスは適用されず、このモデルを使えません: " + strings.Join(names, "・")
	}
	return out
}

// vramText is the guideline of the video memory with where it comes from.
func vramText(minGB int, source string, estimate bool) string {
	if estimate {
		return fmt.Sprintf("%d GB 以上（mam の推定。GPU では未確認）", minGB)
	}
	return fmt.Sprintf("%d GB 以上（出典: %s）", minGB, source)
}

// blockedText says why no model can be installed now, or "".
func blockedText(ctx context.Context, svc *modelmanager.Service) string {
	st, err := svc.Status(ctx)
	if err != nil {
		return "導入できる状態か確かめられませんでした。mam のログを確認してください"
	}
	switch {
	case !st.Engine.Supported:
		return "同梱の ComfyUI は Windows 版だけで使えるため、この環境ではモデルを導入できません。外部の ComfyUI の URL を設定してください"
	case st.Pinned:
		return "起動オプション（--model-config）でモデルの設定ファイルが固定されているため、モデルを導入・削除できません"
	case !st.Engine.Writable:
		return "mam.exe のあるフォルダに書き込めないため、モデルを導入できません"
	case st.Engine.InstallJob != "":
		return "ComfyUI エンジンを導入しています。終わるとモデルを導入できます"
	case !st.Engine.Installed || st.Engine.Broken:
		return "先に ComfyUI エンジンを導入してください"
	}
	return ""
}

func modelData(ctx context.Context, svc *modelmanager.Service, m modelmanager.ModelView, free uint64, tokenMissing bool) view.Model {
	out := view.Model{
		ID: m.ID, Name: m.Name, KindLabel: kindLabel(string(m.Kind)), Advanced: m.Tier == "advanced", Description: m.Description,
		Adult: m.ContentRating == catalog.RatingAdult, TriggerWords: m.TriggerWords, Sources: sourcesOf(m),
		Size: bytesText(m.TotalSize), Need: bytesText(m.NeedBytes), VRAM: vramText(m.VRAM.MinGB, m.VRAM.Source, m.VRAM.Estimate),
		License: licenseOf(m.License), Consent: m.ConsentRequired, Accepted: m.Accepted, NeedsToken: m.RequiresHFToken,
		State: string(m.State), ModelIDs: m.ModelIDs, WorkflowCheck: m.WorkflowCheck,
		LowDisk: m.NeedBytes > 0 && free > 0 && uint64(m.NeedBytes) > free,
	}
	if m.RequiresHFToken && tokenMissing {
		out.TokenGuide = hfguide.NoToken // the same words as the refusal of the install (service, MCP)
	}
	for _, f := range m.Files {
		out.Files = append(out.Files, view.File{Path: f.Folder + "/" + f.Name, Size: bytesText(f.Size), Present: f.Present, Shared: f.SharedWith, SourceNote: f.SourceNote})
	}
	if m.InstallJob != "" {
		out.Install = progressOf(ctx, svc, m.InstallJob)
	}
	return out
}

// sourcesOf is the places of the originals of the model's mirrored files, each once, in the order of the files.
func sourcesOf(m modelmanager.ModelView) []string {
	var notes []string
	for _, f := range m.Files {
		if f.SourceNote != "" && !slices.Contains(notes, f.SourceNote) {
			notes = append(notes, f.SourceNote)
		}
	}
	return notes
}

func kindLabel(kind string) string {
	if kind == "video" {
		return "動画"
	}
	return "画像"
}

// catalogData is the model list. jobs are the install jobs it reports on besides the running ones: those that ended
// are shown once as a notice or an error.
func catalogData(ctx context.Context, svc *modelmanager.Service, jobs []string) view.Catalog {
	free := svc.Engine().FreeBytes
	hasToken, _, tokenErr := svc.HuggingFaceTokenStatus()
	c := view.Catalog{Blocked: blockedText(ctx, svc)}
	names := map[string]string{}
	for _, m := range svc.Catalog() {
		md := modelData(ctx, svc, m, free, tokenErr == nil && !hasToken)
		names[m.InstallJob] = "「" + m.Name + "」"
		c.Models = append(c.Models, md)
		if m.InstallJob != "" {
			c.Jobs = append(c.Jobs, m.InstallJob)
		}
	}
	var notices, failures []string
	for _, id := range jobs {
		if _, running := names[id]; running {
			continue
		}
		st, err := svc.JobStatus(ctx, id)
		if err != nil {
			continue
		}
		if st.State == domain.JobQueued || st.State == domain.JobRunning {
			c.Jobs = append(c.Jobs, id) // the end is not recorded yet: ask again
			continue
		}
		name := targetName(svc, st)
		if n, f := ended(ctx, svc, id, name); n != "" {
			notices = append(notices, n)
		} else if f != "" {
			failures = append(failures, f)
		}
	}
	c.Notice, c.Error = strings.Join(notices, "。"), strings.Join(failures, "\n")
	return c
}

// targetName is "「<model name>」" for the job of a model install.
func targetName(svc *modelmanager.Service, st modelmanager.JobStatus) string {
	if id, ok := strings.CutPrefix(st.Target, "model:"); ok {
		if m, err := svc.Model(id); err == nil {
			return "「" + m.Name + "」"
		}
	}
	return "モデル"
}
