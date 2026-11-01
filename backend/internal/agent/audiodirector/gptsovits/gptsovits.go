// Package gptsovits is the local voice of the Audio Director: it speaks a line of the script with GPT-SoVITS, the server
// (api_v2.py) that mam installs, starts and stops itself (package localruntime), in the voice of the Character's reference
// recording (spec/architecture/model-adapters-m3.md §14.8). It implements audiodirector.LocalSynthesizer.
package gptsovits

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// Defaults of Config.
const (
	// DefaultTimeout bounds the speaking of one line (a line of a few seconds takes seconds on a GPU; this is the backstop
	// against a server that took the request and never answers).
	DefaultTimeout = 3 * time.Minute
	// DefaultTextLanguage is the language of the lines. The script of a Project is Japanese (GPT-SoVITS "ja" also reads the
	// English words in it).
	DefaultTextLanguage = "ja"
	// maxSpeechBytes bounds one generated audio file: the same limit as the ElevenLabs answers.
	maxSpeechBytes = 32 << 20
	// splitMethod is the text_split_method of api_v2: "cut5" splits the line at every punctuation mark.
	splitMethod = "cut5"
)

// Config is the setting of the client; zero fields take the defaults.
type Config struct {
	// Timeout bounds one line: the call to the server, not its start.
	Timeout time.Duration
	// TextLanguage is the language of the lines (api_v2 text_lang).
	TextLanguage string
}

// Client speaks lines with the GPT-SoVITS server. The GPU is one: the lines of all runs are spoken one after another.
type Client struct {
	cfg    Config
	server localvoice.Server
	doer   httpclient.Doer
	gpu    chan struct{}
}

// New returns a Client of the server. A nil doer selects a plain HTTP client.
func New(server localvoice.Server, cfg Config, doer httpclient.Doer) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.TextLanguage == "" {
		cfg.TextLanguage = DefaultTextLanguage
	}
	if doer == nil {
		doer = httpclient.New()
	}
	return &Client{cfg: cfg, server: server, doer: doer, gpu: make(chan struct{}, 1)}
}

// Prepare starts the server when it is stopped and waits until it answers (audiodirector.LocalSynthesizer). A server that cannot
// be used is an *localvoice.UnavailableError.
func (c *Client) Prepare(ctx context.Context) error {
	_, err := c.server.Ensure(ctx)
	return err
}

// request is the body of POST /tts of api_v2.py (the fields are those of its TTS_Request).
type request struct {
	Text          string `json:"text"`
	TextLang      string `json:"text_lang"`
	RefAudioPath  string `json:"ref_audio_path"`
	PromptText    string `json:"prompt_text"`
	PromptLang    string `json:"prompt_lang"`
	TextSplit     string `json:"text_split_method"`
	MediaType     string `json:"media_type"`
	StreamingMode bool   `json:"streaming_mode"`
}

// Synthesize speaks the line in the voice and returns it as a wav.
//
// api_v2 reads the recording itself, from the path, and needs its transcript (prompt_text) for the model to follow the voice. A
// recording without a transcript is sent with an empty prompt_text: api_v2 then speaks without the transcript ("no prompt
// text" mode of the v2 models mam installs), which works with a weaker likeness; it is the Character's reference voice that
// says whether to add one. prompt_lang is sent either way, as api_v2 requires it.
func (c *Client) Synthesize(ctx context.Context, line domain.VoiceLine, voice localvoice.Voice) (audiodirector.Speech, error) {
	text := strings.TrimSpace(line.Text)
	if text == "" {
		return audiodirector.Speech{}, errors.New("gptsovits: the line has no text")
	}
	if !voice.Usable() {
		return audiodirector.Speech{}, fmt.Errorf("gptsovits: the reference voice of %q is not usable", localvoice.SpeakerOf(line))
	}
	lang := voice.Language
	if lang == "" {
		lang = DefaultTextLanguage
	}
	body, err := json.Marshal(request{
		Text: text, TextLang: c.cfg.TextLanguage, RefAudioPath: voice.Path, PromptText: voice.Text, PromptLang: lang,
		TextSplit: splitMethod, MediaType: "wav",
	})
	if err != nil {
		return audiodirector.Speech{}, fmt.Errorf("marshal request: %w", err)
	}
	select {
	case c.gpu <- struct{}{}:
		defer func() { <-c.gpu }()
	case <-ctx.Done():
		return audiodirector.Speech{}, ctx.Err()
	}
	base, err := c.server.Ensure(ctx)
	if err != nil {
		return audiodirector.Speech{}, err
	}
	data, err := c.post(ctx, strings.TrimRight(base, "/")+"/tts", body)
	if err != nil {
		return audiodirector.Speech{}, err
	}
	return audiodirector.Speech{Data: data, Ext: "wav"}, nil
}

// post sends the request and returns the wav of the answer.
func (c *Client) post(ctx context.Context, url string, body []byte) ([]byte, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.doer.Do(req)
	if err != nil {
		return nil, c.transportError(ctx, callCtx, err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxSpeechBytes+1))
	if err != nil {
		return nil, c.transportError(ctx, callCtx, err)
	}
	if res.StatusCode/100 != 2 {
		return nil, answerError(res.StatusCode, data)
	}
	if len(data) == 0 || len(data) > maxSpeechBytes || !isWav(data) {
		return nil, &audiodirector.ProviderError{Vendor: localvoice.Vendor, Status: res.StatusCode, Message: "the answer is empty, too large or not a wav"}
	}
	return data, nil
}

// transportError is a failure to reach the server or to read its answer: the caller's own cancellation stays as it is, the line
// limit is a timeout, and anything else (refused, reset) is the server having stopped.
func (c *Client) transportError(ctx, callCtx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if callCtx.Err() != nil {
		return &localvoice.UnavailableError{Kind: localvoice.KindTimeout, Detail: fmt.Sprintf("1 行の合成が %s で終わりませんでした", c.cfg.Timeout), Err: err}
	}
	err = httpclient.RedactURL(err)
	return &localvoice.UnavailableError{Kind: localvoice.KindProcess, Detail: err.Error(), Err: err}
}

// answerError is the error of a status that is not 2xx. api_v2 answers 400 with {"message": "tts failed", "Exception": "..."}; an
// exception of CUDA running out of memory is told from the other failures, as the user can do something about it.
func answerError(status int, body []byte) error {
	detail := httpclient.Snippet(body)
	if strings.Contains(strings.ToLower(detail), "out of memory") {
		return &localvoice.UnavailableError{Kind: localvoice.KindGPUMemory, Detail: detail, Err: &audiodirector.ProviderError{Vendor: localvoice.Vendor, Status: status, Message: detail}}
	}
	return &audiodirector.ProviderError{Vendor: localvoice.Vendor, Status: status, Message: detail}
}

// isWav reports whether data starts like a RIFF / WAVE file, which is what api_v2 answers with for media_type wav.
func isWav(data []byte) bool {
	return len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE"
}
