// Package shotaudiosvc ties an audio asset of a Project to a Shot (Issue #1153): the sound a speech-to-video model (Wan 2.2 S2V) makes the Shot's clip from. The Shot screen,
// the REST API, the MCP tools and the chat operation all go through Service, so that every entry point judges the same rules (shotaudiorepo.Repository.Set, in one
// transaction with the write) and says the same sentences. Like voicerefsvc it does not import package service (which holds it and is at its line limit): its refusals are
// ErrNotFound, ErrBusy, *InvalidError and *LockedError, and each entry point turns them into its own.
package shotaudiosvc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gentestcfg"
	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/shotaudio"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/shotaudiorepo"
)

// Deps are what the Service needs. Record and MaxSeconds may be nil.
type Deps struct {
	DB     *repository.DB
	Shots  *repository.GenerationRepository
	Assets *repository.AssetRepository
	// AssetsDir is where the files of the assets are (the audio is read to tell its length).
	AssetsDir string
	// Record notes a hand edit for the Production Analytics (it never fails the edit); nil: none is noted.
	Record func(ctx context.Context, in domain.Intervention)
	// MaxSeconds is the longest sound a model takes (gentest.max_audio_seconds); nil: the default.
	MaxSeconds func() int
}

// Service sets, clears and shows the audio of Shots.
type Service struct {
	repo *shotaudiorepo.Repository
	d    Deps
}

// SecondsOf is Deps.MaxSeconds for the Model Adapter's running setting (gentest.max_audio_seconds); nil without an Adapter.
func SecondsOf(a *modeladapter.Adapter) func() int {
	if a == nil {
		return nil
	}
	return func() int { return a.Config().Gentest.AudioSeconds() }
}

// New returns the Service.
func New(d Deps) *Service { return &Service{repo: shotaudiorepo.New(d.DB), d: d} }

// ErrNotFound means the Shot, the Project's Shot or the audio asset does not exist (an asset of another Project is not found either).
var ErrNotFound = errors.New("shotaudiosvc: not found")

// ErrBusy means the Shot is being generated or repaired: the generation reads the Shot's audio, so it is not changed meanwhile (409).
var ErrBusy = errors.New("shotaudiosvc: the Shot is being generated or repaired")

// InvalidError is input that cannot be accepted. Field names the form field; Message is shown to the user.
type InvalidError struct{ Field, Message string }

func (e *InvalidError) Error() string { return "invalid input: " + e.Field + ": " + e.Message }

// LockedError means a LOCKed parameter of the Shot stops the change (423). It is repository.ErrLocked for errors.Is.
type LockedError struct{ Parameters []domain.LockParameter }

func (e *LockedError) Error() string {
	return fmt.Sprintf("%v: %v", repository.ErrLocked, e.Parameters)
}

// Is makes errors.Is(err, repository.ErrLocked) hold.
func (e *LockedError) Is(target error) bool { return target == repository.ErrLocked }

// Tie is the audio a Shot is tied to, as the Shot screen shows it.
type Tie struct {
	Asset domain.Asset
	// Seconds is the length of the sound (0 when it cannot be read); Problem says why the tie cannot be used now (""): a generation of the Shot would refuse with it.
	Seconds float64
	Problem string
}

// View is the audio of one Shot and the audio assets of its Project it can be tied to.
type View struct {
	ShotID    string
	ProjectID string
	// Scene and Number are the Scene and the Shot as the screens count them (1-based).
	Scene, Number int
	// Tie is nil when the Shot has no audio.
	Tie     *Tie
	Choices []domain.Asset
	// Locked is true when the Prompt of the Shot is LOCKed: the tie cannot be changed.
	Locked bool
	// Busy is true while the Shot is being generated or repaired: the generation reads the audio, so the tie is not changed meanwhile.
	Busy bool
	// MaxSeconds is the longest sound a model takes (gentest.max_audio_seconds). Pictures is how many images (Keyframe, Reference Pack) the Shot has: a speech model
	// draws the speaker from one of them, so 0 means a generation of the Shot with this audio is refused.
	MaxSeconds, Pictures int
}

// Get is the audio of the Shot of the Project (projectID "": whichever Project the Shot is in).
func (s *Service) Get(ctx context.Context, projectID, shotID string) (View, error) {
	ref, err := s.shot(ctx, projectID, shotID)
	if err != nil {
		return View{}, err
	}
	v := View{
		ShotID: shotID, ProjectID: ref.ProjectID, Scene: ref.Scene.OrderIndex + 1, Number: ref.Shot.OrderIndex + 1,
		Busy: !ref.Shot.State.CanEditSpec(), MaxSeconds: s.limit(), Pictures: len(ref.Shot.References()),
	}
	if v.Choices, err = s.repo.Choices(ctx, ref.ProjectID); err != nil {
		return View{}, err
	}
	if id := shotaudio.ID(ref.Shot.ShotSpec); id != "" {
		v.Tie, err = s.tie(ctx, ref.ProjectID, id)
		if err != nil {
			return View{}, err
		}
	}
	locks, err := s.d.Shots.ShotLocks(ctx, shotID)
	if err != nil {
		return View{}, err
	}
	v.Locked = locks.Locked(domain.ParamPrompt)
	return v, nil
}

// Set ties the Shot to the audio asset of its Project, or takes the audio off (assetID ""), and returns the Shot's audio. projectID, when given, must be the Shot's Project
// (a Shot of another Project is ErrNotFound). An asset that is not in the Project is ErrNotFound; one that is not an audio file is an *InvalidError (field asset_id); a Shot
// that is being generated or repaired is ErrBusy; a Shot whose Prompt is LOCKed is a *LockedError. A change is noted as a hand edit of the Shot Spec.
func (s *Service) Set(ctx context.Context, projectID, shotID, assetID string) (View, error) {
	if _, err := s.shot(ctx, projectID, shotID); err != nil {
		return View{}, err
	}
	previous, err := s.repo.Set(ctx, shotID, assetID)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return View{}, ErrNotFound
	case errors.Is(err, repository.ErrShotBusy):
		return View{}, ErrBusy
	case errors.Is(err, shotaudiorepo.ErrNotAudio):
		return View{}, &InvalidError{Field: "asset_id", Message: "音声ファイルを選んでください（画像や動画は音声に使えません）"}
	case errors.Is(err, repository.ErrLocked):
		var locked *repository.LockedParametersError
		if errors.As(err, &locked) {
			return View{}, &LockedError{Parameters: locked.Parameters}
		}
		return View{}, &LockedError{Parameters: []domain.LockParameter{domain.ParamPrompt}}
	case err != nil:
		return View{}, err
	}
	if previous != assetID && s.d.Record != nil {
		s.d.Record(ctx, domain.Intervention{ShotID: shotID, Kind: domain.InterventionEditShotSpec})
	}
	return s.Get(ctx, projectID, shotID)
}

// shot is the Shot of the Project; a Shot of another Project is not found.
func (s *Service) shot(ctx context.Context, projectID, shotID string) (repository.ShotRef, error) {
	ref, err := s.d.Shots.GetShot(ctx, shotID)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && projectID != "" && ref.ProjectID != projectID) {
		return repository.ShotRef{}, ErrNotFound
	}
	return ref, err
}

// tie describes the tied asset: its row, its length and why it cannot be used (the asset or its file is gone, it is no audio, the length cannot be read or is over the limit).
// A row that is gone is a Tie without an asset and with the Problem, never an error: the Shot screen must show a Shot whose audio was deleted, so that it can be changed.
func (s *Service) tie(ctx context.Context, projectID, assetID string) (*Tie, error) {
	asset, err := s.d.Assets.Get(ctx, projectID, assetID)
	if errors.Is(err, repository.ErrNotFound) {
		return &Tie{Asset: domain.Asset{ID: assetID, ProjectID: projectID}, Problem: "音声ファイルが見つかりません（削除されたか、別のプロジェクトのものです）。選び直すか、外してください"}, nil
	}
	if err != nil {
		return nil, err
	}
	t := &Tie{Asset: asset}
	if asset.Type != domain.AssetAudio {
		t.Problem = "音声ファイルではありません。選び直すか、外してください"
		return t, nil
	}
	data, err := s.read(asset)
	if err != nil {
		t.Problem = "音声ファイルを読めません。選び直すか、外してください"
		return t, nil
	}
	d, err := audiofile.Duration(data)
	if err != nil {
		t.Problem = "音声の長さを読めません。選び直すか、外してください"
		return t, nil
	}
	t.Seconds = d.Seconds()
	if limit := s.limit(); t.Seconds > float64(limit) {
		t.Problem = fmt.Sprintf("音声が長すぎます（%.1f 秒。上限は %d 秒）。短い音声に選び直してください", t.Seconds, limit)
	}
	return t, nil
}

func (s *Service) limit() int {
	if s.d.MaxSeconds != nil {
		return s.d.MaxSeconds()
	}
	return gentestcfg.DefaultMaxAudioSeconds
}

func (s *Service) read(a domain.Asset) ([]byte, error) {
	if s.d.AssetsDir == "" || !filepath.IsLocal(filepath.FromSlash(a.FilePath)) {
		return nil, errors.New("no readable path")
	}
	abs := filepath.Join(s.d.AssetsDir, filepath.FromSlash(a.FilePath))
	if info, err := os.Stat(abs); err != nil || !info.Mode().IsRegular() || info.Size() > audiofile.MaxBytes {
		return nil, errors.New("not a readable audio file")
	}
	return os.ReadFile(abs) // #nosec G304 -- the path is the asset row's, under the assets directory
}
