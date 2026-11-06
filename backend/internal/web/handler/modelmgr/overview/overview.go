// Package overview builds the overview of the model manager (GET /model-manager): for each category of this build, one line that says how it stands
// (spec/components/model-manager-screen.md, "分類のサブページ（#1101）"). Every line is read from the services now; nothing is stored. A line says counts and states
// only: never the value of the Hugging Face token, and not how many models the age gate hides. It is a package of its own because the directory of the
// handlers is full (linterly).
package overview

import (
	"context"
	"strconv"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/facefusionsettings"
	"github.com/ousiassllc/mam/backend/internal/service/ffmpegsettings"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/facemodels"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/mmcat"
)

// Deps are the services the lines are read from. A service that is nil is a category that this build does not have (the caller leaves it out of Categories).
type Deps struct {
	Svc      *modelmanager.Service
	Runtimes *localruntime.Service
	Nodes    *modelnodes.Service
	Face     *facemodels.Service
	// FFmpeg, FaceFusion and Codex are the services of the companion software (#1106); the line and the list read them only.
	FFmpeg     *ffmpegsettings.Service
	FaceFusion *facefusionsettings.Service
	Codex      *codex.Service
}

// Build is the overview with the cards of categories, in their order.
func Build(ctx context.Context, d Deps, categories []mmcat.Category) view.Overview {
	o := view.Overview{UpdatedAt: time.Now().Format(time.TimeOnly)}
	for _, c := range categories {
		card := view.CategoryCard{Category: c}
		switch c.Slug {
		case mmcat.ComfyUI:
			card.Summary, card.State = engine(ctx, d.Svc)
		case mmcat.ImageVideo:
			card.Summary, card.State = models(d.Svc)
		case mmcat.CustomNodes:
			card.Summary, card.State = customNodes(ctx, d.Nodes)
		case mmcat.LocalRuntimes:
			card.Summary, card.State = runtimes(ctx, d.Runtimes)
		case mmcat.FaceCheck:
			card.Summary, card.State = face(d.Face)
		case mmcat.HFExternal:
			card.Summary, card.State = hf(d.Svc)
		case mmcat.Companion:
			card.Summary, card.State = companionLine(d)
		}
		o.Installing = o.Installing || card.State == view.StateInstalling
		o.Cards = append(o.Cards, card)
	}
	return o
}

// engine is the line of the bundled ComfyUI engine; its state is the data-engine of the card of the engine (view.EnginePhase).
func engine(ctx context.Context, svc *modelmanager.Service) (string, string) {
	ev := svc.Engine()
	phase := view.EnginePhase(view.Engine{Supported: ev.Supported, Writable: ev.Writable, Installed: ev.Installed, Broken: ev.Broken, Install: installing(ev.InstallJob)})
	switch phase {
	case "unsupported":
		return "この PC では使えません（外部の ComfyUI を使います）", phase
	case "readonly":
		return "導入先に書き込めません", phase
	case "installing":
		text := "導入中"
		if st, err := svc.JobStatus(ctx, ev.InstallJob); err == nil && st.Progress.Percent > 0 {
			text += "（" + strconv.Itoa(st.Progress.Percent) + "%）"
		}
		return text, phase
	case "broken":
		return "ファイルが見つかりません。導入し直してください", phase
	case "missing":
		return "未導入", phase
	}
	if ev.Process.State == proc.StateRunning {
		return "導入済み（ComfyUI " + ev.Version + "・" + ev.Variant + "・起動中）", phase
	}
	return "導入済み（停止中）", phase
}

// installing is the progress that says "an install is running" to EnginePhase (nil for no job).
func installing(job string) *view.Progress {
	if job == "" {
		return nil
	}
	return &view.Progress{JobID: job}
}

// models is the line of the catalog's models: the ones the screen lists (an adult model while the gate is closed is not counted: nothing says how many are hidden).
func models(svc *modelmanager.Service) (string, string) {
	all, installed, running := svc.Catalog(), 0, 0
	for _, m := range all {
		if m.State == modelmanager.ModelInstalled {
			installed++
		}
		if m.InstallJob != "" { // an installed model that fetches its optional files has a job too
			running++
		}
	}
	text := "導入済み " + strconv.Itoa(installed) + " / " + strconv.Itoa(len(all)) + " 件"
	return withRunning(text, running, installed, len(all), 0)
}

// withRunning adds the installs that are running to a line of "installed n / total" and says its state: installing, attention (attention > 0), empty, full or partial.
func withRunning(text string, running, installed, total, attention int) (string, string) {
	switch {
	case running > 0:
		return text + "（導入中 " + strconv.Itoa(running) + " 件）", view.StateInstalling
	case attention > 0:
		return text + "・要確認 " + strconv.Itoa(attention) + " 件", view.StateAttention
	case installed == 0:
		return text, view.StateEmpty
	case installed == total:
		return text, view.StateFull
	}
	return text, view.StatePartial
}

// customNodes is the line of the custom nodes: how many are installed, and a node that is not loaded (outdated or modified) asks for attention.
func customNodes(ctx context.Context, svc *modelnodes.Service) (string, string) {
	all, installed, attention, running := svc.List(ctx), 0, 0, 0
	for _, n := range all {
		switch n.State {
		case nodes.StateInstalled:
			installed++
		case nodes.StateOutdated, nodes.StateModified:
			attention++
		}
		if n.InstallJob != "" {
			running++
		}
	}
	text := "導入済み " + strconv.Itoa(installed) + " / " + strconv.Itoa(len(all)) + " 件"
	return withRunning(text, running, installed, len(all), attention)
}

// runtimes is the line of the local runtimes: how many are installed, how many are running.
func runtimes(ctx context.Context, svc *localruntime.Service) (string, string) {
	all, installed, started, running := svc.List(ctx), 0, 0, 0
	for _, r := range all {
		if r.Installed {
			installed++
		}
		if r.Process.State == proc.StateRunning {
			started++
		}
		if r.InstallJob != "" {
			running++
		}
	}
	text := "導入済み " + strconv.Itoa(installed) + " / " + strconv.Itoa(len(all)) + " 件・起動中 " + strconv.Itoa(started) + " 件"
	return withRunning(text, running, installed, len(all), 0)
}

// face is the line of the face-check models; its state is the one of the section (installed, unverified or missing).
func face(svc *facemodels.Service) (string, string) {
	switch st := svc.Status(); st.State {
	case facemodels.StateInstalled:
		return "導入済み", string(st.State)
	case facemodels.StateUnverified:
		return "ファイルあり（SHA-256 は未確認）", string(st.State)
	default:
		return "未導入", string(facemodels.StateMissing)
	}
}

// hf is the line of Hugging Face and the external ComfyUI: whether a token is set (its last characters, never the token), the files the user added and an import that is running.
func hf(svc *modelmanager.Service) (string, string) {
	state := "未設定"
	if ok, last, err := svc.HuggingFaceTokenStatus(); err == nil && ok {
		state = "設定済み（末尾 " + last + "）"
	}
	files := len(svc.UserModels())
	text := "トークン: " + state + "・追加したファイル " + strconv.Itoa(files) + " 件"
	switch {
	case len(svc.ActiveHFJobs()) > 0:
		return text + "（導入中）", view.StateInstalling
	case files == 0:
		return text, view.StateEmpty
	}
	return text, view.StateFull
}
