// Package audiodirector is the Audio Director (F-20): it reads the Scenes of the cut (emotion and
// editing tempo), makes the Dialogue and Narration of the script with ElevenLabs TTS (and cloned
// voices), plans and makes BGM (ElevenLabs Music), SE / Foley / Ambience (ElevenLabs Sound Effects)
// and, under MAX QUALITY, Lip Sync (Sync Labs), then mixes every layer and normalizes the loudness
// with local DSP (ffmpeg, no AI API). See spec/architecture/model-adapters.md §5 and §14.
package audiodirector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// CredentialProvider is the credential.Store key of the ElevenLabs API key. One key serves the
// three ElevenLabs models of spec/architecture/model-adapters.md §6 (elevenlabs-tts / -music / -sfx).
const CredentialProvider = "elevenlabs"

// ErrNoAPIKey means no ElevenLabs API key is stored.
var ErrNoAPIKey = errors.New("audiodirector: ElevenLabs API key is not configured")

// Vendors named by ProviderError.
const (
	VendorElevenLabs = "elevenlabs"
	VendorSyncLabs   = "sync-labs"
)

// ProviderError is a failure of a vendor call. Status is the HTTP status, 0 for a network failure.
type ProviderError struct {
	// Vendor is VendorElevenLabs or VendorSyncLabs ("" reads as ElevenLabs).
	Vendor  string
	Status  int
	Message string
	Err     error
}

func (e *ProviderError) vendor() string {
	if e.Vendor == "" {
		return VendorElevenLabs
	}
	return e.Vendor
}

func (e *ProviderError) Error() string {
	switch {
	case e.Status != 0:
		return fmt.Sprintf("audiodirector: %s returned HTTP %d: %s", e.vendor(), e.Status, e.Message)
	case e.Err != nil:
		return fmt.Sprintf("audiodirector: %s request failed: %v", e.vendor(), e.Err)
	}
	return fmt.Sprintf("audiodirector: %s request failed: %s", e.vendor(), e.Message)
}

func (e *ProviderError) Unwrap() error { return e.Err }

// Speech is generated audio in a file format ffmpeg reads. (Music and sound effects use it too.)
type Speech struct {
	Data []byte
	// Ext is the file extension of the audio format, without the dot.
	Ext string
}

// Synthesizer turns a line of the script into speech. *ElevenLabs implements it.
type Synthesizer interface {
	Synthesize(ctx context.Context, line domain.VoiceLine) (Speech, error)
}

// Composer makes instrumental music of the given length from a prompt (ElevenLabs Music).
type Composer interface {
	Compose(ctx context.Context, prompt string, seconds float64) (Speech, error)
}

// SoundMaker makes a sound effect from a prompt (ElevenLabs Sound Effects). Loop asks for a sound
// that repeats smoothly (Ambience).
type SoundMaker interface {
	Generate(ctx context.Context, prompt string, seconds float64, loop bool) (Speech, error)
}

// Sample is a recording of the voice to clone.
type Sample struct {
	Filename string
	Data     []byte
}

// VoiceCloner clones a voice from a sample and returns the voice ID (ElevenLabs instant voice cloning).
type VoiceCloner interface {
	Clone(ctx context.Context, name string, sample Sample) (voiceID string, err error)
}

// Providers are the vendors the Audio Director calls. Nil members are left out: without Speech
// there is no audio production; without Music / Sounds / LipSync the layers they make are
// skipped and reported.
type Providers struct {
	Speech  Synthesizer
	Music   Composer
	Sounds  SoundMaker
	Cloner  VoiceCloner
	LipSync LipSyncer
}

// ElevenLabsProviders is Providers with every ElevenLabs capability of e.
func ElevenLabsProviders(e *ElevenLabs) Providers {
	return Providers{Speech: e, Music: e, Sounds: e, Cloner: e}
}

// Config is the ElevenLabs setting.
type Config struct {
	BaseURL string
	// ModelID is the TTS model. The multilingual model speaks Japanese.
	ModelID string
	// MusicModelID and SoundModelID are the Music and Sound Effects models.
	MusicModelID string
	SoundModelID string
	// DialogueVoiceID and NarrationVoiceID are the voices of the two kinds of line.
	DialogueVoiceID  string
	NarrationVoiceID string
	// Timeout bounds one TTS, Sound Effects or cloning call; MusicTimeout one Music call.
	Timeout      time.Duration
	MusicTimeout time.Duration
}

// DefaultConfig is the setting used unless the caller changes it. The voices are ElevenLabs'
// premade ones ("Adam" for Dialogue, "Rachel" for Narration).
func DefaultConfig() Config {
	return Config{
		BaseURL:          "https://api.elevenlabs.io",
		ModelID:          "eleven_multilingual_v2",
		MusicModelID:     "music_v2",
		SoundModelID:     "eleven_text_to_sound_v2",
		DialogueVoiceID:  "pNInz6obpgDQGcFmaJgB",
		NarrationVoiceID: "21m00Tcm4TlvDq8ikWAM",
		Timeout:          60 * time.Second,
		MusicTimeout:     180 * time.Second,
	}
}

// Bounds ElevenLabs accepts for a generated sound.
const (
	MinMusicSeconds = 3.0
	MaxMusicSeconds = 600.0
	MinSFXSeconds   = 0.5
	MaxSFXSeconds   = 30.0
)

// maxSpeechBytes bounds one generated audio file.
const maxSpeechBytes = 32 << 20

// ElevenLabs is the ElevenLabs adapter: TTS, voice cloning, Music and Sound Effects.
type ElevenLabs struct {
	cfg    Config
	keys   credential.Store
	client httpclient.Doer
}

// NewElevenLabs returns an ElevenLabs adapter. A nil client selects a plain http.Client; zero
// fields of cfg take the DefaultConfig values.
func NewElevenLabs(cfg Config, keys credential.Store, client httpclient.Doer) *ElevenLabs {
	def := DefaultConfig()
	fill := func(v *string, d string) {
		if *v == "" {
			*v = d
		}
	}
	fill(&cfg.BaseURL, def.BaseURL)
	fill(&cfg.ModelID, def.ModelID)
	fill(&cfg.MusicModelID, def.MusicModelID)
	fill(&cfg.SoundModelID, def.SoundModelID)
	fill(&cfg.DialogueVoiceID, def.DialogueVoiceID)
	fill(&cfg.NarrationVoiceID, def.NarrationVoiceID)
	if cfg.Timeout <= 0 {
		cfg.Timeout = def.Timeout
	}
	if cfg.MusicTimeout <= 0 {
		cfg.MusicTimeout = def.MusicTimeout
	}
	if client == nil {
		client = httpclient.New()
	}
	return &ElevenLabs{cfg: cfg, keys: keys, client: client}
}

// Synthesize implements Synthesizer (model identifier elevenlabs-tts). A line with a VoiceID is
// spoken by that (cloned) voice.
func (e *ElevenLabs) Synthesize(ctx context.Context, line domain.VoiceLine) (Speech, error) {
	voice := line.VoiceID
	if voice == "" {
		voice = e.cfg.DialogueVoiceID
		if line.Kind == domain.VoiceNarration {
			voice = e.cfg.NarrationVoiceID
		}
	}
	body, err := json.Marshal(map[string]string{"text": line.Text, "model_id": e.cfg.ModelID})
	if err != nil {
		return Speech{}, fmt.Errorf("marshal request: %w", err)
	}
	return e.audio(ctx, e.cfg.Timeout, "/v1/text-to-speech/"+url.PathEscape(voice)+"?output_format=mp3_44100_128", body)
}

// Compose implements Composer (model identifier elevenlabs-music): instrumental music of the given
// length (3 to 600 seconds).
func (e *ElevenLabs) Compose(ctx context.Context, prompt string, seconds float64) (Speech, error) {
	if seconds < MinMusicSeconds || seconds > MaxMusicSeconds {
		return Speech{}, fmt.Errorf("audiodirector: music length %.1f s is outside %.0f-%.0f s", seconds, MinMusicSeconds, MaxMusicSeconds)
	}
	body, err := json.Marshal(map[string]any{
		"prompt": prompt, "music_length_ms": int(seconds * 1000), "model_id": e.cfg.MusicModelID, "force_instrumental": true,
	})
	if err != nil {
		return Speech{}, fmt.Errorf("marshal request: %w", err)
	}
	return e.audio(ctx, e.cfg.MusicTimeout, "/v1/music?output_format=mp3_44100_128", body)
}

// Generate implements SoundMaker (model identifier elevenlabs-sfx): a sound effect of 0.5 to 30 seconds.
func (e *ElevenLabs) Generate(ctx context.Context, prompt string, seconds float64, loop bool) (Speech, error) {
	if seconds < MinSFXSeconds || seconds > MaxSFXSeconds {
		return Speech{}, fmt.Errorf("audiodirector: sound length %.1f s is outside %.1f-%.0f s", seconds, MinSFXSeconds, MaxSFXSeconds)
	}
	body, err := json.Marshal(map[string]any{"text": prompt, "duration_seconds": seconds, "loop": loop, "model_id": e.cfg.SoundModelID})
	if err != nil {
		return Speech{}, fmt.Errorf("marshal request: %w", err)
	}
	return e.audio(ctx, e.cfg.Timeout, "/v1/sound-generation?output_format=mp3_44100_128", body)
}

// Clone implements VoiceCloner: instant voice cloning from one sample.
func (e *ElevenLabs) Clone(ctx context.Context, name string, sample Sample) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("name", name); err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	filename := sample.Filename
	if filename == "" {
		filename = "sample.mp3"
	}
	part, err := mw.CreateFormFile("files", filename)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	if _, err := part.Write(sample.Data); err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	data, err := e.do(ctx, e.cfg.Timeout, "/v1/voices/add", mw.FormDataContentType(), "application/json", buf.Bytes(), 1<<20)
	if err != nil {
		return "", err
	}
	var out struct {
		VoiceID string `json:"voice_id"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.VoiceID == "" {
		return "", &ProviderError{Status: http.StatusOK, Message: "the answer has no voice_id"}
	}
	return out.VoiceID, nil
}

// audio posts a JSON body and returns the mp3 the API answers with.
func (e *ElevenLabs) audio(ctx context.Context, timeout time.Duration, path string, body []byte) (Speech, error) {
	data, err := e.do(ctx, timeout, path, "application/json", "audio/mpeg", body, maxSpeechBytes)
	if err != nil {
		return Speech{}, err
	}
	return Speech{Data: data, Ext: "mp3"}, nil
}

// do posts body to the API and returns the answer of at most limit bytes.
func (e *ElevenLabs) do(ctx context.Context, timeout time.Duration, path, contentType, accept string, body []byte, limit int64) ([]byte, error) {
	key, err := e.keys.Get(CredentialProvider)
	if errors.Is(err, credential.ErrNotFound) {
		return nil, ErrNoAPIKey
	}
	if err != nil {
		return nil, fmt.Errorf("read ElevenLabs API key: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(e.cfg.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("xi-api-key", key)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", accept)
	res, err := e.client.Do(req)
	if err != nil {
		return nil, &ProviderError{Err: redact(err, key)}
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, &ProviderError{Err: err}
	}
	if res.StatusCode/100 != 2 {
		return nil, &ProviderError{Status: res.StatusCode, Message: snippet(data)}
	}
	if len(data) == 0 || int64(len(data)) > limit {
		return nil, &ProviderError{Status: res.StatusCode, Message: "the answer is empty or too large"}
	}
	return data, nil
}

// snippet is the start of an error body, for the message.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// redact keeps the API key and the request URL out of an error message: a transport error carries
// the full URL (a signed output link, for one), which must not reach the saved skip detail or the chat.
func redact(err error, key string) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if key == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), key, "***"))
}
