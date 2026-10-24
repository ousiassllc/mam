package editor

import (
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// ViewInput names the Project (and the Branch) whose editing state is read.
type ViewInput struct {
	ProjectID string `json:"project_id"`
	BranchID  string `json:"branch_id,omitempty" jsonschema:"対象の Branch の ID。省略すると本線（trunk）/ Branch to work on; omitted is the trunk"`
}

// ViewOutput is the editing state of a Project: what the editor panel of the screen shows. Every
// stage tool that answers directly returns it (what the screen redraws after the stage).
type ViewOutput struct {
	ProjectID    string        `json:"project_id"`
	ProjectName  string        `json:"project_name"`
	State        string        `json:"state" jsonschema:"Project の状態（EDITING, AUDIO, READY, EXPORTED ...）"`
	BranchID     string        `json:"branch_id,omitempty"`
	PendingShots int           `json:"pending_shots" jsonschema:"Editor → Reshoot Loop で追加 Shot がまだ生成中の数 / additional Shots still being made"`
	CanRender    bool          `json:"can_render" jsonschema:"false なら ffmpeg が無く、プレビュー・音声・書き出しは使えない"`
	Timeline     *TimelineHead `json:"timeline,omitempty" jsonschema:"最新の Edit Timeline（Rough Cut 前は無い）。クリップは get_edit_timeline"`
	Audio        *AudioOut     `json:"audio,omitempty" jsonschema:"表示中の Branch の最新 Audio Timeline"`
	TrunkAudio   *AudioOut     `json:"trunk_audio,omitempty" jsonschema:"Final QC が検査する本線の Audio Timeline（Branch を指定したときだけ）"`
	Voices       []VoiceOut    `json:"voices"`
	FinalQC      *QCOut        `json:"final_qc,omitempty" jsonschema:"最新の Final QC の結果"`
	Exports      []ExportOut   `json:"exports" jsonschema:"書き出し済みの動画（新しい順）"`
	// ExportDefaults are what export_video uses for a field left empty.
	ExportDefaults ExportOptions `json:"export_defaults"`
	ExportChoices  ExportChoices `json:"export_choices"`
}

// TimelineHead is an Edit Timeline version without its clips.
type TimelineHead struct {
	ID              string    `json:"id" jsonschema:"版の ID。save_edit_timeline・judge_edit_timeline・request_additional_shot に渡す"`
	BranchID        string    `json:"branch_id,omitempty"`
	VersionNumber   int       `json:"version_number"`
	ClipCount       int       `json:"clip_count"`
	DurationSeconds float64   `json:"duration_seconds"`
	Editable        bool      `json:"editable" jsonschema:"false なら Project が編集段階を過ぎていて読み取り専用"`
	Judge           *JudgeOut `json:"judge,omitempty" jsonschema:"この版の最新の Editor Judge の結果"`
	CreatedAt       time.Time `json:"created_at"`
}

// JudgeOut is an Editor Judge result.
type JudgeOut struct {
	ID        string       `json:"id"`
	Verdict   string       `json:"verdict" jsonschema:"pass, re_edit（編集で直せる）, need_shot（素材不足: request_additional_shot）"`
	Findings  []FindingOut `json:"findings"`
	CreatedAt time.Time    `json:"created_at"`
}

// FindingOut is one thing the Editor Judge found wrong.
type FindingOut struct {
	ClipIndex int          `json:"clip_index" jsonschema:"1 始まりのクリップ番号（0: 全体）"`
	Issue     string       `json:"issue"`
	Action    string       `json:"action"`
	NeedShot  *ShotNeedOut `json:"need_shot,omitempty" jsonschema:"素材不足のとき、Director に頼む追加 Shot"`
}

// ShotNeedOut is the additional Shot a finding asks the Director for.
type ShotNeedOut struct {
	Kind            string  `json:"kind" jsonschema:"insert or reaction"`
	AfterClip       int     `json:"after_clip"`
	Objective       string  `json:"objective"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// AudioOut is an Audio Timeline version in short.
type AudioOut struct {
	ID              string         `json:"id"`
	VersionNumber   int            `json:"version_number"`
	EditTimelineID  string         `json:"edit_timeline_id" jsonschema:"この音声を作った Edit Timeline の版"`
	Current         *bool          `json:"current,omitempty" jsonschema:"最新の編集に対して作られた音声か（Branch 指定時の trunk_audio では出ない）。false だと書き出しは conflict になる"`
	Mode            string         `json:"mode,omitempty"`
	SpeechLines     int            `json:"speech_lines"`
	Layers          map[string]int `json:"layers" jsonschema:"レイヤー（dialogue, narration, bgm, se, foley, ambience）ごとのクリップ数"`
	DurationSeconds float64        `json:"duration_seconds"`
	TargetLUFS      float64        `json:"target_lufs"`
	MeasuredLUFS    float64        `json:"measured_lufs,omitempty"`
	TruePeakDB      float64        `json:"true_peak_db,omitempty"`
	EstimatedCost   float64        `json:"estimated_cost,omitempty"`
	HasMix          bool           `json:"has_mix" jsonschema:"ミックス済みの音声ファイルがあるか"`
	Skipped         []SkippedOut   `json:"skipped" jsonschema:"作れなかった任意の工程と理由"`
	CreatedAt       time.Time      `json:"created_at"`
}

// SkippedOut is an audio step that was left out.
type SkippedOut struct {
	Layer  string `json:"layer"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// VoiceOut is a cloned voice of a speaker. The vendor's voice id is not part of it.
type VoiceOut struct {
	ID        string    `json:"id"`
	Speaker   string    `json:"speaker"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// QCOut is a Final QC result.
type QCOut struct {
	ID        string      `json:"id"`
	Verdict   string      `json:"verdict" jsonschema:"pass or fail"`
	Findings  []QCFinding `json:"findings"`
	CreatedAt time.Time   `json:"created_at"`
}

// QCFinding is one finding of Final QC.
type QCFinding struct {
	Check    string `json:"check"`
	Message  string `json:"message"`
	Stage    string `json:"stage" jsonschema:"戻り先の状態（EDITING or AUDIO）"`
	Advisory bool   `json:"advisory,omitempty" jsonschema:"true は警告（Final QC を落とさない）"`
}

// ExportOut is an exported video. Download it with get_export_download_url.
type ExportOut struct {
	ID          string    `json:"id"`
	BranchID    string    `json:"branch_id,omitempty"`
	Resolution  string    `json:"resolution"`
	AspectRatio string    `json:"aspect_ratio"`
	Codec       string    `json:"codec"`
	ExportedAt  time.Time `json:"exported_at"`
}

// ExportOptions are the choices of an export.
type ExportOptions struct {
	AspectRatio string `json:"aspect_ratio"`
	Resolution  string `json:"resolution"`
	Codec       string `json:"codec"`
}

// ExportChoices are the values each export option takes.
type ExportChoices struct {
	AspectRatios []string `json:"aspect_ratios"`
	Resolutions  []string `json:"resolutions"`
	Codecs       []string `json:"codecs"`
}

// NewViewOutput turns the editing state of the service into the output of the tools.
func NewViewOutput(v service.EditorView) ViewOutput {
	out := ViewOutput{
		ProjectID: v.Project.ID, ProjectName: v.Project.Name, State: string(v.Project.State), BranchID: v.BranchID,
		PendingShots: v.PendingShots, CanRender: v.CanRender,
		Voices:         make([]VoiceOut, 0, len(v.Voices)),
		Exports:        make([]ExportOut, 0, len(v.Exports)),
		ExportDefaults: ExportOptions{AspectRatio: v.Defaults.AspectRatio, Resolution: v.Defaults.Resolution, Codec: v.Defaults.Codec},
		ExportChoices:  ExportChoices{AspectRatios: domain.ExportAspectRatios(), Resolutions: domain.ExportResolutions(), Codecs: domain.ExportCodecs()},
	}
	if t := v.Timeline; t != nil {
		head := newTimelineHead(*t)
		out.Timeline = &head
	}
	out.Audio = newAudioOut(v.Audio, v.Timeline)
	if v.BranchID != "" {
		out.TrunkAudio = newAudioOut(v.TrunkAudio, nil)
	}
	for _, c := range v.Voices {
		out.Voices = append(out.Voices, VoiceOut{ID: c.ID, Speaker: c.Speaker, Name: c.Name, CreatedAt: c.CreatedAt})
	}
	if q := v.QC; q != nil {
		out.FinalQC = &QCOut{ID: q.ID, Verdict: string(q.Verdict), CreatedAt: q.CreatedAt, Findings: make([]QCFinding, 0, len(q.Findings))}
		for _, f := range q.Findings {
			out.FinalQC.Findings = append(out.FinalQC.Findings, QCFinding{Check: f.Check, Message: f.Message, Stage: string(f.Stage), Advisory: f.Advisory})
		}
	}
	for _, e := range v.Exports {
		out.Exports = append(out.Exports, ExportOut{ID: e.ID, BranchID: e.BranchID, Resolution: e.Resolution, AspectRatio: e.AspectRatio, Codec: e.Codec, ExportedAt: e.ExportedAt})
	}
	return out
}

func newTimelineHead(t service.TimelineView) TimelineHead {
	h := TimelineHead{
		ID: t.ID, BranchID: t.BranchID, VersionNumber: t.VersionNumber, ClipCount: len(t.Clips),
		DurationSeconds: t.Duration, Editable: t.Editable, CreatedAt: t.CreatedAt,
	}
	if j := t.Judge; j != nil {
		out := JudgeOut{ID: j.ID, Verdict: string(j.Verdict), CreatedAt: j.CreatedAt, Findings: make([]FindingOut, 0, len(j.Findings))}
		for _, f := range j.Findings {
			fo := FindingOut{ClipIndex: f.ClipIndex, Issue: f.Issue, Action: f.Action}
			if n := f.Need; n != nil {
				fo.NeedShot = &ShotNeedOut{Kind: string(n.Kind), AfterClip: n.AfterClip, Objective: n.Objective, DurationSeconds: n.DurationSeconds}
			}
			out.Findings = append(out.Findings, fo)
		}
		h.Judge = &out
	}
	return h
}

var audioLayers = []domain.AudioLayer{domain.LayerDialogue, domain.LayerNarration, domain.LayerBGM, domain.LayerSE, domain.LayerFoley, domain.LayerAmbience}

// newAudioOut summarizes an Audio Timeline; shown is the Edit Timeline of the same series (nil: unknown).
func newAudioOut(a *domain.AudioTimelineVersion, shown *service.TimelineView) *AudioOut {
	if a == nil {
		return nil
	}
	t := a.Timeline
	out := &AudioOut{
		ID: a.ID, VersionNumber: a.VersionNumber, EditTimelineID: t.EditTimelineID, Mode: t.Mode, SpeechLines: t.SpeechLines(),
		Layers: make(map[string]int, len(audioLayers)), DurationSeconds: t.DurationSeconds, TargetLUFS: t.TargetLUFS,
		MeasuredLUFS: t.MeasuredLUFS, TruePeakDB: t.TruePeakDB, EstimatedCost: t.EstimatedCost, HasMix: t.MixPath != "",
		Skipped: make([]SkippedOut, 0, len(t.Skipped)), CreatedAt: a.CreatedAt,
	}
	if shown != nil {
		current := a.CurrentFor(shown.ID)
		out.Current = &current
	}
	for _, l := range audioLayers {
		out.Layers[string(l)] = t.Count(l)
	}
	for _, s := range t.Skipped {
		out.Skipped = append(out.Skipped, SkippedOut{Layer: string(s.Layer), Reason: s.Reason, Detail: s.Detail})
	}
	return out
}
