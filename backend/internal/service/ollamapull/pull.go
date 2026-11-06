package ollamapull

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// errCancelled is why a pull the user stopped failed. The job is failed, not requeued: Ollama keeps the layers it fetched, so pulling again goes on from them.
var errCancelled = apperr.WithText("取得を中断しました。もう一度取得すると、取得済みの分から続きます（Ollama が途中の層を残します）", apperr.New(apperr.CodeInstallCancelled))

// JobRef is a started job: poll it with JobStatus. Existing is true when the same pull was already queued or running and no new job was made (the same shape as modelmanager.JobRef).
type JobRef struct {
	JobID    string `json:"job_id"`
	Existing bool   `json:"existing,omitempty"`
}

type payload struct {
	Target string `json:"target"`
}

// Pull fetches the model of the list called name, as a job (JobStatus). It refuses at once, before the server is asked anything: a name that is not in the list (*InvalidError), a server that is
// not installed or cannot run here (the errors of localruntime), an install that is running (localruntime.ErrBusy) and a disk without the room for the model and 1 GiB (*fetch.NoSpaceError).
// A server that is stopped is no reason to refuse: the job starts it. The same model that is being pulled is the same job (Existing).
func (s *Service) Pull(ctx context.Context, name string) (JobRef, error) {
	m, err := s.find(name)
	if err != nil {
		return JobRef{}, err
	}
	rv, err := s.Runtime.Status(ctx, RuntimeID)
	if err != nil {
		return JobRef{}, err
	}
	switch server, _ := serverOf(rv); server {
	case ServerUnsupported:
		return JobRef{}, &localruntime.SupportError{Runtime: RuntimeID, Support: rv.Support}
	case ServerNotInstalled:
		return JobRef{}, localruntime.ErrNotInstalled
	case ServerInstalling:
		return JobRef{}, localruntime.ErrBusy
	}
	target := TargetPrefix + m.Name
	if job := s.activeJob(target); job != "" {
		return JobRef{JobID: job, Existing: true}, nil
	}
	have, err := s.free()
	if err != nil {
		return JobRef{}, fmt.Errorf("free space: %w", err)
	}
	if need := uint64(m.TotalBytes) + spaceMargin; need > have { //nolint:gosec // a validated size
		return JobRef{}, &fetch.NoSpaceError{Need: need, Free: have, Path: s.layout.ModelsDir()}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.active[target]; ok {
		return JobRef{JobID: job, Existing: true}, nil
	}
	job, err := s.Queue.Enqueue(ctx, domain.JobModelInstall, payload{Target: target})
	if err != nil {
		return JobRef{}, err
	}
	s.active[target] = job.ID
	return JobRef{JobID: job.ID}, nil
}

// Handler is the handler of domain.JobModelInstall that takes the jobs of a pull and gives every other job to next (the queue keeps one handler per kind). It MUST be built before the queue
// starts, and the runtimes' handler wraps it (app.newLocalRuntimes).
func (s *Service) Handler(next jobqueue.Handler) jobqueue.Handler {
	return func(ctx context.Context, job domain.Job) error {
		var p payload
		name, ok := "", false
		if json.Unmarshal(job.Payload, &p) == nil {
			name, ok = strings.CutPrefix(p.Target, TargetPrefix)
		}
		if !ok {
			return next(ctx, job)
		}
		return s.handlePull(ctx, job, p.Target, name)
	}
}

func (s *Service) handlePull(ctx context.Context, job domain.Job, target, name string) error {
	s.mu.Lock()
	s.active[target] = job.ID // a job resumed after a restart is active again
	s.mu.Unlock()
	defer s.release(target, job.ID)
	defer s.tracker.Finish(job.ID)
	ctx, done := s.cancellable(ctx, job.ID)
	defer done()
	err := s.runPull(ctx, job.ID, target, name)
	if err != nil && errors.Is(context.Cause(ctx), errCancelled) {
		return errCancelled
	}
	return err
}

// runPull starts the server when it is stopped, streams the pull and compares the manifest. It reports to the tracker of the job.
func (s *Service) runPull(ctx context.Context, jobID, target, name string) error {
	m, err := s.find(name)
	if err != nil {
		return err
	}
	set := func(u track.Update) { s.tracker.Set(jobID, target, u) }
	set(track.Update{Phase: "manifest", Message: "Ollama を準備しています"})
	if err := s.Runtime.Prepare(ctx, RuntimeID); err != nil {
		return s.startFailure(ctx, err)
	}
	rv, err := s.Runtime.Status(ctx, RuntimeID)
	if err != nil {
		return err
	}
	baseURL := processURL(rv)
	if baseURL == "" {
		return &userError{msg: "Ollama が起動していません。モデルマネージャーのカードで状態を確かめてください" + logSuffix(rv.LogTail)}
	}
	body, err := json.Marshal(map[string]any{"model": m.Name, "stream": true})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &userError{msg: "Ollama に接続できませんでした", err: httpclient.RedactURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return &userError{msg: explainError(extractError(data, res.Status)), err: fmt.Errorf("ollama: /api/pull answered %s", res.Status)}
	}
	if err := s.readStream(ctx, res.Body, newProgress(m.TotalBytes), set); err != nil {
		return err
	}
	set(track.Update{Percent: 100, Phase: "done", Message: "取得しました", DoneBytes: m.TotalBytes, TotalBytes: m.TotalBytes})
	if s.CheckManifest(m) == CheckChanged {
		s.Logger.Warn("the manifest of a pulled Ollama model differs from the record of the catalog", "model", m.Name)
	}
	return nil
}

// readStream reads the NDJSON of the pull until its "success" line. An "error" line, a stream that ends without success and a read that fails are failures in words.
func (s *Service) readStream(ctx context.Context, r io.Reader, p *progress, set func(track.Update)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var l line
		if err := json.Unmarshal(raw, &l); err != nil {
			continue // a line that is not one of Ollama's (a progress that mam does not read) changes nothing
		}
		if l.Error != "" {
			return &userError{msg: explainError(l.Error), err: errors.New("ollama pull: " + l.Error)}
		}
		u, ok := p.add(l)
		set(u)
		if ok {
			return nil
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := sc.Err(); err != nil {
		return &userError{msg: "Ollama との接続が途中で切れました。もう一度取得すると、取得済みの分から続きます", err: err}
	}
	return &userError{msg: "Ollama が最後まで応答しませんでした（success の行がありません）。もう一度取得すると、取得済みの分から続きます"}
}

// startFailure is the failure of the start of the server before a pull, with the end of its log.
func (s *Service) startFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if rv, e := s.Runtime.Status(ctx, RuntimeID); e == nil {
		return &userError{msg: "Ollama を起動できませんでした" + logSuffix(rv.LogTail), err: err}
	}
	return &userError{msg: "Ollama を起動できませんでした", err: err}
}

func logSuffix(tail string) string {
	if tail = strings.TrimSpace(tail); tail == "" {
		return ""
	}
	if len(tail) > 600 {
		tail = tail[len(tail)-600:]
	}
	return "（ログの末尾: " + tail + "）"
}

// extractError is the message of an error answer: {"error": "..."} of Ollama, else the status.
func extractError(data []byte, status string) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" {
		return e.Error
	}
	return status
}

// explainError is a message of Ollama as a sentence for the user, with the message itself after it (so that what Ollama said is not lost).
func explainError(msg string) string {
	lower := strings.ToLower(msg)
	var why string
	switch {
	case strings.Contains(lower, "no space left"), strings.Contains(lower, "not enough space"), strings.Contains(lower, "disk full"):
		why = "ディスクの空きが足りません"
	case strings.Contains(lower, "file does not exist"), strings.Contains(lower, "manifest unknown"), strings.Contains(lower, "not found"):
		why = "Ollama のレジストリにこの名前のモデルがありません"
	case strings.Contains(lower, "digest mismatch"), strings.Contains(lower, "verification"), strings.Contains(lower, "sha256"):
		why = "ダウンロードしたデータの検証に失敗しました"
	case strings.Contains(lower, "dial tcp"), strings.Contains(lower, "no such host"), strings.Contains(lower, "timeout"), strings.Contains(lower, "connection"),
		strings.Contains(lower, "eof"), strings.Contains(lower, "tls"), strings.Contains(lower, "network"):
		why = "ネットワークに接続できないか、途中で切れました"
	default:
		why = "Ollama が取得に失敗しました"
	}
	return why + "（Ollama の応答: " + strings.TrimSpace(msg) + "）。もう一度取得すると、取得済みの分から続きます"
}

// userError carries the message for the user and the error behind it, which errors.Is / As still reach.
type userError struct {
	msg string
	err error
}

func (e *userError) Error() string       { return e.msg }
func (e *userError) UserMessage() string { return e.msg } // mmview.Failure words the failure with it (#1010)
func (e *userError) Unwrap() error       { return e.err }

// cancellable returns ctx with a cancel that Cancel reaches by the id of the job, and what to call when the handler ends.
func (s *Service) cancellable(ctx context.Context, jobID string) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	s.mu.Lock()
	s.cancels[jobID] = cancel
	s.mu.Unlock()
	return ctx, func() {
		s.mu.Lock()
		delete(s.cancels, jobID)
		s.mu.Unlock()
		cancel(nil)
	}
}

// Cancel stops a running pull by the id of its job: the connection to /api/pull is closed. The job ends as failed with a message that says so; Ollama keeps the layers it fetched, so pulling again
// goes on from them. A job that is not running (queued, ended, or not a pull's) is modelmanager.ErrNotFound.
func (s *Service) Cancel(jobID string) error {
	s.mu.Lock()
	cancel, ok := s.cancels[jobID]
	s.mu.Unlock()
	if !ok {
		return modelmanager.ErrNotFound
	}
	cancel(errCancelled)
	return nil
}
