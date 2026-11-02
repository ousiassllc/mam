// Package joblist reads the jobs of mam for the job list screen (spec/components/jobs-screen.md): the ones that run now and the
// latest that ended, what each works on and how far it is. It only reads. The jobs themselves are kept by the job table (the
// background queue: model installs, generation test batches, Shot generation, Judge, Repair and the planning of a Project), and what a
// job works on and its progress are asked of the service that owns that kind of job; this package changes none of them. Work that does
// not go through the job table (the export of a video, the audio mix, the editor's Judge) has no row here.
package joblist

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/logging"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// Newest is how many of the newest jobs the list reads (the jobs that have not finished are listed on top of that, however old).
const Newest = 50

// State is where a job stands.
type State string

// The states of a job. A generation test batch can also be cancelled.
const (
	Queued    State = "queued"
	Running   State = "running"
	Succeeded State = "succeeded"
	Failed    State = "failed"
	Cancelled State = "cancelled"
)

// Screen is the screen a job leads back to (the web layer turns it into a path).
type Screen string

// The screens a job leads to. ProjectScenes and ProjectChat carry the Project (Dest.ProjectID).
const (
	ScreenNone           Screen = ""
	ScreenModelManager   Screen = "model-manager"
	ScreenGenerationTest Screen = "generation-test"
	ScreenProjectScenes  Screen = "project-scenes"
	ScreenProjectChat    Screen = "project-chat"
)

// Dest is where the job is worked on: the screen, and the Project when it is a screen of a Project.
type Dest struct {
	Screen    Screen
	ProjectID string
}

// Progress is how far a running job is. Percent is 0-100; Text says it in words ("2 / 4 件", the file that is being downloaded).
type Progress struct {
	Percent int
	Text    string
}

// Entry is one job of the list.
type Entry struct {
	ID string
	// Kind is the kind of the job table (domain.JobKind) and Label what the screen calls it.
	Kind  domain.JobKind
	Label string
	State State
	// StartedAt is when the job started; of a job that has not started yet, when it was queued.
	StartedAt  time.Time
	FinishedAt *time.Time
	// Subject is what the job works on: the model (or the models) of an install or a trial, the Project of a Shot. "" when it
	// cannot be told (the Project was deleted, the model is hidden).
	Subject string
	// Progress is nil when nothing says how far the job is.
	Progress *Progress
	// Error is why a job that failed failed, with the secrets hidden.
	Error string
	Dest  Dest
}

// Table is the persisted job queue (jobrecent.Repository).
type Table interface {
	ListRecent(ctx context.Context, newest int) ([]domain.Job, error)
}

// Shots finds the Project of a Shot (repository.GenerationRepository).
type Shots interface {
	GetShot(ctx context.Context, shotID string) (repository.ShotRef, error)
}

// Deps are what the list reads. ModelManager and Gentest may be nil (a build without them): the jobs of their kind then have no subject
// and no progress.
type Deps struct {
	Table        Table
	Shots        Shots
	Projects     *service.ProjectService
	ModelManager *modelmanager.Service
	Gentest      *gentest.Service
}

// Service lists the jobs.
type Service struct{ Deps }

// New returns the list over the job table and the services that know what a job works on.
func New(d Deps) *Service { return &Service{d} }

// Recent lists the jobs that have not finished and the latest that have, the newest start first.
func (s *Service) Recent(ctx context.Context) ([]Entry, error) {
	rows, err := s.Table.ListRecent(ctx, Newest)
	if err != nil {
		return nil, err
	}
	r := &reader{Service: s, projects: map[string]*string{}}
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		e, err := r.entry(ctx, row)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// payload is what the payloads of the kinds name (a Shot, a Project, a batch, an install target).
type payload struct {
	ShotID    string `json:"shot_id"`
	ProjectID string `json:"project_id"`
	BatchID   string `json:"batch_id"`
	Target    string `json:"target"`
}

func (r *reader) entry(ctx context.Context, row domain.Job) (Entry, error) {
	var p payload
	_ = json.Unmarshal(row.Payload, &p)
	e := Entry{
		ID: row.ID, Kind: row.Kind, Label: label(row.Kind), State: State(row.Status), StartedAt: row.CreatedAt, FinishedAt: row.FinishedAt,
		Error: logging.Redact(row.Error),
	}
	if row.StartedAt != nil {
		e.StartedAt = *row.StartedAt
	}
	var err error
	switch row.Kind {
	case domain.JobModelInstall:
		err = r.install(ctx, &e, p)
	case domain.JobGentest:
		err = r.gentest(ctx, &e, p)
	case domain.JobPlan:
		err = r.project(ctx, &e, p.ProjectID, ScreenProjectChat)
	case domain.JobGenerate, domain.JobJudge, domain.JobRepair:
		err = r.shot(ctx, &e, p.ShotID)
	}
	return e, err
}

// label is what the screen calls a kind of job.
func label(k domain.JobKind) string {
	switch k {
	case domain.JobModelInstall:
		return "導入"
	case domain.JobGentest:
		return "生成テスト"
	case domain.JobGenerate:
		return "Shot の生成"
	case domain.JobJudge:
		return "Take の判定"
	case domain.JobRepair:
		return "Take の修正"
	case domain.JobPlan:
		return "企画・脚本の作成"
	}
	return string(k)
}

// missing reports that what a job names is gone (a deleted Project, a model that is no longer in the catalog): not a fault of the list.
func missing(err error) bool {
	return errors.Is(err, repository.ErrNotFound) || errors.Is(err, service.ErrNotFound)
}
