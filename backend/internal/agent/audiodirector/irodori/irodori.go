// Package irodori is the second local voice of the Audio Director: it speaks a line of the script with Irodori-TTS, the Japanese speech model
// of Aratako, through its OpenAI-compatible server (Irodori-TTS-Server behind mam's own launcher, which answers only the caller that has the
// token of the start) that mam installs, starts and stops itself (package localruntime), in the voice of the Character's reference recording
// (spec/architecture/model-adapters-m3.md §14.11). It implements audiodirector.LocalSynthesizer, like package gptsovits, and shares with it
// the choice of provider, the reference voice, the server and the typed errors (package localvoice).
package irodori

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// Defaults of Config and the facts of the server's API.
const (
	// DefaultTimeout bounds the speaking of one line (the server itself waits up to 300 seconds for a synthesis slot; this is the backstop
	// against a server that took the request and never answers).
	DefaultTimeout = 5 * time.Minute
	// modelName is the model id the server accepts (its IRODORI_MODEL_NAME default).
	modelName = "irodori-tts"
	// maxSpeechBytes bounds one generated audio file: a minute of 48 kHz 16-bit wav is under 6 MB, and the longest line is a few minutes.
	maxSpeechBytes = 64 << 20
	// tokenHeader carries the token of this start of the server: mam's launcher answers /v1/audio/speech only with it.
	tokenHeader = "X-Mam-Token" //nolint:gosec // the name of a header, not a credential
	speechPath  = "/v1/audio/speech"
)

// Config is the setting of the client; zero fields take the defaults.
type Config struct {
	// Timeout bounds one line: the call to the server, not its start.
	Timeout time.Duration
}

// Client speaks lines with the Irodori-TTS server. The GPU is one: the lines of all runs are spoken one after another.
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
	if doer == nil {
		doer = httpclient.New()
	}
	return &Client{cfg: cfg, server: server, doer: doer, gpu: make(chan struct{}, 1)}
}

// Prepare starts the server when it is stopped and waits until it answers (audiodirector.LocalSynthesizer). A server that cannot be used is an
// *localvoice.UnavailableError.
func (c *Client) Prepare(ctx context.Context) error {
	_, _, err := c.server.Ensure(ctx)
	return err
}

// CheckVoices refuses, before anything is started or paid for, a script whose speakers have a reference voice that Irodori-TTS cannot be given
// (CheckAll). It is what makes the Client a localvoice.VoiceChecker.
func (c *Client) CheckVoices(voices map[string]localvoice.Voice) error { return CheckAll(voices) }

// request is the body of POST /v1/audio/speech of the server. The reference voice is given by the path of its recording (irodori.ref_wav): the
// server runs on this machine and reads the file itself.
type request struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	ResponseFormat string  `json:"response_format"`
	Irodori        options `json:"irodori"`
}

// options are the Irodori-specific fields of a request.
type options struct {
	RefWav  string `json:"ref_wav"`
	Caption string `json:"caption,omitempty"`
}

// Synthesize speaks the line in the voice and returns it as a wav (48 kHz). The text is the line with its stage directions turned into emoji
// (Emojify), and the Voice Design caption is built from the Character's settings and the Scene Emotion of the line (Caption). The reference
// voice is checked before anything is sent (CheckReferences).
func (c *Client) Synthesize(ctx context.Context, line domain.VoiceLine, voice localvoice.Voice) (audiodirector.Speech, error) {
	text := strings.TrimSpace(Emojify(strings.TrimSpace(line.Text)))
	if text == "" {
		return audiodirector.Speech{}, errors.New("irodori: the line has no text")
	}
	speaker := localvoice.SpeakerOf(line)
	if err := CheckReferences(speaker, voice); err != nil {
		return audiodirector.Speech{}, err
	}
	// The server is another process with its own working directory: a relative path (a relative data directory) would not be found.
	refPath, err := filepath.Abs(voice.Path)
	if err != nil {
		return audiodirector.Speech{}, fmt.Errorf("irodori: the path of the reference voice of %q: %w", speaker, err)
	}
	body, err := json.Marshal(request{
		Model: modelName, Input: text, ResponseFormat: "wav",
		Irodori: options{RefWav: refPath, Caption: Caption(voice.Profile, localvoice.EmotionOf(ctx), true)},
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
	base, token, err := c.server.Ensure(ctx)
	if err != nil {
		return audiodirector.Speech{}, err
	}
	data, err := c.post(ctx, strings.TrimRight(base, "/")+speechPath, token, body)
	if err != nil {
		return audiodirector.Speech{}, err
	}
	return audiodirector.Speech{Data: data, Ext: "wav"}, nil
}

// post sends the request and returns the wav of the answer.
func (c *Client) post(ctx context.Context, url, token string, body []byte) ([]byte, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(tokenHeader, token)
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
	if len(data) == 0 || len(data) > maxSpeechBytes || !localvoice.IsWav(data) {
		return nil, &audiodirector.ProviderError{Vendor: localvoice.VendorIrodori, Status: res.StatusCode, Message: "the answer is empty, too large or not a wav"}
	}
	return data, nil
}

// transportError is a failure to reach the server or to read its answer: the caller's own cancellation stays as it is, the line limit is a
// timeout, and anything else (refused, reset) is the server having stopped.
func (c *Client) transportError(ctx, callCtx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if callCtx.Err() != nil {
		return &localvoice.UnavailableError{Kind: localvoice.KindTimeout, Vendor: localvoice.VendorIrodori, Detail: fmt.Sprintf("1 行の合成が %s で終わりませんでした", c.cfg.Timeout), Err: err}
	}
	err = httpclient.RedactURL(err)
	return &localvoice.UnavailableError{Kind: localvoice.KindProcess, Vendor: localvoice.VendorIrodori, Detail: err.Error(), Err: err}
}

// answerError is the error of a status that is not 2xx. The server answers {"error": {"message": ..., "type": ...}} (the OpenAI shape); an
// exception of CUDA running out of memory is told from the other failures, as the user can do something about it. A 503 is the model still
// loading (or no synthesis slot in time): it is the server being busy, not a fault of the line.
func answerError(status int, body []byte) error {
	detail := httpclient.Snippet(body)
	var shaped struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &shaped) == nil && shaped.Error.Message != "" {
		detail = httpclient.Snippet([]byte(shaped.Error.Message))
	}
	perr := &audiodirector.ProviderError{Vendor: localvoice.VendorIrodori, Status: status, Message: detail}
	switch {
	case strings.Contains(strings.ToLower(detail), "out of memory"):
		return &localvoice.UnavailableError{Kind: localvoice.KindGPUMemory, Vendor: localvoice.VendorIrodori, Detail: detail, Err: perr}
	case status == http.StatusServiceUnavailable:
		return &localvoice.UnavailableError{Kind: localvoice.KindTimeout, Vendor: localvoice.VendorIrodori, Detail: detail, Err: perr}
	}
	return perr
}
