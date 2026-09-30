package domain

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// TransitionType is how a clip of the Edit Timeline follows the one before it (F-18 Simple).
type TransitionType string

// Transition types. Cut is a hard cut; the others overlap the two clips for Transition.Seconds.
const (
	TransitionCut       TransitionType = "cut"
	TransitionFade      TransitionType = "fade"
	TransitionFadeBlack TransitionType = "fade_black"
	TransitionDissolve  TransitionType = "dissolve"
)

// TransitionTypes lists every valid TransitionType.
func TransitionTypes() []TransitionType {
	return []TransitionType{TransitionCut, TransitionFade, TransitionFadeBlack, TransitionDissolve}
}

// Valid reports whether t is a known transition.
func (t TransitionType) Valid() bool {
	for _, v := range TransitionTypes() {
		if t == v {
			return true
		}
	}
	return false
}

// Limits of the Edit Timeline.
const (
	// MinClipSeconds is the shortest clip (Timing).
	MinClipSeconds = 0.5
	// MaxTransitionSeconds is the longest transition that can be asked for.
	MaxTransitionSeconds = 2.0
	// DefaultTransitionSeconds is the length of the fade Rough Cut puts between Scenes.
	DefaultTransitionSeconds = 0.25
	// MaxClips bounds the number of clips of a timeline.
	MaxClips = 200
)

// Transition is the switch from the previous clip into this one.
type Transition struct {
	Type    TransitionType `json:"type"`
	Seconds float64        `json:"seconds"`
}

// EditClip is one Shot of the Edit Timeline: the part of its Take from In to Out, and the
// transition into it (Shot Selection, Cut Point, Timing, Transition).
type EditClip struct {
	ShotID string `json:"shot_id"`
	TakeID string `json:"take_id"`
	// SourceSeconds is the length of the Take file (0 when unknown); Out never needs to exceed it.
	SourceSeconds float64    `json:"source_seconds,omitempty"`
	In            float64    `json:"in_point"`
	Out           float64    `json:"out_point"`
	Transition    Transition `json:"transition"`
}

// Length is the duration of the clip in seconds.
func (c EditClip) Length() float64 { return c.Out - c.In }

// EditTimeline is the sequence of clips of a Rough Cut (edit_timeline_version.timeline).
type EditTimeline struct {
	Clips []EditClip `json:"clips"`
}

// ErrInvalidTimeline marks a timeline the editor must not save.
var ErrInvalidTimeline = errors.New("invalid edit timeline")

func invalidTimeline(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidTimeline, fmt.Sprintf(format, args...))
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Normalize fixes what has one right value (a cut has no length, the first clip has no
// transition) and validates the rest. It returns a copy.
func (t EditTimeline) Normalize() (EditTimeline, error) {
	if len(t.Clips) == 0 {
		return EditTimeline{}, invalidTimeline("タイムラインにクリップがありません")
	}
	if len(t.Clips) > MaxClips {
		return EditTimeline{}, invalidTimeline("クリップは最大 %d 個までです", MaxClips)
	}
	out := EditTimeline{Clips: make([]EditClip, len(t.Clips))}
	for i, c := range t.Clips {
		if c.ShotID == "" || c.TakeID == "" {
			return EditTimeline{}, invalidTimeline("クリップ %d に Shot または Take が指定されていません", i+1)
		}
		if !finite(c.In) || !finite(c.Out) || c.In < 0 || c.Length() < MinClipSeconds {
			return EditTimeline{}, invalidTimeline("クリップ %d は %.1f 秒以上の区間にしてください", i+1, MinClipSeconds)
		}
		if c.SourceSeconds > 0 && c.Out > c.SourceSeconds+0.05 {
			return EditTimeline{}, invalidTimeline("クリップ %d の終了点が Take の長さ（%.1f 秒）を超えています", i+1, c.SourceSeconds)
		}
		if c.Transition.Type == "" {
			c.Transition.Type = TransitionCut
		}
		if !c.Transition.Type.Valid() {
			return EditTimeline{}, invalidTimeline("クリップ %d の Transition が正しくありません: %s", i+1, c.Transition.Type)
		}
		switch {
		case i == 0 || c.Transition.Type == TransitionCut:
			c.Transition = Transition{Type: TransitionCut}
		case !finite(c.Transition.Seconds) || c.Transition.Seconds <= 0 || c.Transition.Seconds > MaxTransitionSeconds:
			return EditTimeline{}, invalidTimeline("クリップ %d の Transition は 0 秒より長く %.0f 秒以下にしてください", i+1, MaxTransitionSeconds)
		}
		out.Clips[i] = c
	}
	return out, nil
}

// OverlapSeconds is how long clip i overlaps the clip before it: the transition length, capped
// at half of the shorter of the two clips so that no clip is used up by its two transitions.
func (t EditTimeline) OverlapSeconds(i int) float64 {
	if i <= 0 || i >= len(t.Clips) || t.Clips[i].Transition.Type == TransitionCut {
		return 0
	}
	return min(t.Clips[i].Transition.Seconds, t.Clips[i-1].Length()/2, t.Clips[i].Length()/2)
}

// Starts is the time each clip begins at in the finished video.
func (t EditTimeline) Starts() []float64 {
	starts := make([]float64, len(t.Clips))
	at := 0.0
	for i, c := range t.Clips {
		at -= t.OverlapSeconds(i)
		starts[i] = max(at, 0)
		at += c.Length()
	}
	return starts
}

// Duration is the length of the finished video.
func (t EditTimeline) Duration() float64 {
	if len(t.Clips) == 0 {
		return 0
	}
	last := len(t.Clips) - 1
	return t.Starts()[last] + t.Clips[last].Length()
}

// EditTimelineVersion is one row of edit_timeline_version. Every edit appends a version.
type EditTimelineVersion struct {
	ID            string
	ProjectID     string
	VersionNumber int
	Timeline      EditTimeline
	CreatedAt     time.Time
}

// EditorVerdict is the result of the Editor Judge (editor_judge_result.result).
type EditorVerdict string

// Editor Judge verdicts. NeedShot (missing material, F-19) is judged from M3 on.
const (
	EditorPass     EditorVerdict = "pass"
	EditorReEdit   EditorVerdict = "re_edit"
	EditorNeedShot EditorVerdict = "need_shot"
)

// EditorFinding is one thing the Editor Judge found wrong, with the edit that fixes it.
type EditorFinding struct {
	// ClipIndex is the 1-based position of the clip (0: the timeline as a whole).
	ClipIndex int    `json:"clip_index"`
	Issue     string `json:"issue"`
	Action    string `json:"action"`
}

// EditorJudgeResult is one row of editor_judge_result.
type EditorJudgeResult struct {
	ID                    string
	EditTimelineVersionID string
	Verdict               EditorVerdict
	Findings              []EditorFinding
	CreatedAt             time.Time
}

// VoiceKind tells the two spoken tracks of the simple Audio Timeline apart.
type VoiceKind string

// Voice kinds.
const (
	VoiceDialogue  VoiceKind = "dialogue"
	VoiceNarration VoiceKind = "narration"
)

// VoiceLine is one spoken line of the script: who says it and what.
type VoiceLine struct {
	Kind    VoiceKind `json:"kind"`
	Speaker string    `json:"speaker,omitempty"`
	Text    string    `json:"text"`
}

// ParseVoiceLine reads a script dialogue entry: "speaker: line", or a bare line, or
// "ナレーション: line" / "NA: line" (narration).
func ParseVoiceLine(entry string) (VoiceLine, bool) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return VoiceLine{}, false
	}
	speaker, text, found := strings.Cut(entry, ":")
	if !found {
		speaker, text, found = strings.Cut(entry, "：")
	}
	if !found || strings.ContainsAny(speaker, "。.!?！？0123456789０１２３４５６７８９") || len([]rune(speaker)) > 20 {
		return VoiceLine{Kind: VoiceNarration, Text: entry}, true
	}
	speaker, text = strings.TrimSpace(speaker), strings.TrimSpace(text)
	if text == "" {
		return VoiceLine{}, false
	}
	switch strings.ToLower(speaker) {
	case "ナレーション", "ナレ", "narration", "narrator", "na", "n":
		return VoiceLine{Kind: VoiceNarration, Text: text}, true
	}
	return VoiceLine{Kind: VoiceDialogue, Speaker: speaker, Text: text}, true
}

// AudioClip is a synthesized line placed on the Audio Timeline.
type AudioClip struct {
	VoiceLine
	// Path is the audio file, relative to the assets directory.
	Path            string  `json:"path"`
	StartSeconds    float64 `json:"start_seconds"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// End is the second the clip stops at.
func (c AudioClip) End() float64 { return c.StartSeconds + c.DurationSeconds }

// DefaultTargetLUFS is the integrated loudness the mix is normalized to (SNS delivery).
const DefaultTargetLUFS = -14.0

// AudioTimeline is what audio_timeline_version.timeline stores for the simple Audio Production
// (F-20 Simple): the spoken lines and the mastered mix.
type AudioTimeline struct {
	// EditTimelineID is the Edit Timeline version the audio was made for.
	EditTimelineID string      `json:"edit_timeline_id"`
	Clips          []AudioClip `json:"clips"`
	// SpeechExpected is true when the script has lines to speak.
	SpeechExpected bool `json:"speech_expected"`
	// MixPath is the mastered audio, relative to the assets directory ("" when there is no sound).
	MixPath         string  `json:"mix_path,omitempty"`
	DurationSeconds float64 `json:"duration_seconds"`
	TargetLUFS      float64 `json:"target_lufs"`
	// MeasuredLUFS is the integrated loudness of the mix after normalization.
	MeasuredLUFS float64 `json:"measured_lufs,omitempty"`
	TruePeakDB   float64 `json:"true_peak_db,omitempty"`
}

// AudioTimelineVersion is one row of audio_timeline_version.
type AudioTimelineVersion struct {
	ID            string
	ProjectID     string
	VersionNumber int
	Timeline      AudioTimeline
	CreatedAt     time.Time
}

// FinalQCVerdict is the result of Final QC.
type FinalQCVerdict string

// Final QC verdicts.
const (
	FinalQCPass FinalQCVerdict = "pass"
	FinalQCFail FinalQCVerdict = "fail"
)

// QCFinding is one finding of Final QC. Stage is where the Project goes back to: StateEditing or
// StateAudio. An Advisory finding (a broken Advisory Brand constraint) is a warning: it does not
// fail Final QC.
type QCFinding struct {
	Check    string       `json:"check"`
	Message  string       `json:"message"`
	Stage    ProjectState `json:"stage"`
	Advisory bool         `json:"advisory,omitempty"`
}

// FinalQCResult is one row of final_qc_result.
type FinalQCResult struct {
	ID                     string
	ProjectID              string
	AudioTimelineVersionID string
	Verdict                FinalQCVerdict
	Findings               []QCFinding
	CreatedAt              time.Time
}

// Export codecs and the container each is written in.
const (
	CodecH264 = "h264"
	CodecH265 = "h265"
	CodecVP9  = "vp9"
)

// ExportCodecs lists the codecs an export can use.
func ExportCodecs() []string { return []string{CodecH264, CodecH265, CodecVP9} }

// ExportExtension is the file extension of the codec's container.
func ExportExtension(codec string) string {
	if codec == CodecVP9 {
		return "webm"
	}
	return "mp4"
}

// ExportAspectRatios lists the aspect ratios an export can use.
func ExportAspectRatios() []string { return []string{"16:9", "9:16", "1:1", "4:5"} }

// ExportResolutions lists the resolutions an export can use: the pixels of the short side.
func ExportResolutions() []string { return []string{"720p", "1080p", "1440p", "2160p"} }

// ExportOptions is what the user chooses for an export.
type ExportOptions struct {
	AspectRatio string
	Resolution  string
	Codec       string
}

// Size is the picture size of the options: the short side is the resolution and the long side
// follows the aspect ratio (1080p at 9:16 is 1080x1920), both even.
func (o ExportOptions) Size() (width, height int, err error) {
	w, h, ok := strings.Cut(o.AspectRatio, ":")
	rw, err1 := strconv.Atoi(w)
	rh, err2 := strconv.Atoi(h)
	if !ok || err1 != nil || err2 != nil || rw <= 0 || rh <= 0 {
		return 0, 0, fmt.Errorf("aspect ratio %q is not W:H", o.AspectRatio)
	}
	short := ResolutionHeight(o.Resolution)
	if short <= 0 {
		return 0, 0, fmt.Errorf("resolution %q is not like 1080p", o.Resolution)
	}
	long := int(math.Round(float64(short) * float64(max(rw, rh)) / float64(min(rw, rh))))
	long &^= 1
	if rw >= rh {
		return long, short, nil
	}
	return short, long, nil
}

// Export is one row of export: a video written to disk.
type Export struct {
	ID          string
	ProjectID   string
	FilePath    string // relative to the assets directory
	Resolution  string
	AspectRatio string
	Codec       string
	ExportedAt  time.Time
}
