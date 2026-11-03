// Package localspeech connects the Audio Director's local voice (GPT-SoVITS, package gptsovits) to the rest of mam: it is the
// server the voice calls, which the local runtime manager installs, starts and stops (package localruntime), and it finds the
// reference voice of every speaker of a script (package voicerefsvc), so that the Director can refuse a speaker without one before it
// pays or starts anything. Like localruntime it does not import package service: it gives the Kind of the failure and the
// service writes the user's sentence.
package localspeech

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

// The ids of the local voice runtimes of the catalog (data/runtimes/gptsovits.json and irodori-tts.json).
const (
	RuntimeID        = "gpt-sovits"
	IrodoriRuntimeID = "irodori-tts"
)

// Runtime is the part of *localruntime.Service the server uses.
type Runtime interface {
	// Prepare starts the runtime when it is stopped (the lazy start) and waits until it answers.
	Prepare(ctx context.Context, id string) error
	Status(ctx context.Context, id string) (localruntime.View, error)
	// Token is the token of the server's current start (the header X-Mam-Token of every request to it).
	Token(id string) (string, error)
}

// Server is the GPT-SoVITS server of the runtime manager, for gptsovits.New. For Irodori-TTS it is the Server of IrodoriServer.
type Server struct {
	Runtime Runtime
	// ID is the runtime ("" is GPT-SoVITS: RuntimeID) and Name what its sentences call it ("" is "GPT-SoVITS").
	ID, Name string
	// Vendor is the Vendor of the errors (localvoice.VendorIrodori; "" is GPT-SoVITS).
	Vendor string
	// Before runs before the runtime is started, only when its server is not running (nil for nothing): it can make room on the GPU or
	// refuse the start (package gpufree).
	Before func(ctx context.Context) error
}

// IrodoriServer is the Server of the Irodori-TTS runtime, for irodori.New.
func IrodoriServer(rt Runtime, before func(ctx context.Context) error) Server {
	return Server{Runtime: rt, ID: IrodoriRuntimeID, Name: "Irodori-TTS", Vendor: localvoice.VendorIrodori, Before: before}
}

// Ensure implements localvoice.Server: it starts the runtime when it is stopped and returns the address it listens on and the token of
// this start. Every refusal of the manager is an *localvoice.UnavailableError with its Kind.
func (s Server) Ensure(ctx context.Context) (string, string, error) {
	id, name := cmp.Or(s.ID, RuntimeID), cmp.Or(s.Name, "GPT-SoVITS")
	if s.Before != nil {
		if v, err := s.Runtime.Status(ctx, id); err == nil && v.Process.State != proc.StateRunning {
			if err := s.Before(ctx); err != nil {
				return "", "", err
			}
		}
	}
	if err := s.Runtime.Prepare(ctx, id); err != nil {
		return "", "", UnavailableFor(ctx, s.Vendor, err)
	}
	v, err := s.Runtime.Status(ctx, id)
	if err != nil {
		return "", "", UnavailableFor(ctx, s.Vendor, err)
	}
	// A server that died between Prepare and Status keeps its address in the Failed state: only a running one is an address to call.
	if v.Process.State != proc.StateRunning || v.Process.URL == "" {
		return "", "", &localvoice.UnavailableError{Kind: localvoice.KindProcess, Vendor: s.Vendor, Detail: name + " のアドレスを取得できませんでした"}
	}
	token, err := s.Runtime.Token(id)
	if err != nil {
		return "", "", UnavailableFor(ctx, s.Vendor, err)
	}
	if token == "" { // the server answers no request without it: say so here, not as a refusal of every line
		return "", "", &localvoice.UnavailableError{Kind: localvoice.KindProcess, Vendor: s.Vendor, Detail: name + " のトークンを取得できませんでした"}
	}
	return v.Process.URL, token, nil
}

// Unavailable turns the error of the runtime manager into the typed error of the local voice. ctx is the context of the call: its own
// cancellation is not a failure of the runtime and stays as it is.
func Unavailable(ctx context.Context, err error) error { return UnavailableFor(ctx, "", err) }

// UnavailableFor is Unavailable for the provider named vendor ("" is GPT-SoVITS; localvoice.VendorIrodori the voice Irodori-TTS;
// localvoice.VendorMuseTalk the lip sync).
func UnavailableFor(ctx context.Context, vendor string, err error) error {
	var (
		support *localruntime.SupportError
		weights *localruntime.WeightsError
	)
	un := func(kind localvoice.Kind, detail string) error {
		return &localvoice.UnavailableError{Kind: kind, Vendor: vendor, Detail: detail, Err: err}
	}
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.As(err, &support):
		kind := localvoice.KindUnsupported
		if support.Support.Code == rtm.CodeLowVRAM {
			kind = localvoice.KindGPUMemory
		}
		return un(kind, support.Error())
	case errors.Is(err, choice.ErrConsentRequired):
		return un(localvoice.KindConsent, "")
	case errors.Is(err, localruntime.ErrBusy):
		return un(localvoice.KindNotInstalled, "導入の最中です。終わってからもう一度実行してください")
	case errors.Is(err, localruntime.ErrTraining):
		return un(localvoice.KindProcess, "声の学習が動いています。終わってからもう一度実行してください")
	case errors.Is(err, localruntime.ErrNotInstalled), errors.Is(err, localruntime.ErrNotFound):
		return un(localvoice.KindNotInstalled, "")
	case errors.As(err, &weights):
		return un(localvoice.KindWeightsMissing, "足りない重み・依存ファイル: "+joinAssets(weights.Assets))
	case errors.Is(err, localruntime.ErrWeightsMissing):
		return un(localvoice.KindWeightsMissing, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return un(localvoice.KindTimeout, err.Error())
	}
	return un(localvoice.KindProcess, err.Error())
}

// Lookup finds the reference voice of a speaker (*voicerefsvc.Service).
type Lookup interface {
	ForSpeaker(ctx context.Context, projectID, branchID, speaker string) (voicerefsvc.Voice, error)
}

// Voices finds the reference voice of every speaker of the Scenes' lines (the narration is the Character named voiceref.NarrationName)
// in the Project, or in the Branch when branchID is set: the Characters of a Branch are the Episode's copies of the Series' canon, so
// they speak with the canon's recording. A speaker that has none is in the map with the Cause, never left out or given another voice:
// the Director refuses the run with every such name. A Project or Branch that does not exist is voicerefsvc.ErrNotFound.
func Voices(ctx context.Context, lookup Lookup, projectID, branchID string, scenes []audiodirector.Scene) (map[string]localvoice.Voice, error) {
	voices := map[string]localvoice.Voice{}
	for _, sc := range scenes {
		for _, entry := range sc.Lines {
			line, ok := domain.ParseVoiceLine(entry)
			if !ok {
				continue
			}
			name := localvoice.SpeakerOf(line)
			if _, done := voices[name]; done {
				continue
			}
			v, err := lookup.ForSpeaker(ctx, projectID, branchID, name)
			switch {
			case err == nil:
				voices[name] = localvoice.Voice{
					Path: v.Path, Text: v.Reference.Text, Language: v.Reference.Language, Duration: v.Reference.Duration(),
					Profile: localvoice.Profile{Voice: v.Character.Voice, Personality: v.Character.Personality},
				}
				if v.Trained != nil {
					lv := voices[name]
					lv.Trained = v.Trained.ID
					voices[name] = lv
				}
			case errors.Is(err, voicerefsvc.ErrNoWeights):
				voices[name] = localvoice.Voice{Cause: fmt.Errorf("%w: %w", localvoice.ErrTrainedVoice, err)}
			case errors.Is(err, voicerefsvc.ErrNoCharacter), errors.Is(err, voicerefsvc.ErrNoReference),
				errors.Is(err, voicerefsvc.ErrAmbiguous), errors.Is(err, voicerefsvc.ErrFileMissing):
				voices[name] = localvoice.Voice{Cause: err}
			default:
				return nil, fmt.Errorf("find the reference voice of %q: %w", name, err)
			}
		}
	}
	return voices, nil
}

// Reason is the sentence for the user of an error of the local voice or lip sync, and false for any other error. Every sentence of an
// unavailable provider is localvoice.Reason: the Director's Skipped list, the error of a run, the settings screen and the MCP tools say
// the same words.
func Reason(err error) (string, bool) {
	var (
		mv *localvoice.MissingVoiceError
		re *localvoice.ReferenceError
		nk *localvoice.NoCloudKeyError
		um interface{ UserMessage() string } // an error that carries its own sentence (audiosettings.ErrUnreadable)
	)
	switch {
	case errors.As(err, &mv) && len(brokenTrained(mv)) > 0:
		broken := brokenTrained(mv)
		text := mv.Role().LocalName() + "で話せません。学習した声のファイルが無いか壊れている話者がいます: " + joinNames(broken) +
			"。Character の「学習した声」を削除してゼロショットの声に戻すか、学習し直してください（別の声に黙って替えることはしません）"
		if len(broken) < len(mv.Speakers) {
			text += "。参照音声が無い話者もいます: " + joinNames(withoutTrained(mv))
		}
		return text, true
	case errors.As(err, &mv):
		return mv.Role().LocalName() + "で話せません。参照音声が無い話者がいます: " + joinNames(mv.Names()) +
			"。Character に参照音声を設定するか、声のプロバイダーを ElevenLabs に切り替えてください", true
	case errors.As(err, &re):
		return re.Role().LocalName() + "の参照音声を使えません（" + re.Speaker + "）: " + re.Problem +
			"。Character の参照音声を設定し直すか、声のプロバイダーを ElevenLabs に切り替えてください", true
	case errors.As(err, &nk):
		return nk.Error(), true
	case errors.As(err, &um):
		return um.UserMessage(), true
	case errors.Is(err, localvoice.ErrUnknownProvider):
		return "声のプロバイダーが正しくありません（elevenlabs か local を指定してください）", true
	case errors.Is(err, localvoice.ErrUnknownLipSyncProvider):
		return "口パクのプロバイダーが正しくありません（synclabs か local を指定してください）", true
	}
	return localvoice.UnavailableReason(err)
}

// brokenTrained are the speakers whose trained voice is gone or damaged; withoutTrained the other speakers that have no voice.
func brokenTrained(mv *localvoice.MissingVoiceError) []string { return namesWhere(mv, true) }

func withoutTrained(mv *localvoice.MissingVoiceError) []string { return namesWhere(mv, false) }

func namesWhere(mv *localvoice.MissingVoiceError, broken bool) []string {
	var names []string
	for _, s := range mv.Speakers {
		if errors.Is(s.Cause, localvoice.ErrTrainedVoice) == broken {
			names = append(names, s.Name)
		}
	}
	return names
}

func joinAssets(assets []rtm.AssetStatus) string {
	names := make([]string, 0, len(assets))
	for _, a := range assets {
		names = append(names, a.Name)
	}
	return strings.Join(names, "、")
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += "、"
		}
		out += "「" + n + "」"
	}
	return out
}
