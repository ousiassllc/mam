// Package audio is the "音声・口パク" category of the global settings screen (spec/components/settings-screen-categories.md §9): who speaks
// the lines (ElevenLabs in the cloud, or GPT-SoVITS on this PC), who redoes the mouths (Sync Labs, or MuseTalk 1.5 on this PC), where each
// local provider stands, why one cannot be chosen on this PC (macOS, no NVIDIA GPU), and whether a run whose local provider cannot be
// used goes to the cloud. Like the other organisms it knows only what the handler hands it, never the service layer.
package audio

import "github.com/ousiassllc/mam/backend/internal/web/atoms"

// PanelID is what a save replaces.
const PanelID = "audio-panel"

// The names of the form fields.
const (
	FieldVoice    = "voice_provider"
	FieldLipSync  = "lip_sync_provider"
	FieldFallback = "fallback_to_cloud"
)

// Description is what the category says under its title.
const Description = "声（セリフ・ナレーション）と口パクを、クラウド（ElevenLabs・Sync Labs）で作るか、この PC のローカルモデル（GPT-SoVITS・MuseTalk 1.5）で作るかを選びます。何も選ばないときはクラウドです。ローカルは Windows + NVIDIA の GPU だけで動かせます（mam が自動で導入・起動します）。保存した内容は、次の音声作成から使われます。"

// Local is the local provider of a card: where it stands and why it cannot be used.
type Local struct {
	// Name is the model ("GPT-SoVITS"); State is the text of the badge and Tone its colour.
	Name  string
	State string
	Tone  atoms.BadgeTone
	// Supported is false when this machine cannot run it: the radio is disabled and SupportReason says why and what to use instead.
	Supported     bool
	SupportReason string
	// Reason is, when it cannot be used now, the sentence a failed run says (the same words as the chat and the MCP tools); ProcessError is
	// the end of the log of a server that did not start.
	Reason       string
	ProcessError string
	// VRAM is the video memory it needs, as a sentence that says when the figure is mam's guess.
	VRAM string
	// SourceURL, License and LicenseURL are where the program comes from and its license; Condition is the note on its commercial use
	// ("" for none). Unclear are the weights and dependencies whose commercial use is not clear: said prominently.
	SourceURL  string
	License    string
	LicenseURL string
	Condition  string
	Unclear    []string
}

// Provider is the card of one choice (the voice or the lip sync).
type Provider struct {
	// ID is the id of the card; Title its heading; Field the name of its radio group.
	ID, Title, Field string
	// Description is what the choice is for.
	Description string
	// Selected is the saved provider ("elevenlabs"/"local" or "synclabs"/"local").
	Selected string
	// CloudValue is the value of the cloud radio, CloudLabel its label.
	CloudValue, CloudLabel string
	// LocalLabel is the label of the local radio.
	LocalLabel string
	// Local is nil in an app without the local runtimes (the local radio is then left out).
	Local *Local
	// CloudKey is the name of the API key of the cloud provider ("elevenlabs") and CloudKeySet whether it is stored.
	CloudKey    string
	CloudKeySet bool
	// Note is one more line under the card (what the choice does not change).
	Note string
}

// LocalValue is the value of the local radio.
const LocalValue = "local"

// Data is the category. Problem is why the settings file cannot be read (the rest is then not shown). Notice and Error are the result of
// the last save.
type Data struct {
	Problem string
	Notice  string
	Error   string
	Voice   Provider
	LipSync Provider
	// Fallback is whether a run whose local provider cannot be used goes to the cloud (off by default).
	Fallback bool
}
