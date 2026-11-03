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
const Description = "声（セリフ・ナレーション）と口パクを、クラウド（ElevenLabs・Sync Labs）で作るか、この PC のローカルモデル（GPT-SoVITS・Irodori-TTS・MuseTalk 1.5）で作るかを選びます。何も選ばないときはクラウドです。ローカルは Windows + NVIDIA の GPU だけで動かせます（mam が自動で導入・起動します）。保存した内容は、次の音声作成から使われます。"

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
	// Model is the model in force of a runtime with several ("" for none) and Cautions are the restrictions of its use, said with it.
	Model    string
	Cautions []string
}

// The slugs of the two choices in the paths of their details, and of the two providers of each.
const (
	SlugVoice   = "voice"
	SlugLipSync = "lip-sync"
	WhichCloud  = "cloud"
	WhichLocal  = "local"
	// WhichIrodori is the card of the second local voice (the value of its radio is IrodoriValue).
	WhichIrodori = "irodori"
)

// Provider is one choice (the voice or the lip sync): a heading over the cards of its providers, the cloud one and the local one.
type Provider struct {
	// ID is the id of the choice (the cards' ids start with it); Title its heading; Field the name of its radio group; Slug names it in the
	// path of the detail of a card.
	ID, Title, Field, Slug string
	// Description is what the choice is for.
	Description string
	// Selected is the saved provider ("elevenlabs"/"local" or "synclabs"/"local").
	Selected string
	// CloudValue is the value of the cloud radio, CloudLabel its label.
	CloudValue, CloudLabel string
	// LocalLabel is the label of the local radio.
	LocalLabel string
	// Local is nil in an app without the local runtimes (the local radio is then left out). Irodori is the second local provider of the voice
	// (nil for the others and in an app without the runtimes); As shapes the provider as the card of one of them.
	Local        *Local
	Irodori      *Local
	IrodoriLabel string
	localValue   string
	// CloudKey is the name of the API key of the cloud provider ("elevenlabs") and CloudKeySet whether it is stored.
	CloudKey    string
	CloudKeySet bool
	// Note is one more line under the card (what the choice does not change).
	Note string
	// CloneNotice is the notice under the cards of a choice that copies a voice from a recording ("" for none): the recording must be of a
	// voice the user may copy (the person's own consent).
	CloneNotice string
}

// LocalValue is the value of the local radio, IrodoriValue the value of the radio of the second local voice.
const (
	LocalValue   = "local"
	IrodoriValue = "irodori"
)

// As is the provider as the card of its local provider which (WhichLocal or WhichIrodori) shows it: the card draws p.Local, so the second local
// provider is put in its place, with its own radio value.
func (p Provider) As(which string) Provider {
	if which == WhichIrodori {
		p.Local, p.LocalLabel, p.localValue = p.Irodori, p.IrodoriLabel, IrodoriValue
	}
	return p
}

// value is the value of the radio of the local provider the card draws, and slug its name in ids and in the path of its detail.
func (p Provider) value() string {
	if p.localValue != "" {
		return p.localValue
	}
	return LocalValue
}

func (p Provider) slug() string {
	if p.localValue == IrodoriValue {
		return WhichIrodori
	}
	return WhichLocal
}

// ChosenLocal reports whether the saved provider is a local one that this PC can run (the radio is then on); the cloud radio is on otherwise.
func (p Provider) ChosenLocal() bool {
	return p.Selected == p.value() && p.Local != nil && p.Local.Supported
}

// Choice is the choice the slug names, or false when there is none.
func (d Data) Choice(slug string) (Provider, bool) {
	switch slug {
	case SlugVoice:
		return d.Voice, true
	case SlugLipSync:
		return d.LipSync, true
	}
	return Provider{}, false
}

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
