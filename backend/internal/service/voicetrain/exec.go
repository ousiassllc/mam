package voicetrain

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// execute runs the stages in turn and ends the run. Whatever happens, the processes are ended, the working directory is removed and the weights
// written but not bound to the Character are removed, and the runtime is given back.
func (s *Service) execute(ctx context.Context, r *run, held Held, j job, clips []Clip, secs float64) {
	var written string // the directory of the weights, until they are bound
	defer s.wg.Done()
	defer func() {
		_ = os.RemoveAll(j.Work)
		if written != "" {
			_ = os.RemoveAll(written)
		}
		held.End()
		s.release(r)
		close(r.done)
	}()
	defer func() {
		if p := recover(); p != nil {
			s.Logger.Error("voice training panicked", "panic", p)
			r.finish(worktrack.Failed, fmt.Errorf("panic: %v", p), apperr.New(apperr.CodeTrainFailed).WithDetail(fmt.Sprint(p)), nil, s.Now())
		}
	}()

	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	go func() {
		for {
			select {
			case <-tick.C:
				r.publish(s.Now())
			case <-ctx.Done():
				return
			case <-r.done:
				return
			}
		}
	}()

	err := s.pipeline(ctx, r, held, j)
	var ft *voiceref.Finetune
	if err == nil {
		ft, written, err = s.keep(ctx, r, j, len(clips), secs)
	}
	switch {
	case errors.Is(context.Cause(ctx), errCancelled):
		r.finish(worktrack.Cancelled, nil, nil, nil, s.Now())
	case err != nil:
		failure := s.failure(err)
		s.Logger.Warn("voice training failed", "id", r.id, "err", err)
		r.finish(worktrack.Failed, err, failure, nil, s.Now())
	default:
		written = "" // bound: the weights stay
		r.finish(worktrack.Succeeded, nil, nil, ft, s.Now())
	}
}

// pipeline runs the stages of the training in order.
func (s *Service) pipeline(ctx context.Context, r *run, held Held, j job) error {
	for i := range stages {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		r.begin(i, s.Now())
		if err := s.runStage(ctx, r, held, i, j); err != nil {
			return err
		}
	}
	return nil
}

// stageError is a stage whose process ended with a code other than 0. Tail is the end of what it wrote to its standard error.
type stageError struct {
	stage int
	code  int
	tail  string
}

func (e *stageError) Error() string {
	return fmt.Sprintf("%s（%d / %d 段階）が終了コード %d で止まりました", stages[e.stage].label, e.stage+1, len(stages), e.code)
}

// runStage runs the process of the stage and follows what it reports on its standard output ("MAM_TRAIN {json}" lines of the driver) until it ends.
// A cancelled context ends the process (and everything it started) before it returns.
func (s *Service) runStage(ctx context.Context, r *run, held Held, i int, j job) error {
	spec, err := held.Spec(Script, []string{stages[i].step, j.configPath}, nil)
	if err != nil {
		return fmt.Errorf("prepare the %s step: %w", stages[i].step, err)
	}
	spec.LogPath = filepath.Join(j.Work, "train-"+stages[i].step+".log")
	spec.Name = "GPT-SoVITS training"
	p, err := proc.StartPiped(spec, s.Logger)
	if err != nil {
		return fmt.Errorf("start the %s step: %w", stages[i].step, err)
	}
	read := make(chan struct{})
	go func() {
		defer close(read)
		sc := bufio.NewScanner(p.Stdout)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			if done, total, ok := parseProgress(sc.Text(), stages[i].step); ok {
				r.counted(done, total, s.Now())
			}
		}
	}()
	select {
	case <-p.Done():
	case <-ctx.Done():
		p.Stop(s.Stop)
	}
	select {
	case <-read:
	case <-time.After(5 * time.Second): // a grandchild that kept the pipe open: the group was killed with the child
	}
	_ = p.Stdout.Close()
	_ = p.Stdin.Close()
	if err := context.Cause(ctx); err != nil {
		return err
	}
	if code := p.ExitCode(); code != 0 {
		return &stageError{stage: i, code: code, tail: p.LogTail()}
	}
	return nil
}

// parseProgress reads a line of the driver: {"step": "sovits", "done": 3, "total": 8}. A line of another step, or anything else, is not progress.
func parseProgress(line, step string) (done, total int, ok bool) {
	raw, found := strings.CutPrefix(line, "MAM_TRAIN ")
	if !found {
		return 0, 0, false
	}
	var m struct {
		Step  string `json:"step"`
		Done  int    `json:"done"`
		Total int    `json:"total"`
	}
	if json.Unmarshal([]byte(raw), &m) != nil || m.Step != step || m.Total <= 0 || m.Done < 0 {
		return 0, 0, false
	}
	return m.Done, m.Total, true
}

// keep moves the two weights the driver wrote to the directory of the trained voice below the assets directory and binds them to the Character. It
// returns the directory it wrote (so that the caller removes it unless it is bound); nothing is written under its final name until both files are there.
func (s *Service) keep(ctx context.Context, r *run, j job, nclips int, secs float64) (*voiceref.Finetune, string, error) {
	id := newID()
	final := s.Voices.WeightsDir(id)
	if final == "" {
		return nil, "", errors.New("there is no assets directory to keep the trained voice in")
	}
	staging := filepath.Join(filepath.Dir(final), voicerefsvc.StagingPrefix+id)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return nil, "", fmt.Errorf("create the directory of the trained voice: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	var sizes [2]int64
	for n, name := range []string{voiceref.GPTFileName, voiceref.SoVITSFileName} {
		if err := fsutil.CopyFile(filepath.Join(j.Out, name), filepath.Join(staging, name)); err != nil {
			return nil, "", fmt.Errorf("keep %s: %w", name, err)
		}
		st, err := os.Stat(filepath.Join(staging, name))
		if err != nil || st.Size() == 0 {
			return nil, "", fmt.Errorf("the training wrote an empty %s", name)
		}
		sizes[n] = st.Size()
	}
	if err := os.Rename(staging, final); err != nil {
		return nil, "", fmt.Errorf("keep the trained voice: %w", err)
	}
	ft := voiceref.Finetune{
		ID: id, GPTSizeBytes: sizes[0], SoVITSSizeBytes: sizes[1], Version: voiceref.FinetuneVersion, GPTEpochs: j.EpochsS1, SoVITSEpochs: j.EpochsS2,
		Clips: nclips, DataSeconds: int(secs), TrainedAt: s.Now().UTC(),
	}
	if err := s.Voices.BindFinetune(context.WithoutCancel(ctx), r.owner, r.key.char, ft); err != nil {
		return nil, final, bindError(err)
	}
	return &ft, final, nil
}

func bindError(err error) error {
	switch {
	case errors.Is(err, voicerefsvc.ErrNoReference), errors.Is(err, voicerefsvc.ErrNotFound):
		return conflictf("学習のあいだに、この登場人物の参照音声（または登場人物そのもの）がなくなったため、学習した声を結び付けられませんでした")
	}
	return fmt.Errorf("bind the trained voice: %w", err)
}

// failure is the common error of a training that failed: the cause of what went wrong, as the trainer's own output says it when it names one
// (the GPU or the disk running out), else the stage that stopped and the end of its log.
func (s *Service) failure(err error) *apperr.Error {
	if ae, ok := apperr.As(err); ok {
		return ae
	}
	var se *stageError
	if errors.As(err, &se) {
		tail := strings.ToLower(se.tail)
		switch {
		case strings.Contains(tail, "out of memory"):
			return apperr.New(apperr.CodeTrainFailed).WithCause("学習中に GPU のメモリが足りなくなりました。").WithDetail(se.Error() + "\n" + se.tail)
		case strings.Contains(tail, "no space left") || strings.Contains(tail, "not enough space"):
			return apperr.New(apperr.CodeDiskFull).With("where", "（学習の途中で）").WithDetail(se.Error() + "\n" + se.tail)
		}
		return apperr.New(apperr.CodeTrainFailed).WithCause("声の学習が途中で止まりました。" + se.Error() + "。").WithDetail(se.tail)
	}
	return apperr.New(apperr.CodeTrainFailed).Wrap(err)
}
