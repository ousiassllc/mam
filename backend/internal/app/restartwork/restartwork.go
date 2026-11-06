// Package restartwork is what the app tells the restart service (service/apprestart) about the process it runs in: what is
// running that a restart would interrupt, and which saved settings wait for a restart. It is a package of its own, under app,
// so that app stays within its size.
package restartwork

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/apprestart"
	"github.com/ousiassllc/mam/backend/internal/service/restartpending"
)

// Busy lists what is running that a restart would interrupt: the jobs of the queue (running or queued; an install is a
// download), the editing stages and the AUTO chain, the ffmpeg processes, the runs of a saved workflow and the downloads of
// ffmpeg, of the pose library and of the Codex engine (codexSvc is nil when the app has none). A lookup that fails is a
// "check_failed" entry: nothing is assumed to be idle.
func Busy(services *service.Services, jobs *repository.JobRepository, ffmpegDL *ffmpegdl.Service, codexSvc *codex.Service) func(context.Context) []apprestart.Busy {
	return func(ctx context.Context) []apprestart.Busy {
		var busy []apprestart.Busy
		running, installs := map[domain.JobStatus]int{}, 0
		for _, status := range []domain.JobStatus{domain.JobRunning, domain.JobQueued} {
			list, err := jobs.ListByStatus(ctx, status)
			if err != nil {
				return append(busy, apprestart.Busy{Kind: apprestart.KindCheckFailed, Message: "動いている作業を確かめられませんでした"})
			}
			for _, j := range list {
				if j.Kind == domain.JobModelInstall {
					installs++
					continue
				}
				running[status]++
			}
		}
		if n := running[domain.JobRunning] + running[domain.JobQueued]; n > 0 {
			busy = append(busy, apprestart.Busy{Kind: apprestart.KindJobs, Message: fmt.Sprintf("生成などのジョブ %d 件（実行中 %d・待機 %d）", n, running[domain.JobRunning], running[domain.JobQueued])})
		}
		if installs > 0 {
			busy = append(busy, apprestart.Busy{Kind: apprestart.KindDownload, Message: fmt.Sprintf("モデル・エンジン・ランタイムの導入 %d 件", installs)})
		}
		if ffmpegDL.Status().Install.State == exeinstall.Running {
			busy = append(busy, apprestart.Busy{Kind: apprestart.KindDownload, Message: "ffmpeg のダウンロード"})
		}
		if services.Gentest != nil && services.Gentest.Poses != nil && services.Gentest.Poses.Progress().Running {
			busy = append(busy, apprestart.Busy{Kind: apprestart.KindDownload, Message: "ポーズ集の導入"})
		}
		if codexSvc != nil && codexSvc.InstallStatus().State == exeinstall.Running {
			busy = append(busy, apprestart.Busy{Kind: apprestart.KindDownload, Message: "Codex エンジンの導入"})
		}
		if services.Editor.Busy() {
			busy = append(busy, apprestart.Busy{Kind: apprestart.KindEditing, Message: "編集の工程（Rough Cut・判定・音声・Final QC・書き出し）または AUTO の連鎖"})
		}
		if n := media.Running(); n > 0 {
			busy = append(busy, apprestart.Busy{Kind: apprestart.KindMedia, Message: fmt.Sprintf("動画の変換（ffmpeg）%d 件", n)})
		}
		if n := workflowRuns(services); n > 0 {
			busy = append(busy, apprestart.Busy{Kind: apprestart.KindWorkflow, Message: fmt.Sprintf("ワークフローの実行（ComfyUI）%d 件", n)})
		}
		return busy
	}
}

// workflowRuns is how many runs of a saved workflow are running; an app without workflows has none.
func workflowRuns(services *service.Services) int {
	if services.Workflows == nil {
		return 0
	}
	return services.Workflows.Running()
}

// LLMApplier is what a saved LLM assignment is handed to (llmassign.Service.SetApplier): the running client takes the routes at
// once, and what only a restart applies (the base URLs, the timeout, the retry policy and the breakers of llm.json edited by hand)
// is listed as waiting for one.
func LLMApplier(client *llm.Client, pending *restartpending.Registry) func(llm.Config) []string {
	return func(c llm.Config) []string {
		restart := client.Apply(c)
		var reasons []string
		for _, r := range restart {
			reasons = append(reasons, "llm.json の"+r)
		}
		pending.Set("llm", reasons)
		return restart
	}
}

// New is the restart service of the app (spec/architecture/restart.md) with the registry of the settings that wait for a restart:
// restart asks the front to quit and start the app again (app.Options.Restart; nil in the browser build, which cannot), and what a
// restart would break is read by Busy.
func New(restart func(), services *service.Services, jobs *repository.JobRepository, ffmpegDL *ffmpegdl.Service, codexSvc *codex.Service, logger *slog.Logger) (*apprestart.Service, *restartpending.Registry) {
	pending := restartpending.New()
	return apprestart.New(apprestart.Deps{
		Restart: restart, Busy: Busy(services, jobs, ffmpegDL, codexSvc), Pending: pending, Logger: logger,
		BootID: domain.NewID(), StartedAt: time.Now().UTC(),
	}), pending
}
