package modelmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Limits of a trial generation.
const (
	// MaxPromptRunes is the longest prompt (and negative prompt) of a trial, in characters.
	MaxPromptRunes   = 4000
	maxReferenceSize = 20 << 20
	gentestDirName   = "gentest"
	inputsDirName    = "_inputs"
	recordFile       = "record.json"
)

// Reference is the optional picture of a trial: the reference image of an image model, the first frame of a video model.
type Reference struct {
	Data     []byte
	MimeType string
}

// GentestInput is a trial generation. ModelID is the #831 identifier of a model of an endpoint
// ("comfyui:bundled-comfyui/sdxl-base"; the models of an external ComfyUI and of an OpenAI-compatible server
// work too). NegativePrompt, Resolution and DurationSeconds are for video models (empty and 0 take the first
// the model offers); AspectRatio is "W:H" (default 1:1 for an image, 16:9 for a video).
type GentestInput struct {
	ModelID         string
	Prompt          string
	NegativePrompt  string
	AspectRatio     string
	Resolution      string
	DurationSeconds float64
	Reference       *Reference
	// ReferenceID names a picture that StageReference stored before (the MCP upload of a trial's reference): the
	// alternative to Reference for a client that cannot hand over bytes in the same call. Not both.
	ReferenceID string
}

func (in GentestInput) hasReference() bool { return in.Reference != nil || in.ReferenceID != "" }

// GentestStatus is the state of a trial.
type GentestStatus string

// The states of a trial.
const (
	GentestRunning   GentestStatus = "running"
	GentestSucceeded GentestStatus = "succeeded"
	GentestFailed    GentestStatus = "failed"
)

// GentestRecord is a trial as it is stored in DataDir/gentest/<job id>/record.json. File is the result inside that
// directory; a trial is outside any Project: nothing of it is a Take, a Cost Ledger row or an asset.
type GentestRecord struct {
	ID              string         `json:"id"` // the job id
	ModelID         string         `json:"model_id"`
	Media           localcfg.Media `json:"media"`
	Prompt          string         `json:"prompt"`
	NegativePrompt  string         `json:"negative_prompt,omitempty"`
	AspectRatio     string         `json:"aspect_ratio"`
	Resolution      string         `json:"resolution,omitempty"`
	DurationSeconds float64        `json:"duration_seconds,omitempty"`
	HasReference    bool           `json:"has_reference,omitempty"`
	Status          GentestStatus  `json:"status"`
	Error           string         `json:"error,omitempty"`
	File            string         `json:"file,omitempty"`
	MimeType        string         `json:"mime_type,omitempty"`
	SizeBytes       int64          `json:"size_bytes,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	FinishedAt      *time.Time     `json:"finished_at,omitempty"`
}

// gentestPayload is the payload of a gentest job: the request without the picture, which waits in the inputs
// directory until the job starts and then moves into the trial's directory.
type gentestPayload struct {
	ModelID         string  `json:"model_id"`
	Prompt          string  `json:"prompt"`
	NegativePrompt  string  `json:"negative_prompt,omitempty"`
	AspectRatio     string  `json:"aspect_ratio"`
	Resolution      string  `json:"resolution,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	Input           string  `json:"input,omitempty"` // file name in the inputs directory
	InputMime       string  `json:"input_mime,omitempty"`
}

var aspectPattern = regexp.MustCompile(`^[1-9][0-9]?:[1-9][0-9]?$`)

func (s *Service) gentestDir() string { return filepath.Join(s.DataDir, gentestDirName) }

// StartGentest checks the request against the model and starts the trial as a job (JobStatus; not resumable:
// a trial that was running when mam stopped is failed, and the user runs it again). The engine is started when
// the model is on the bundled endpoint. The result is stored in DataDir/gentest/<job id>/ (Gentests).
func (s *Service) StartGentest(ctx context.Context, in GentestInput) (JobRef, error) {
	model, err := s.gentestModel(in.ModelID)
	if err != nil {
		return JobRef{}, err
	}
	p, err := s.checkGentest(in, model)
	if err != nil {
		return JobRef{}, err
	}
	staged := false
	switch {
	case in.Reference != nil:
		name, mime, err := s.stageInput(*in.Reference)
		if err != nil {
			return JobRef{}, err
		}
		p.Input, p.InputMime, staged = name, mime, true
	case in.ReferenceID != "":
		mime, err := s.stagedMime(in.ReferenceID)
		if err != nil {
			return JobRef{}, err
		}
		p.Input, p.InputMime = in.ReferenceID, mime
	}
	job, err := s.Jobs.Enqueue(ctx, domain.JobGentest, p)
	if err != nil {
		if staged {
			_ = os.Remove(filepath.Join(s.gentestDir(), inputsDirName, p.Input))
		}
		return JobRef{}, err
	}
	return JobRef{JobID: job.ID}, nil
}

// gentestModel finds the model among the models of the running setting.
func (s *Service) gentestModel(id string) (localcfg.Model, error) {
	_, endpoint, name, ok := localcfg.ParseID(id)
	if !ok {
		return localcfg.Model{}, invalid("モデルの識別子が正しくありません: %q（comfyui:<接続先>/<名前> の形）", id)
	}
	m, ok := s.Adapter.Config().LocalModels[localcfg.Key(endpoint, name)]
	if !ok {
		return localcfg.Model{}, ErrNotFound
	}
	return m, nil
}

// checkGentest validates the request for the model and fills the defaults.
func (s *Service) checkGentest(in GentestInput, m localcfg.Model) (gentestPayload, error) {
	kind, _, _, _ := localcfg.ParseID(in.ModelID)
	p := gentestPayload{ModelID: in.ModelID, Prompt: strings.TrimSpace(in.Prompt), NegativePrompt: strings.TrimSpace(in.NegativePrompt),
		AspectRatio: in.AspectRatio, Resolution: in.Resolution, DurationSeconds: in.DurationSeconds}
	if in.Reference != nil && in.ReferenceID != "" {
		return p, invalid("参照画像は、バイト列か ID のどちらか一方で指定してください")
	}
	if p.Prompt == "" || utf8.RuneCountInString(p.Prompt) > MaxPromptRunes {
		return p, invalid("プロンプトは 1〜%d 文字で入力してください", MaxPromptRunes)
	}
	if utf8.RuneCountInString(p.NegativePrompt) > MaxPromptRunes {
		return p, invalid("ネガティブプロンプトは %d 文字以内にしてください", MaxPromptRunes)
	}
	if m.Media == localcfg.MediaImage {
		if p.NegativePrompt != "" || p.Resolution != "" || p.DurationSeconds != 0 {
			return p, invalid("ネガティブプロンプト・解像度・長さは動画モデルでだけ指定できます")
		}
		if p.AspectRatio == "" {
			p.AspectRatio = "1:1"
		}
		if in.hasReference() && !m.ImageInputs(kind) {
			return p, invalid("このモデルは参照画像を使えません")
		}
	} else {
		caps, ok := m.VideoCaps()
		if !ok {
			return p, invalid("このモデルの動画の設定が不完全です")
		}
		if p.AspectRatio == "" {
			p.AspectRatio = "16:9"
		}
		if p.Resolution == "" {
			p.Resolution = caps.Resolutions[0]
		}
		if p.DurationSeconds == 0 {
			p.DurationSeconds = caps.Durations[0]
		}
		if !slices.Contains(caps.Resolutions, p.Resolution) {
			return p, invalid("解像度 %s は使えません（%s）", p.Resolution, strings.Join(caps.Resolutions, "・"))
		}
		if !slices.Contains(caps.Durations, p.DurationSeconds) {
			return p, invalid("長さ %g 秒は使えません（%v 秒）", p.DurationSeconds, caps.Durations)
		}
		if in.hasReference() && !caps.FirstFrame && caps.MaxReferenceImages == 0 {
			return p, invalid("このモデルは画像からの動画に対応していません")
		}
	}
	if !aspectPattern.MatchString(p.AspectRatio) {
		return p, invalid("縦横比は 16:9 のように指定してください")
	}
	return p, nil
}

// cleanInputs removes pictures of trials that never started (older than a day: a queued trial still needs its own).
func (s *Service) cleanInputs() {
	dir := filepath.Join(s.gentestDir(), inputsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && s.Now().Sub(info.ModTime()) > 24*time.Hour {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

var idPattern = regexp.MustCompile(`^[0-9A-Za-z-]{8,64}$`)

// Gentests lists the trials that started, newest first. A trial whose job ended without writing its result (mam was
// stopped while it ran) is shown as failed.
func (s *Service) Gentests(ctx context.Context) ([]GentestRecord, error) {
	entries, err := os.ReadDir(s.gentestDir())
	if errors.Is(err, os.ErrNotExist) {
		return []GentestRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []GentestRecord{}
	for _, e := range entries {
		if !e.IsDir() || !idPattern.MatchString(e.Name()) {
			continue
		}
		if rec, err := s.Gentest(ctx, e.Name()); err == nil {
			out = append(out, rec)
		}
	}
	slices.SortFunc(out, func(a, b GentestRecord) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}

// Gentest is one trial (ErrNotFound when it has no directory).
func (s *Service) Gentest(ctx context.Context, id string) (GentestRecord, error) {
	if !idPattern.MatchString(id) {
		return GentestRecord{}, ErrNotFound
	}
	data, err := os.ReadFile(filepath.Join(s.gentestDir(), id, recordFile)) //nolint:gosec // id matches idPattern above
	if errors.Is(err, os.ErrNotExist) {
		return GentestRecord{}, ErrNotFound
	}
	if err != nil {
		return GentestRecord{}, err
	}
	var rec GentestRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return GentestRecord{}, fmt.Errorf("read the trial %s: %w", id, err)
	}
	if rec.Status == GentestRunning && s.JobTable != nil {
		if job, err := s.JobTable.Get(ctx, id); err == nil && (job.Status == domain.JobFailed || job.Status == domain.JobSucceeded) {
			rec.Status, rec.Error = GentestFailed, orDefault(job.Error, "中断されました")
		} else if errors.Is(err, repository.ErrNotFound) {
			rec.Status, rec.Error = GentestFailed, "中断されました"
		}
	}
	return rec, nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// GentestFile is the path and the type of the result file of a succeeded trial, for the HTTP handler that hands it out.
func (s *Service) GentestFile(ctx context.Context, id string) (path, mime string, err error) {
	rec, err := s.Gentest(ctx, id)
	if err != nil {
		return "", "", err
	}
	if rec.Status != GentestSucceeded || rec.File == "" || rec.File != filepath.Base(rec.File) {
		return "", "", ErrNotFound
	}
	return filepath.Join(s.gentestDir(), id, rec.File), rec.MimeType, nil
}

// DeleteGentest removes a trial and its files. A trial that is running is ErrBusy.
func (s *Service) DeleteGentest(ctx context.Context, id string) error {
	rec, err := s.Gentest(ctx, id)
	if err != nil {
		return err
	}
	if rec.Status == GentestRunning {
		return ErrBusy
	}
	return os.RemoveAll(filepath.Join(s.gentestDir(), id))
}
