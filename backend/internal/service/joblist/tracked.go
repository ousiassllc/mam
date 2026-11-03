package joblist

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/logging"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// tracked reads the works that have no row in the job table (worktrack): what the owners keep in memory, as Entries of the same shape
// as the rows of the table. Each owner's list is read once per reading of the list.
func (r *reader) tracked(ctx context.Context) ([]Entry, error) {
	var out []Entry
	for _, src := range r.Works {
		for _, w := range src.Works() {
			e, err := r.work(ctx, w)
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		}
	}
	return out, nil
}

// work is one tracked work: it leads to the screen where it is worked on, names what it is on, and while it runs says in words what it
// does and, when it has a percentage, how far it is. A work that cannot say how far it is (an ffmpeg render has no steps to count) shows
// its words and the time it has run, so that a long one is seen to be going on.
func (r *reader) work(ctx context.Context, w worktrack.Record) (Entry, error) {
	e := Entry{
		ID: w.ID, Kind: string(w.Kind), Label: workLabel(w.Kind), State: State(w.State), StartedAt: w.StartedAt, FinishedAt: w.FinishedAt,
		Error: logging.Redact(w.Error),
	}
	var err error
	switch w.Kind {
	case worktrack.KindExport, worktrack.KindAudio, worktrack.KindEditorJudge:
		err = r.project(ctx, &e, w.ProjectID, ScreenProjectEditor)
	case worktrack.KindWorkflowRun:
		e.Dest = Dest{Screen: ScreenWorkflows}
		// A workflow whose model is hidden (an adult one while the age confirmation is off) has no name here (#992).
		if r.Hidden == nil || w.ModelID == "" || !r.Hidden(w.ModelID) {
			e.Subject = w.Subject
		}
	case worktrack.KindCodexInstall:
		e.Subject, e.Dest = w.Subject, Dest{Screen: ScreenChatGPT}
	case worktrack.KindFFmpegInstall:
		e.Subject, e.Dest = w.Subject, Dest{Screen: ScreenFFmpeg}
	}
	if e.State == Running {
		e.Progress = &Progress{Percent: w.Percent, Text: r.runningText(w)}
	}
	return e, err
}

// runningText is the words of a running work; of one with no percentage, with how long it has run.
func (r *reader) runningText(w worktrack.Record) string {
	if w.Percent != worktrack.NoPercent {
		return w.Text
	}
	elapsed := "経過 " + clock(int(r.now().Sub(w.StartedAt).Seconds()))
	if w.Text == "" {
		return elapsed
	}
	return w.Text + "・" + elapsed
}

func (r *reader) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// workLabel is what the screen calls a tracked kind of work.
func workLabel(k worktrack.Kind) string {
	switch k {
	case worktrack.KindExport:
		return "動画の書き出し"
	case worktrack.KindAudio:
		return "音声の作成"
	case worktrack.KindEditorJudge:
		return "Editor の判定"
	case worktrack.KindWorkflowRun:
		return "ワークフローの実行"
	case worktrack.KindCodexInstall:
		return "Codex の導入"
	case worktrack.KindFFmpegInstall:
		return "ffmpeg のダウンロード"
	}
	return string(k)
}
