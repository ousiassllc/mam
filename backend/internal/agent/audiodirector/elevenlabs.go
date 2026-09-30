// Package audiodirector is the simple Audio Director (F-20, M1): it speaks the Dialogue and
// Narration lines of the script with ElevenLabs TTS, mixes them with the sound of the Rough Cut and
// normalizes the loudness with local DSP (ffmpeg, no AI API). BGM, sound effects, Foley, Ambience and
// Lip Sync belong to the full Audio Director of M3 (spec/architecture/model-adapters.md §5).
package audiodirector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// CredentialProvider is the credential.Store key of the ElevenLabs API key (the vendor name of
// spec/architecture/model-adapters.md §6, "elevenlabs-tts").
const CredentialProvider = "elevenlabs"

// ErrNoAPIKey means no ElevenLabs API key is stored.
var ErrNoAPIKey = errors.New("audiodirector: ElevenLabs API key is not configured")

// ProviderError is a failure of an ElevenLabs call. Status is the HTTP status, 0 for a network failure.
type ProviderError struct {
	Status  int
	Message string
	Err     error
}

func (e *ProviderError) Error() string {
	switch {
	case e.Status != 0:
		return fmt.Sprintf("audiodirector: elevenlabs returned HTTP %d: %s", e.Status, e.Message)
	case e.Err != nil:
		return fmt.Sprintf("audiodirector: elevenlabs request failed: %v", e.Err)
	}
	return "audiodirector: elevenlabs request failed: " + e.Message
}

func (e *ProviderError) Unwrap() error { return e.Err }

// Speech is a synthesized line.
type Speech struct {
	Data []byte
	// Ext is the file extension of the audio format, without the dot.
	Ext string
}

// Synthesizer turns a line of the script into speech. *ElevenLabs implements it.
type Synthesizer interface {
	Synthesize(ctx context.Context, line domain.VoiceLine) (Speech, error)
}

// Config is the ElevenLabs setting.
type Config struct {
	BaseURL string
	// ModelID is the TTS model. The multilingual model speaks Japanese.
	ModelID string
	// DialogueVoiceID and NarrationVoiceID are the voices of the two kinds of line.
	DialogueVoiceID  string
	NarrationVoiceID string
	// Timeout bounds one synthesis call.
	Timeout time.Duration
}

// DefaultConfig is the setting used unless the caller changes it. The voices are ElevenLabs'
// premade ones ("Adam" for Dialogue, "Rachel" for Narration).
func DefaultConfig() Config {
	return Config{
		BaseURL:          "https://api.elevenlabs.io",
		ModelID:          "eleven_multilingual_v2",
		DialogueVoiceID:  "pNInz6obpgDQGcFmaJgB",
		NarrationVoiceID: "21m00Tcm4TlvDq8ikWAM",
		Timeout:          60 * time.Second,
	}
}

// maxSpeechBytes bounds one synthesized line.
const maxSpeechBytes = 32 << 20

// Doer is the part of *http.Client ElevenLabs uses.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// ElevenLabs is the Synthesizer backed by the ElevenLabs text-to-speech API.
type ElevenLabs struct {
	cfg    Config
	keys   credential.Store
	client Doer
}

// NewElevenLabs returns an ElevenLabs Synthesizer. A nil client selects a plain http.Client; zero
// fields of cfg take the DefaultConfig values.
func NewElevenLabs(cfg Config, keys credential.Store, client Doer) *ElevenLabs {
	def := DefaultConfig()
	if cfg.BaseURL == "" {
		cfg.BaseURL = def.BaseURL
	}
	if cfg.ModelID == "" {
		cfg.ModelID = def.ModelID
	}
	if cfg.DialogueVoiceID == "" {
		cfg.DialogueVoiceID = def.DialogueVoiceID
	}
	if cfg.NarrationVoiceID == "" {
		cfg.NarrationVoiceID = def.NarrationVoiceID
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = def.Timeout
	}
	if client == nil {
		client = &http.Client{}
	}
	return &ElevenLabs{cfg: cfg, keys: keys, client: client}
}

// Synthesize implements Synthesizer.
func (e *ElevenLabs) Synthesize(ctx context.Context, line domain.VoiceLine) (Speech, error) {
	key, err := e.keys.Get(CredentialProvider)
	if errors.Is(err, credential.ErrNotFound) {
		return Speech{}, ErrNoAPIKey
	}
	if err != nil {
		return Speech{}, fmt.Errorf("read ElevenLabs API key: %w", err)
	}
	voice := e.cfg.DialogueVoiceID
	if line.Kind == domain.VoiceNarration {
		voice = e.cfg.NarrationVoiceID
	}
	body, err := json.Marshal(map[string]string{"text": line.Text, "model_id": e.cfg.ModelID})
	if err != nil {
		return Speech{}, fmt.Errorf("marshal request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	defer cancel()
	target := strings.TrimRight(e.cfg.BaseURL, "/") + "/v1/text-to-speech/" + url.PathEscape(voice) + "?output_format=mp3_44100_128"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return Speech{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("xi-api-key", key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")
	res, err := e.client.Do(req)
	if err != nil {
		return Speech{}, &ProviderError{Err: redact(err, key)}
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxSpeechBytes+1))
	if err != nil {
		return Speech{}, &ProviderError{Err: err}
	}
	if res.StatusCode/100 != 2 {
		return Speech{}, &ProviderError{Status: res.StatusCode, Message: snippet(data)}
	}
	if len(data) == 0 || len(data) > maxSpeechBytes {
		return Speech{}, &ProviderError{Status: res.StatusCode, Message: "the audio is empty or too large"}
	}
	return Speech{Data: data, Ext: "mp3"}, nil
}

// snippet is the start of an error body, for the message.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// redact keeps the API key out of an error message.
func redact(err error, key string) error {
	if key == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), key, "***"))
}
