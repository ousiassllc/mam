// Package voicerefsvc keeps the reference voice of a Character (F-20, F-29): the recording a local voice
// provider clones so that the same Character has the same voice in every Episode. The Series canon Character owns
// it and the Episodes copy it (repository, spec/architecture/er.md §4); a Project's own Characters have one too.
// Like localmodels it does not import package service (which holds it): its refusals are ErrNotFound and
// *InvalidError, and the screen and the MCP tools turn them into their own.
package voicerefsvc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/bundlepath"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/voicerefrepo"
)

// ErrNotFound means the Project, Series or Character does not exist (or has no reference voice to play).
var ErrNotFound = errors.New("voicerefsvc: not found")

// InvalidError is input that cannot be accepted. Field names the form field; Message is shown to the user.
type InvalidError struct {
	Field   string
	Message string
}

func (e *InvalidError) Error() string { return "invalid input: " + e.Field + ": " + e.Message }

func invalid(field, format string, args ...any) error {
	return &InvalidError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// File is the recording the user sent. Open is called at most once. It has the fields of service.Upload.
type File struct {
	Name string
	Size int64
	Open func() (io.ReadCloser, error)
}

// Prober measures the length of an audio file in seconds (media.Tools).
type Prober interface {
	AudioDuration(ctx context.Context, path string) (float64, error)
}

// Deps are what the Service needs. Probe may be nil (ffprobe is not installed): the length is then not checked.
type Deps struct {
	DB        *repository.DB
	Series    *repository.SeriesRepository
	Projects  *repository.ProjectRepository
	World     *repository.WorldRepository
	AssetsDir string
	// Media is ffprobe; nil: the length of a recording is not checked. Probe, when set, is used instead (tests).
	Media  *media.Tools
	Probe  Prober
	Logger *slog.Logger
}

// Service sets, removes, plays and looks up reference voices.
type Service struct {
	repo      *voicerefrepo.Repository
	series    *repository.SeriesRepository
	projects  *repository.ProjectRepository
	world     *repository.WorldRepository
	assetsDir string
	probe     Prober
	logger    *slog.Logger
}

// New returns the Service.
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Probe == nil && d.Media != nil { // a typed nil *media.Tools in the interface would not be nil
		d.Probe = d.Media
	}
	return &Service{
		repo: voicerefrepo.New(d.DB, d.Series), series: d.Series, projects: d.Projects, world: d.World,
		assetsDir: d.AssetsDir, probe: d.Probe, logger: d.Logger,
	}
}

// Input is what is set on a Character. File may be left out to change the transcript or the language of the
// reference voice it already has. Text and Language nil keep what the Character has (a new recording starts with
// no transcript and the default language); "" for Language is the default language.
//
// TextPrefilled is for the screen, whose transcript field is pre-filled with the stored transcript and so always
// sends it: with a new File, a Text equal to the stored transcript is then the untouched field, not a transcript
// for the new recording, and counts as not sent.
type Input struct {
	File          File
	Text          *string
	Language      *string
	TextPrefilled bool
}

// owner is a Project or a Series: whose Character it is and the directory its recordings are stored in.
type owner struct {
	id     string
	series bool
}

func (s *Service) exists(ctx context.Context, o owner) error {
	var err error
	if o.series {
		_, err = s.series.Get(ctx, o.id)
	} else {
		_, err = s.projects.Get(ctx, o.id)
	}
	return notFound(err)
}

func (s *Service) current(ctx context.Context, o owner, characterID string) (*voiceref.Reference, error) {
	var ref *voiceref.Reference
	var err error
	if o.series {
		ref, err = s.repo.Series(ctx, o.id, characterID)
	} else {
		ref, err = s.repo.Project(ctx, o.id, characterID)
	}
	return ref, notFound(err)
}

func (s *Service) write(ctx context.Context, o owner, characterID string, ref *voiceref.Reference) (*voiceref.Reference, error) {
	var previous *voiceref.Reference
	var err error
	if o.series {
		previous, err = s.repo.SetSeries(ctx, o.id, characterID, ref)
	} else {
		previous, err = s.repo.SetProject(ctx, o.id, characterID, ref)
	}
	return previous, notFound(err)
}

func notFound(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// SetForSeries sets the reference voice of a canonical Character of the Series (a new version of the canon; the
// Episodes that exist keep what they have). SetForProject does the same for a Character row of the Project.
func (s *Service) SetForSeries(ctx context.Context, seriesID, characterID string, in Input) (voiceref.Reference, error) {
	return s.set(ctx, owner{id: seriesID, series: true}, characterID, in)
}

// SetForProject sets the reference voice of a Character of the Project: an Episode may replace the voice it
// inherited, for that Episode only.
func (s *Service) SetForProject(ctx context.Context, projectID, characterID string, in Input) (voiceref.Reference, error) {
	return s.set(ctx, owner{id: projectID}, characterID, in)
}

func (s *Service) set(ctx context.Context, o owner, characterID string, in Input) (voiceref.Reference, error) {
	if err := s.exists(ctx, o); err != nil {
		return voiceref.Reference{}, err
	}
	if !bundlepath.ValidID(o.id) {
		return voiceref.Reference{}, ErrNotFound
	}
	have, err := s.current(ctx, o, characterID)
	if err != nil {
		return voiceref.Reference{}, err
	}
	text, lang, err := metadata(in, have)
	if err != nil {
		return voiceref.Reference{}, err
	}
	var ref voiceref.Reference
	switch {
	case in.File.Open != nil:
		if ref, err = s.store(ctx, o.id, in.File); err != nil {
			return voiceref.Reference{}, err
		}
	case have != nil:
		ref = *have
	default:
		return voiceref.Reference{}, invalid("file", "参照音声のファイルを選んでください（%s）", strings.Join(voiceref.Extensions(), " "))
	}
	ref.Text, ref.Language = text, lang
	previous, err := s.write(ctx, o, characterID, &ref)
	if err != nil {
		s.discard(ctx, have, &ref)
		return voiceref.Reference{}, err
	}
	s.discard(ctx, previous, &ref)
	return ref, nil
}

// metadata is the transcript and the language the Character gets: the ones sent, else the ones it has, else none
// and the default language. A new recording drops the transcript of the old one.
func metadata(in Input, have *voiceref.Reference) (text, lang string, err error) {
	text, lang = "", voiceref.DefaultLanguage
	if have != nil {
		lang = have.Language
		if in.File.Open == nil {
			text = have.Text
		}
	}
	if in.Text != nil && !untouched(in, have) {
		text = *in.Text
	}
	if in.Language != nil {
		lang = *in.Language
	}
	if text, err = voiceref.NormalizeText(text); err != nil {
		return "", "", invalid("text", "書き起こしは %d 文字以内で入力してください", voiceref.MaxTextRunes)
	}
	if lang, err = voiceref.NormalizeLanguage(lang); err != nil {
		return "", "", invalid("language", "言語は %s のいずれかを指定してください", strings.Join(voiceref.Languages(), " / "))
	}
	return text, lang, nil
}

// untouched reports that the transcript sent with a new recording is the stored one sent back by a pre-filled field.
func untouched(in Input, have *voiceref.Reference) bool {
	return in.TextPrefilled && in.File.Open != nil && have != nil && strings.TrimSpace(*in.Text) == have.Text
}
