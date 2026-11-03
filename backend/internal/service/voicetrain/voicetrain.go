// Package voicetrain fine-tunes the voice of a Character with GPT-SoVITS (Issue #936, spec/architecture/model-manager/local-runtime.md §16): the
// user gathers short recordings of a speaker with their transcripts (the clips of a Character), and a training job turns them into the two
// weights of a trained voice (the GPT part and the SoVITS part) that the synthesis loads for that Character in place of the zero-shot clone of its
// reference recording.
//
// The training runs on the local runtime of GPT-SoVITS (package localruntime) as a few processes in turn, each one fixed script of the package that
// mam's driver (data/overlays/mam_gptsovits_train.py) starts: nothing in a request names a command or a path. It is a work tracked in memory
// (package worktrack, like the export of a video): it has no row in the job table and a restart of mam ends it, because a training takes tens of
// minutes and mam does not resume it from a checkpoint (§16.5). Nothing partial is kept: a cancelled or failed training removes its working
// directory and the weights it had written.
//
// Like voicerefsvc it does not import package service: its refusals are *apperr.Error (the common error of #1010: the cause and the remedy in
// words) and the errors of voicerefsvc.
package voicetrain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// RuntimeID is the local runtime that trains (data/runtimes/gptsovits.json) and Script the driver of mam's own that it runs (its tool file).
const (
	RuntimeID = "gpt-sovits"
	Script    = "mam_gptsovits_train.py"
)

// Pretrained is the weight that only the training reads and the server does not (the discriminator of the SoVITS training, an asset that is not
// required to start the runtime): BeginTool refuses to start without it.
const Pretrained = "GPT-SoVITS v2 の SoVITS 判別器（学習用）"

// Errors. A refusal that has words of its own is an *apperr.Error.
var (
	// ErrNotFound: the Project, Series, Character or clip does not exist.
	ErrNotFound = voicerefsvc.ErrNotFound
	// ErrNoRun: no training of that Character is running.
	ErrNoRun = errors.New("voicetrain: no training is running")
)

// Held is the local runtime of GPT-SoVITS reserved for a training: it gives the launch of the driver, the directory the job works in, the GPU,
// and is given back with End (localruntime.Tool).
type Held interface {
	// Spec is the launch of the script of the runtime's tool files with args and env.
	Spec(script string, args, env []string) (proc.Spec, error)
	// WorkDir is the directory the job makes its own subdirectory in.
	WorkDir() string
	// GPU is the adapter's name and its video memory in GB (0 when unknown).
	GPU() (name string, vramGB int)
	End()
}

// Runtime reserves the runtime for a training: the refusals are those of localruntime.Service.BeginTool.
type Runtime interface {
	BeginTool(ctx context.Context, id string, assets ...string) (Held, error)
}

// Prober measures the length of an audio file (media.Tools).
type Prober interface {
	AudioDuration(ctx context.Context, path string) (float64, error)
}

// GPUGuard makes room on the GPU before a training starts, or refuses it: the bundled ComfyUI keeps its models in video memory and is not run
// together with a training (gpufree.Guard.Before).
type GPUGuard interface {
	Before(ctx context.Context) error
}

// Deps are what the Service needs. Probe may not be nil in a real use: without ffprobe the length of a clip is not known and the amount of data
// cannot be checked, so no clip is accepted.
type Deps struct {
	Voices  *voicerefsvc.Service
	Runtime Runtime
	Probe   Prober
	GPU     GPUGuard
	// Dir is the assets directory: the drafts of the clips and the trained voices are kept below it.
	AssetsDir string
	// FreeBytes reports the free space at a path (nil: fsutil.FreeBytes).
	FreeBytes func(path string) (uint64, error)
	Logger    *slog.Logger
	Now       func() time.Time
	// Stop is how long a step is given to end after it is asked to (nil: 2 s per stage of the stop; the group is killed after that).
	Stop time.Duration
}

// Service keeps the clips of the Characters and runs their trainings, one at a time (the GPU is one).
type Service struct {
	Deps
	works worktrack.Tracker

	mu    sync.Mutex
	cur   *run            // the training that runs now
	runs  map[string]*run // the trainings of this process by id (the latest of them)
	byKey map[key]*run    // the latest training of each Character
	order []string        // the ids of runs, oldest first
	wg    sync.WaitGroup
}

// New returns the Service.
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.FreeBytes == nil {
		d.FreeBytes = fsutil.FreeBytes
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Stop <= 0 {
		d.Stop = 2 * time.Second
	}
	s := &Service{Deps: d, runs: map[string]*run{}, byKey: map[key]*run{}}
	s.works.Now = d.Now
	return s
}

// Works is the trainings of this process, for the job list (worktrack.Source): the one that runs and the latest that ended. A restart forgets them.
func (s *Service) Works() []worktrack.Record { return s.works.Works() }

// Close cancels the training that runs and waits until its files are removed (shutdown).
func (s *Service) Close() {
	s.mu.Lock()
	cur := s.cur
	s.mu.Unlock()
	if cur != nil {
		cur.cancel(errCancelled)
	}
	s.wg.Wait()
}

var errCancelled = errors.New("the user cancelled the training")

// invalidf is a refusal of the input: the cause is the sentence, the remedy is the common one of the code.
func invalidf(format string, args ...any) *apperr.Error {
	return apperr.New(apperr.CodeInvalidInput).WithCause(fmt.Sprintf(format, args...))
}

// conflictf is a refusal of a state that is not the one the request needs.
func conflictf(format string, args ...any) *apperr.Error {
	return apperr.New(apperr.CodeConflict).WithCause(fmt.Sprintf(format, args...))
}

// newID is the id of a draft clip, a training and the directory of a trained voice.
func newID() string { return domain.NewID() }

// key is what tells the Characters apart: whose they are and which one.
type key struct {
	series bool
	owner  string
	char   string
}

func keyOf(o voicerefsvc.Owner, characterID string) key {
	return key{series: o.IsSeries(), owner: o.ID(), char: characterID}
}

// Trained is the trained voice of a Character, nil when it has none; an unknown Character is ErrNotFound.
func (s *Service) Trained(ctx context.Context, o voicerefsvc.Owner, characterID string) (*voiceref.Finetune, error) {
	ref, err := s.Voices.Reference(ctx, o, characterID)
	if err != nil || ref == nil {
		return nil, err
	}
	return ref.Finetune, nil
}

// RemoveTrained takes the trained voice off the Character: it speaks with the zero-shot clone of its recording again.
func (s *Service) RemoveTrained(ctx context.Context, o voicerefsvc.Owner, characterID string) error {
	return s.Voices.RemoveFinetune(ctx, o, characterID)
}
