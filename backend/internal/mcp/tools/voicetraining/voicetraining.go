// Package voicetraining holds the MCP tools of the fine-tuning of a Character's voice (Issue #936, spec/architecture/model-manager/local-runtime.md
// §16): the clips of the speaker (uploaded with create_upload_ticket, purpose character_voice_training_clip), the summary of the data, the start,
// the cancel and the removal of the trained voice. A training is a job (get_job: the epoch, the time that has gone and the time that is left).
//
// No tool takes a path, a command, a script or a weight file: the Character names the data, the service lays out the files, and the runtime
// starts one fixed script. The consent of the owner of the voice is a notice in the descriptions, which the agent passes on to the person: it is
// not a switch a tool could turn.
package voicetraining

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/service/voicetrain"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// Names of the job kind and the upload purpose of this package.
const (
	KindTrain     = "train_character_voice"
	PurposeUpload = "character_voice_training_clip"
)

// Consent is the notice every tool that makes a voice carries: it is what the person must be told before a training starts.
const Consent = "声のコピーには声の持ち主本人の同意が要る。同意なしに実在の人（声優・有名人など）の声を学習したり、なりすましや人をだます合成音声に使ったりしてはならない。エージェントが本人の同意を代わりに確認したことにしてはならない"

// Register adds the tools, the job kind and the upload purpose of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	t := svc.VoiceTrain
	h := handlers{t: t, r: r}
	r.Jobs.RegisterKind(KindTrain, h.probe, mcpkit.InMemory())
	r.Files.RegisterUpload(uploadSpec(t))

	mcpkit.Read(r, "get_character_voice_training",
		"登場人物の声の学習（fine-tune）の状態: 学習に使う音声（clips）と合計の長さ、始められるか（problems に理由）、エポック数の既定と上限、学習済みの声（trained）、いまの学習（current）。series_id か project_id のどちらか一方と character_id を渡す。見込み時間は学習が始まる前には出せない（最初のエポックが終わると get_job の progress に出る）/ Data, limits and state of the voice training of a Character.",
		h.get)
	mcpkit.Write(r, "start_character_voice_training",
		"登場人物の声を GPT-SoVITS で学習（fine-tune）するジョブを始める。job_id を返すので get_job で進み具合（エポック n/m・経過・残り時間）を見る。先に参照音声と、学習用の音声（purpose=character_voice_training_clip で 2〜30 秒の録音と書き起こしを合計 1〜30 分）が要る。同梱の ComfyUI が生成中なら始められない（学習は ComfyUI と同時に動かさない）。数十分かかる。mam を再起動すると学習は途中から再開せず、やり直しになる。学習した声は、その登場人物（Series の正本なら以後に作る Episode）の声の合成に使われる。"+Consent+" / Start the fine-tuning of a Character's voice (a job: poll with get_job).",
		h.start, mcpkit.OpenWorld())
	mcpkit.Write(r, "cancel_character_voice_training",
		"動いている声の学習を中止する。子プロセスを止め、途中までの結果は残さない。動いていなければ conflict / Cancel the running voice training; nothing of it is kept.",
		h.cancel, mcpkit.Idempotent())
	mcpkit.Destroy(r, "delete_character_voice_training_clips",
		"学習用の音声を消す。clip_id を渡すとその 1 つ、渡さなければ全部（学習済みの声は残る）/ Remove one training clip, or all of them when clip_id is left out.",
		h.deleteClips, mcpkit.Idempotent())
	mcpkit.Destroy(r, "delete_character_trained_voice",
		"学習済みの声を取り除き、参照音声のゼロショットの声に戻す（重みのファイルも消える。参照音声は残る）。学習していなければ何もしない / Remove the trained voice of a Character: it speaks with the zero-shot clone of its reference voice again.",
		h.deleteTrained, mcpkit.Idempotent())
}

type handlers struct {
	t *voicetrain.Service
	r *mcpkit.Registrar
}

// Target names the Character: of a Series canon (series_id) or of a Project (project_id), exactly one.
type Target struct {
	SeriesID    string `json:"series_id,omitempty" jsonschema:"Series の正本の登場人物のとき（get_series の canon.characters の id と組）"`
	ProjectID   string `json:"project_id,omitempty" jsonschema:"Project の登場人物のとき（get_world_state の characters の id と組）"`
	CharacterID string `json:"character_id"`
}

func (t Target) owner() (voicerefsvc.Owner, error) {
	if (t.SeriesID == "") == (t.ProjectID == "") {
		return voicerefsvc.Owner{}, &service.ValidationError{Field: "series_id", Message: "series_id か project_id のどちらか一方を指定してください"}
	}
	if strings.TrimSpace(t.CharacterID) == "" {
		return voicerefsvc.Owner{}, &service.ValidationError{Field: "character_id", Message: "character_id を指定してください"}
	}
	if t.SeriesID != "" {
		return voicerefsvc.ForSeries(t.SeriesID), nil
	}
	return voicerefsvc.ForProject(t.ProjectID), nil
}

// serviceError turns a refusal of the voice services into the error of package service the tools report. The apperr errors (the refusals of a
// training) pass as they are: the tools tell them as the common error.
func serviceError(err error) error {
	var invalid *voicerefsvc.InvalidError
	switch {
	case errors.As(err, &invalid):
		return &service.ValidationError{Field: invalid.Field, Message: invalid.Message}
	case errors.Is(err, voicerefsvc.ErrNotFound):
		return service.ErrNotFound
	case errors.Is(err, voicetrain.ErrNoRun):
		return &service.ConflictError{Message: "この登場人物の学習は動いていません"}
	}
	return err
}

// Clip is one training clip as the tools tell it (the stored file is not named).
type Clip struct {
	ID         string  `json:"id" jsonschema:"clip_id（delete_character_voice_training_clips に渡す）"`
	Name       string  `json:"name" jsonschema:"送ったファイル名"`
	Text       string  `json:"text" jsonschema:"書き起こし"`
	DurationMS int     `json:"duration_ms"`
	Seconds    float64 `json:"seconds"`
}

// Trained is the trained voice of a Character.
type Trained struct {
	Version      string    `json:"version" jsonschema:"学習した GPT-SoVITS のモデルの版（v2）"`
	GPTEpochs    int       `json:"gpt_epochs"`
	SoVITSEpochs int       `json:"sovits_epochs"`
	Clips        int       `json:"clips"`
	DataSeconds  int       `json:"data_seconds"`
	TrainedAt    time.Time `json:"trained_at"`
}

func trained(f *voiceref.Finetune) *Trained {
	if f == nil {
		return nil
	}
	return &Trained{Version: f.Version, GPTEpochs: f.GPTEpochs, SoVITSEpochs: f.SoVITSEpochs, Clips: f.Clips, DataSeconds: f.DataSeconds, TrainedAt: f.TrainedAt}
}

// Epochs are the settings of a training.
type Epochs struct {
	SoVITS int `json:"sovits"`
	GPT    int `json:"gpt"`
}

// Current is a training of this process: the one that runs, or the latest that ended.
type Current struct {
	State    string     `json:"state" jsonschema:"running / succeeded / failed / cancelled"`
	Text     string     `json:"text,omitempty" jsonschema:"いま何をしているか（段階・エポック n/m・経過・残り時間）"`
	Percent  int        `json:"percent"`
	Error    string     `json:"error,omitempty"`
	Epochs   Epochs     `json:"epochs"`
	Started  time.Time  `json:"started_at"`
	Finished *time.Time `json:"finished_at,omitempty"`
}

// View is the voice training of a Character.
type View struct {
	CharacterID  string         `json:"character_id"`
	HasReference bool           `json:"has_reference_voice" jsonschema:"参照音声がある（学習の前提）"`
	Clips        []Clip         `json:"clips"`
	DataSeconds  float64        `json:"data_seconds" jsonschema:"学習用の音声の合計の秒数"`
	Problems     []string       `json:"problems,omitempty" jsonschema:"いまは学習を始められない理由（日本語）。空なら音声の量は足りている"`
	Limits       map[string]int `json:"limits" jsonschema:"min_clip_seconds・max_clip_seconds・min_total_seconds・max_total_seconds・max_clips"`
	Defaults     Epochs         `json:"default_epochs"`
	MaxEpochs    Epochs         `json:"max_epochs"`
	Trained      *Trained       `json:"trained,omitempty" jsonschema:"学習済みの声。あれば声の合成はその重みを使う"`
	Current      *Current       `json:"current,omitempty"`
	GPUBusy      bool           `json:"gpu_busy" jsonschema:"別の登場人物の学習が GPU を使っている"`
	Consent      string         `json:"consent"`
}

func (h handlers) view(ctx context.Context, o voicerefsvc.Owner, characterID string) (View, error) {
	s, err := h.t.Summarize(ctx, o, characterID)
	if err != nil {
		return View{}, serviceError(err)
	}
	v := View{
		CharacterID: characterID, HasReference: s.HasReference, Clips: make([]Clip, 0, len(s.Clips)), DataSeconds: s.DataSeconds, Problems: s.Problems,
		Limits: map[string]int{
			"min_clip_seconds": s.Limits.MinClipSeconds, "max_clip_seconds": s.Limits.MaxClipSeconds, "min_total_seconds": s.Limits.MinTotalSeconds,
			"max_total_seconds": s.Limits.MaxTotalSeconds, "max_clips": s.Limits.MaxClips,
		},
		Defaults: Epochs{SoVITS: s.Defaults.EpochsSoVITS, GPT: s.Defaults.EpochsGPT}, MaxEpochs: Epochs{SoVITS: s.MaxEpochs.EpochsSoVITS, GPT: s.MaxEpochs.EpochsGPT},
		Trained: trained(s.Trained), GPUBusy: s.GPUBusy, Consent: Consent,
	}
	for _, c := range s.Clips {
		v.Clips = append(v.Clips, Clip{ID: c.ID, Name: c.Name, Text: c.Text, DurationMS: c.DurationMS, Seconds: c.Duration().Seconds()})
	}
	if s.Current != nil {
		v.Current = &Current{
			State: string(s.Current.State), Text: s.Current.Text, Percent: s.Current.Progress.Percent, Error: s.Current.Error,
			Epochs: Epochs{SoVITS: s.Current.Epochs.EpochsSoVITS, GPT: s.Current.Epochs.EpochsGPT}, Started: s.Current.StartedAt, Finished: s.Current.FinishedAt,
		}
	}
	return v, nil
}

func (h handlers) get(ctx context.Context, in Target) (View, error) {
	o, err := in.owner()
	if err != nil {
		return View{}, err
	}
	return h.view(ctx, o, in.CharacterID)
}

// StartInput is the Character and the epochs (0 / left out: the default).
type StartInput struct {
	Target
	EpochsSoVITS int `json:"epochs_sovits,omitempty" jsonschema:"SoVITS の学習のエポック数（既定 8、1〜25）"`
	EpochsGPT    int `json:"epochs_gpt,omitempty" jsonschema:"GPT の学習のエポック数（既定 15、1〜50）"`
}

// StartOutput is the job.
type StartOutput struct {
	mcpkit.JobRef
	Character string `json:"character" jsonschema:"学習する登場人物の名前"`
}

func (h handlers) start(ctx context.Context, in StartInput) (StartOutput, error) {
	o, err := in.owner()
	if err != nil {
		return StartOutput{}, err
	}
	var st voicetrain.Status
	ref, err := h.r.Jobs.RunResolved(ctx, KindTrain, func(ctx context.Context) (string, string, error) {
		var err error
		st, err = h.t.Start(ctx, o, in.CharacterID, voicetrain.Options{EpochsSoVITS: in.EpochsSoVITS, EpochsGPT: in.EpochsGPT})
		return st.ID, "", serviceError(err)
	})
	if err != nil {
		return StartOutput{}, err
	}
	return StartOutput{JobRef: ref, Character: st.Character}, nil
}

// probe reads the training from the service: the job's target is the id of the training. A training of an earlier run of mam is not known; the
// job history answers for it.
func (h handlers) probe(_ context.Context, job mcpkit.JobInfo) (mcpkit.Progress, error) {
	st, ok := h.t.Status(job.Target)
	if !ok {
		return mcpkit.Progress{State: mcpkit.JobFailed, Error: "この学習の記録が見つかりません（mam を再起動した可能性があります。学習は再開しません。get_character_voice_training で状態を確かめ、必要ならもう一度始めてください）"}, nil
	}
	p := mcpkit.Progress{Message: st.Text, Done: st.Progress.Percent, Total: 100}
	if st.Running() {
		p.State, p.Steps = mcpkit.JobRunning, st.Progress.View()
		return p, nil
	}
	switch st.State {
	case worktrack.Succeeded:
		p.State, p.Done, p.Message = mcpkit.JobSucceeded, 100, "学習が終わりました"
		p.Result = map[string]any{"trained": trained(st.Trained)}
	case worktrack.Cancelled:
		p.State, p.Message = mcpkit.JobCancelled, "学習を中止しました（途中までの結果は残していません）"
	default:
		p.State, p.Error = mcpkit.JobFailed, st.Error
		if st.Failure != nil {
			v := st.Failure.View(apperr.LangJa)
			p.Failure = &v
		}
	}
	return p, nil
}

// CancelOutput is the answer of a cancel.
type CancelOutput struct {
	Cancelled bool `json:"cancelled"`
}

func (h handlers) cancel(_ context.Context, in Target) (CancelOutput, error) {
	o, err := in.owner()
	if err != nil {
		return CancelOutput{}, err
	}
	if err := h.t.Cancel(o, in.CharacterID); err != nil {
		return CancelOutput{}, serviceError(err)
	}
	return CancelOutput{Cancelled: true}, nil
}

// ClipInput is a Character and, optionally, one clip.
type ClipInput struct {
	Target
	ClipID string `json:"clip_id,omitempty" jsonschema:"消す音声の id（get_character_voice_training の clips）。省略すると全部"`
}

func (h handlers) deleteClips(ctx context.Context, in ClipInput) (View, error) {
	o, err := in.owner()
	if err != nil {
		return View{}, err
	}
	if err := h.t.RemoveClips(ctx, o, in.CharacterID, in.ClipID); err != nil {
		return View{}, serviceError(err)
	}
	return h.view(ctx, o, in.CharacterID)
}

func (h handlers) deleteTrained(ctx context.Context, in Target) (View, error) {
	o, err := in.owner()
	if err != nil {
		return View{}, err
	}
	if err := h.t.RemoveTrained(ctx, o, in.CharacterID); err != nil {
		return View{}, serviceError(err)
	}
	return h.view(ctx, o, in.CharacterID)
}

func uploadSpec(t *voicetrain.Service) mcpkit.UploadSpec {
	return mcpkit.UploadSpec{
		Purpose: PurposeUpload,
		Description: fmt.Sprintf("登場人物の声の学習用の音声を 1 つ足す（%s、%d MB まで、%d〜%d 秒、書き起こし必須）。学習には合計 %d 秒〜%d 分のクリップが要る（%d 個まで）。series_id か project_id のどちらか一方と character_id と text を渡す。"+Consent+" / Add one training clip of a Character's voice.",
			strings.Join(voiceref.Extensions(), " "), voicetrain.MaxClipBytes>>20, voicetrain.MinClipSeconds, voicetrain.MaxClipSeconds, voicetrain.MinTotalSeconds, voicetrain.MaxTotalSeconds/60, voicetrain.MaxClips),
		MaxBytes: voicetrain.MaxClipBytes,
		Params: []mcpkit.UploadParam{
			{Name: "series_id", Description: "Series の正本の登場人物のとき（project_id とどちらか一方）"},
			{Name: "project_id", Description: "Project の登場人物のとき（series_id とどちらか一方）"},
			{Name: "character_id", Description: "get_series の canon.characters / get_world_state の characters の id", Required: true},
			{Name: "text", Description: "録音の書き起こし（500 文字以内）", Required: true},
		},
		Handle: func(ctx context.Context, in mcpkit.UploadInput) (any, error) {
			p := in.Params
			o, err := Target{SeriesID: p["series_id"], ProjectID: p["project_id"], CharacterID: p["character_id"]}.owner()
			if err != nil {
				return nil, err
			}
			c, err := t.AddClip(ctx, o, p["character_id"], voicerefsvc.File(in.Upload()), p["text"])
			if err != nil {
				return nil, serviceError(err)
			}
			return Clip{ID: c.ID, Name: c.Name, Text: c.Text, DurationMS: c.DurationMS, Seconds: c.Duration().Seconds()}, nil
		},
	}
}
