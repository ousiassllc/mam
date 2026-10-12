package domain

import "time"

// Audio models. The values are the canonical identifiers of spec/architecture/model-adapters.md §6,
// stored in cost_ledger_entry.provider.
const (
	ModelElevenLabsTTS   ModelName = "elevenlabs-tts"
	ModelElevenLabsMusic ModelName = "elevenlabs-music"
	ModelElevenLabsSFX   ModelName = "elevenlabs-sfx"
	ModelSyncLabs        ModelName = "sync-labs"
)

// MaxSpeakerRunes bounds the speaker name of a cloned voice.
const MaxSpeakerRunes = 20

// AudioLayer is one layer of the Audio Timeline (spec/spec.md §18).
type AudioLayer string

// Audio layers. LayerLipSync is not a sound: it only names the Lip Sync step where a skipped
// optional step is reported.
const (
	LayerDialogue  AudioLayer = "dialogue"
	LayerNarration AudioLayer = "narration"
	LayerBGM       AudioLayer = "bgm"
	LayerSE        AudioLayer = "se"
	LayerFoley     AudioLayer = "foley"
	LayerAmbience  AudioLayer = "ambience"
	LayerLipSync   AudioLayer = "lip_sync"
)

// PlannedLayers lists the layers the Audio Director plans from the Scene Emotion and the editing
// tempo (the spoken layers come from the script).
func PlannedLayers() []AudioLayer {
	return []AudioLayer{LayerBGM, LayerSE, LayerAmbience, LayerFoley}
}

// Model is the model that makes the layer.
func (l AudioLayer) Model() ModelName {
	switch l {
	case LayerDialogue, LayerNarration:
		return ModelElevenLabsTTS
	case LayerBGM:
		return ModelElevenLabsMusic
	case LayerSE, LayerFoley, LayerAmbience:
		return ModelElevenLabsSFX
	case LayerLipSync:
		return ModelSyncLabs
	}
	return ""
}

// MixGainDB is the level of the layer in the mix relative to its source, in dB: the speech stays as
// it is, the rest sits below it.
func (l AudioLayer) MixGainDB() float64 {
	switch l {
	case LayerBGM:
		return -16
	case LayerAmbience:
		return -22
	case LayerSE:
		return -8
	case LayerFoley:
		return -12
	}
	return 0
}

// AudioScope is what the Quality Policy pays for in Audio Production: the layers that are made and
// whether the picture gets Lip Sync. Dialogue and Narration are always made.
type AudioScope struct {
	Planned []AudioLayer
	LipSync bool
	// LipSyncModel is the Sync Labs model of the Lip Sync.
	LipSyncModel string
}

// Has reports whether the scope makes the planned layer.
func (s AudioScope) Has(l AudioLayer) bool {
	for _, p := range s.Planned {
		if l == p {
			return true
		}
	}
	return false
}

// AudioScopeOf is the scope of the Quality Policy: FAST adds only BGM to the speech, BALANCED adds
// SE and Ambience, MAX QUALITY adds Foley and Lip Sync (with the most capable Sync Labs model).
func AudioScopeOf(q QualityPolicy) AudioScope {
	switch q {
	case QualityFast:
		return AudioScope{Planned: []AudioLayer{LayerBGM}}
	case QualityMaxQuality:
		return AudioScope{Planned: PlannedLayers(), LipSync: true, LipSyncModel: "sync-3"}
	}
	return AudioScope{Planned: []AudioLayer{LayerBGM, LayerSE, LayerAmbience}}
}

// AudioModeDirector marks an Audio Timeline the Audio Director made (layers planned from the Scene
// Emotion and the editing tempo). A timeline of the simple Audio Production of M1 has no mode.
const AudioModeDirector = "director"

// Editing tempo of a Scene, from the average length of its clips.
const (
	TempoFast   = "fast"
	TempoMedium = "medium"
	TempoSlow   = "slow"
)

// TempoOf classifies the average clip length of a Scene: under 2.5 s is fast, over 5 s is slow.
func TempoOf(avgClipSeconds float64) string {
	switch {
	case avgClipSeconds <= 0:
		return TempoMedium
	case avgClipSeconds < 2.5:
		return TempoFast
	case avgClipSeconds > 5:
		return TempoSlow
	}
	return TempoMedium
}

// AudioScene is what the Audio Director read of a Scene of the cut: where it sits in the video, how
// fast it is edited and the emotion it judged the Scene to have.
type AudioScene struct {
	// Index is the 1-based position of the Scene in the story.
	Index          int     `json:"index"`
	Title          string  `json:"title,omitempty"`
	Start          float64 `json:"start_seconds"`
	End            float64 `json:"end_seconds"`
	Tempo          string  `json:"tempo"`
	Emotion        string  `json:"emotion,omitempty"`
	Clips          int     `json:"clips"`
	AvgClipSeconds float64 `json:"avg_clip_seconds"`
}

// LipSyncClip is a clip of the Edit Timeline whose picture was redone by Lip Sync: Path is the
// lip-synced video (relative to the assets directory) and replaces the clip's Take at export.
type LipSyncClip struct {
	// ClipIndex is the 1-based clip of the Edit Timeline.
	ClipIndex       int       `json:"clip_index"`
	TakeID          string    `json:"take_id"`
	Path            string    `json:"path"`
	DurationSeconds float64   `json:"duration_seconds"`
	Model           ModelName `json:"model"`
}

// Reasons an optional step of the Audio Director was left out.
const (
	SkipPolicy = "policy" // the Quality Policy does not pay for it
	SkipBudget = "budget" // the budget ceiling would be exceeded
	SkipError  = "error"  // the provider or the planner failed
)

// SkippedAudio is an optional layer or step the Audio Director left out, and why.
type SkippedAudio struct {
	Layer  AudioLayer `json:"layer"`
	Prompt string     `json:"prompt,omitempty"`
	Reason string     `json:"reason"`
	Detail string     `json:"detail,omitempty"`
}

// VoiceClone is a cloned voice that speaks the lines of one speaker of a Project (ElevenLabs
// instant voice cloning).
type VoiceClone struct {
	ID        string
	ProjectID string
	// Speaker is the name the script gives the speaker ("美咲").
	Speaker string
	Name    string
	// VoiceID is the ElevenLabs voice.
	VoiceID   string
	CreatedAt time.Time
}
