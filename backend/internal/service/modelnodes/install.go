package modelnodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/instlog"
)

// JobRef is a started job: poll it with JobStatus. Existing is true when the same work was already queued or running and no new job was
// made (the same shape as modelmanager.JobRef).
type JobRef struct {
	JobID    string `json:"job_id"`
	Existing bool   `json:"existing,omitempty"`
}

// LicenseRef names what a user accepted: the ID and the version of the license, and the version of the security notice, as the screen showed them.
type LicenseRef struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	NoticeVersion string `json:"notice_version"`
}

type installPayload struct {
	Target string `json:"target"`
}

func notFound(err error) error {
	if errors.Is(err, nodes.ErrUnknown) {
		return modelmanager.ErrNotFound
	}
	return err
}

// Accept records that the user accepted the license of the node and the security notice (ID and versions as the list gives them). An install
// refuses without it: the API enforces it, not only the screen. A new version of the license or of the notice asks for a new acceptance.
func (s *Service) Accept(id string, ref LicenseRef) error {
	n, ok := s.Nodes.Allowlist().Node(id)
	if !ok {
		return modelmanager.ErrNotFound
	}
	if ref.ID != n.License.ID || ref.Version != n.License.Version || ref.NoticeVersion != NoticeVersion {
		return invalid("同意するライセンスと注意事項が違います（%s %s・注意事項 %s が必要です）", n.License.ID, n.License.Version, NoticeVersion)
	}
	return s.Nodes.Accept(id, nodes.Consent{LicenseID: ref.ID, LicenseVersion: ref.Version, NoticeVersion: ref.NoticeVersion, AcceptedAt: s.Now().UTC()})
}

// Install fetches the pinned archive of the node and installs it, as a job (JobStatus). It refuses at once, before anything is fetched: a node
// that mam does not install (the reason is the message), a machine without the bundled engine, a license and security notice that were not
// accepted (accept records one), a folder that cannot be written to and a disk without the room.
func (s *Service) Install(ctx context.Context, id string, accept *LicenseRef) (JobRef, error) {
	n, ok := s.Nodes.Allowlist().Node(id)
	if !ok {
		return JobRef{}, modelmanager.ErrNotFound
	}
	if !n.Installable() {
		return JobRef{}, invalid("%s は導入できません: %s", n.Name, n.InstallBlocked)
	}
	if msg := s.unavailable(); msg != "" {
		ev := s.Engine.Engine()
		switch {
		case !ev.Supported:
			return JobRef{}, modelmanager.ErrUnsupported
		case !ev.Installed || ev.Broken:
			return JobRef{}, modelmanager.ErrEngineNotInstalled
		}
		return JobRef{}, invalid("%s", msg)
	}
	if accept != nil {
		if err := s.Accept(id, *accept); err != nil {
			return JobRef{}, err
		}
	}
	if c, ok := s.Nodes.Consent(id); !ok || !s.consentCurrent(n, c) {
		return JobRef{}, invalid("%s のライセンス（%s）と注意事項に同意してから導入してください", n.Name, n.License.Name)
	}
	if err := s.Nodes.CheckSpace(n); err != nil {
		return JobRef{}, err
	}
	target := TargetPrefix + id
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.active[target]; ok {
		return JobRef{JobID: job, Existing: true}, nil
	}
	job, err := s.Queue.Enqueue(ctx, domain.JobModelInstall, installPayload{Target: target})
	if err != nil {
		return JobRef{}, err
	}
	s.active[target] = job.ID
	return JobRef{JobID: job.ID}, nil
}

// Handler is the handler of domain.JobModelInstall that takes the jobs of a node and gives every other job to next (the queue keeps one handler
// per kind). It MUST be built before the queue starts.
func (s *Service) Handler(next jobqueue.Handler) jobqueue.Handler {
	return func(ctx context.Context, job domain.Job) error {
		var p installPayload
		id, ok := "", false
		if json.Unmarshal(job.Payload, &p) == nil {
			id, ok = strings.CutPrefix(p.Target, TargetPrefix)
		}
		if !ok {
			return next(ctx, job)
		}
		return s.handleInstall(ctx, job, p.Target, id)
	}
}

func (s *Service) handleInstall(ctx context.Context, job domain.Job, target, id string) error {
	s.mu.Lock()
	s.active[target] = job.ID // a job resumed after a restart is active again
	s.mu.Unlock()
	defer s.release(target, job.ID)
	defer s.tracker.Finish(job.ID)
	set := func(u track.Update) { s.tracker.Set(job.ID, target, u) }
	ctx, done := s.cancellable(instlog.WithJob(ctx, job.ID, target), job.ID)
	defer done()
	err := s.runInstall(ctx, id, set)
	stopped := errors.Is(context.Cause(ctx), errCancelled)
	instlog.Finished(ctx, s.Logger, job.ID, target, err, err != nil && stopped)
	if err != nil && stopped {
		return errCancelled
	}
	return userFacing(ctx, err)
}

// The weights of the steps of an install, in percent: the download, the extraction, then the restart of the engine and the check that it loaded the node.
const (
	pctDownload = 50
	pctExtract  = 30
)

// runInstall installs the node. The engine is stopped for the swap of the folder (it holds the modules of a loaded node open) and, when it was
// running, started again with the node in its whitelist and asked for the classes of the node: a node that ComfyUI did not load is taken out
// again, so that "installed" always means "loaded at the install".
func (s *Service) runInstall(ctx context.Context, id string, set func(track.Update)) error {
	n, ok := s.Nodes.Allowlist().Node(id)
	if !ok {
		return fmt.Errorf("node %q is not in the allowlist", id)
	}
	wasRunning := s.engineRunning()
	err := s.Nodes.Install(ctx, id, func(ph nodes.Phase, done, total int64) {
		pct, msg := 0, n.Name+" をダウンロードしています"
		if total > 0 {
			pct = int(float64(done) / float64(total) * pctDownload)
		}
		if ph == nodes.PhaseExtract {
			pct, msg = pctDownload, n.Name+" を展開しています"
			if total > 0 {
				pct = pctDownload + int(float64(done)/float64(total)*pctExtract)
			}
		}
		set(track.Update{Percent: pct, Phase: string(ph), Message: msg, DoneBytes: done, TotalBytes: total})
	}, s.stopEngine)
	if err != nil {
		s.restart(ctx, wasRunning)
		return err
	}
	if !wasRunning {
		set(track.Update{Percent: 100, Phase: "done", Message: "導入しました（次に ComfyUI を起動したときに読み込みます）"})
		return nil
	}
	set(track.Update{Percent: pctDownload + pctExtract, Phase: "restart", Message: "ComfyUI を再起動してノードを確認しています"})
	if err := s.verify(ctx, n); err != nil {
		if rerr := s.removeNode(ctx, id); rerr != nil {
			s.Logger.Warn("take out the node that did not load failed", "node", id, "err", rerr)
		}
		s.restart(ctx, true)
		return err
	}
	set(track.Update{Percent: 100, Phase: "done", Message: "導入しました"})
	return nil
}

// verify restarts the engine and asks it for the classes of the node.
func (s *Service) verify(ctx context.Context, n nodecat.Node) error {
	st, err := s.Engine.StartEngine(ctx)
	if err != nil {
		return &userError{msg: "ノードは導入しましたが、ComfyUI を再起動できませんでした。ノードを取り消しました。comfyui の logs\\comfyui.log を確認してください", err: err}
	}
	missing, err := s.Loaded(ctx, st.URL, n.NodeClasses)
	if err != nil {
		return &userError{msg: "ComfyUI にノードの読み込みを確かめられませんでした。ノードを取り消しました", err: err}
	}
	if len(missing) > 0 {
		return &userError{msg: fmt.Sprintf("ComfyUI が %s を読み込めませんでした（見つからないノード: %s）。ノードを取り消しました。原因は comfyui の logs\\comfyui.log にあります", n.Name, strings.Join(missing, "、")),
			err: errors.New("the engine did not load the node")}
	}
	return nil
}

// engineRunning says the engine process is up or on its way up.
func (s *Service) engineRunning() bool {
	st := s.Engine.Engine().Process.State
	return st == proc.StateRunning || st == proc.StateStarting
}

func (s *Service) stopEngine() error {
	if !s.engineRunning() {
		return nil
	}
	return s.Engine.StopEngine()
}

// restart starts the engine again after a change of the nodes when it was running; a failure is logged and shows on the engine's card.
// It does not look at ctx: an install the user stopped (or a request that ended) while the engine was down for the swap must still bring the
// engine back, so the start is made on a context that cannot be cancelled.
func (s *Service) restart(ctx context.Context, wasRunning bool) {
	if !wasRunning {
		return
	}
	if _, err := s.Engine.StartEngine(context.WithoutCancel(ctx)); err != nil {
		s.Logger.Warn("start the bundled ComfyUI after a change of the nodes failed", "err", err)
	}
}

// Remove deletes the node: the engine is stopped, the folder and the record go, and the engine is started again when it was running. The
// models that need the node stay installed and cannot be chosen until it is installed again. An install of the node that is queued or running
// is modelmanager.ErrBusy; a node that is not installed is modelmanager.ErrNotInstalled.
func (s *Service) Remove(ctx context.Context, id string) error {
	if _, ok := s.Nodes.Allowlist().Node(id); !ok {
		return modelmanager.ErrNotFound
	}
	if s.activeJob(TargetPrefix+id) != "" {
		return modelmanager.ErrBusy
	}
	wasRunning := s.engineRunning()
	err := s.removeNode(ctx, id)
	s.restart(ctx, wasRunning)
	return err
}

func (s *Service) removeNode(_ context.Context, id string) error {
	if err := s.stopEngine(); err != nil {
		return err
	}
	if err := s.Nodes.Remove(id); err != nil {
		if errors.Is(err, nodes.ErrNotInstalled) {
			return modelmanager.ErrNotInstalled
		}
		return err
	}
	return nil
}

// userFacing turns a failure of an install into the message the screen shows. A cancelled context stays as it is: the queue decides what an
// interruption means.
func userFacing(ctx context.Context, err error) error {
	if err == nil || ctx.Err() != nil {
		return err
	}
	var ns *fetch.NoSpaceError
	var he *fetch.HTTPError
	switch {
	case errors.As(err, &ns):
		return &userError{fmt.Sprintf("空き容量が足りません（必要 %s、空き %s、場所 %s）", fetch.HumanBytes(ns.Need), fetch.HumanBytes(ns.Free), ns.Path), err}
	case errors.Is(err, fetch.ErrChecksum):
		return &userError{"ダウンロードしたファイルの SHA-256 が、許可リストに固定した値と一致しません。ダウンロードを破棄しました（取得元の改ざんか転送の破損の可能性があります。GitHub のアーカイブの作り直しの場合は、mam の更新を待ってください）", err}
	case errors.Is(err, fetch.ErrSize):
		return &userError{"ダウンロードしたファイルの大きさが固定した値と違います。ダウンロードを破棄しました", err}
	case errors.Is(err, nodes.ErrNotAPackage):
		return &userError{"取得したアーカイブが ComfyUI のノードの形（__init__.py）ではありません。導入しませんでした", err}
	case errors.As(err, &he) && he.Definitive():
		return &userError{fmt.Sprintf("取得元が応答しません（HTTP %d）。取得元のリポジトリが移動または削除された可能性があります", he.Status), err}
	case instlog.IsLost(err):
		return &userError{instlog.ConnectionLostMessage, err}
	case errors.Is(err, store.ErrNotWritable):
		return &userError{"mam.exe のあるフォルダに書き込めません。書き込める場所に置いてください", err}
	}
	return err
}

// userError carries the message for the user and the error behind it, which errors.Is / As still reach.
type userError struct {
	msg string
	err error
}

func (e *userError) Error() string { return e.msg }
func (e *userError) Unwrap() error { return e.err }
