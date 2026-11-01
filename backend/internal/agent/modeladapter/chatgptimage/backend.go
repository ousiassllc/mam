// Package chatgptimage is the UNOFFICIAL image backend that draws GPT Image 2 with the user's ChatGPT plan: it calls
// the Codex endpoint (https://chatgpt.com/backend-api/codex/responses) with the built-in image_generation tool, signed in
// with the Codex public client (package chatgpt, Image). It is not an OpenAI API: it can stop working without notice,
// the official ChatGPT plan usage preview lists image generation as unsupported, and every image uses the plan's limit.
// It is therefore off until the user turns it on and confirms the risks; while it is off no request is made.
//
// The shape follows github.com/AhmedAburady/imagine-cli (providers/openai/subscription.go) and openai/codex.
package chatgptimage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/aspectsize"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/catalog"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// CredentialSource gives the credentials of one image call, or chatgpt.ErrImageDisabled (without a request) while the
// image generation is off. *chatgpt.Service implements it.
type CredentialSource interface {
	ImageCredentials(ctx context.Context) (chatgpt.Credentials, error)
}

const (
	// DefaultBaseURL is the Codex backend.
	DefaultBaseURL = "https://chatgpt.com/backend-api/codex"
	// DefaultStall is how long the stream may be silent before the call is given up.
	DefaultStall = 180 * time.Second

	vendor = "chatgpt-subscription"
	// instructions is the minimal system prompt the endpoint needs to reliably call the tool.
	instructions = "You are an image generation assistant. Generate exactly one image for the user's request using the image_generation tool."
	// originator presents mam as the Codex client it signed in as: a foreign originator risks a rejection.
	originator     = "codex_cli_rs"
	userAgent      = "mam (codex_cli_rs)"
	maxStreamBytes = 64 << 20
)

// Options configure a Backend. The zero value selects the defaults.
type Options struct {
	BaseURL string
	// Driver pins the Codex model that hosts the image_generation tool call. Empty (the default) lets the backend choose
	// a model the ChatGPT account may use by itself (driver.go); a pinned model is used as it is, with no fallback.
	Driver string
	// ClientVersion is the Codex version sent to the model list ("" selects the version of the pinned Codex catalog).
	ClientVersion string
	Stall         time.Duration
	Client        httpclient.Doer
}

// Backend implements modeladapter.ImageBackend.
type Backend struct {
	source        CredentialSource
	baseURL       string
	pinned        string
	clientVersion string
	stall         time.Duration
	client        httpclient.Doer

	mu     sync.Mutex
	picked string // the driver that worked last; "" until one did, and again once it stopped working
}

var _ modeladapter.ImageBackend = (*Backend)(nil)

// New returns the backend over source.
func New(source CredentialSource, o Options) *Backend {
	if o.BaseURL == "" {
		o.BaseURL = DefaultBaseURL
	}
	if o.ClientVersion == "" {
		o.ClientVersion = catalog.Default().Version
	}
	if o.Stall <= 0 {
		o.Stall = DefaultStall
	}
	if o.Client == nil {
		o.Client = httpclient.New()
	}
	return &Backend{
		source: source, baseURL: strings.TrimRight(o.BaseURL, "/"), pinned: o.Driver, clientVersion: o.ClientVersion,
		stall: o.Stall, client: o.Client,
	}
}

// GenerateImage implements modeladapter.ImageBackend. apiModel is the image model of the tool ("gpt-image-2"). The Codex
// model that hosts the tool call is chosen by driverLoop.
func (b *Backend) GenerateImage(ctx context.Context, apiModel string, req modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	creds, err := b.source.ImageCredentials(ctx)
	if err != nil {
		return modeladapter.ImageResult{}, fmt.Errorf("chatgpt image generation: %w", err)
	}
	return b.driverLoop(ctx, creds, func(driver string) (modeladapter.ImageResult, error) {
		return b.generate(ctx, creds, driver, apiModel, req)
	})
}

// generate is one call of the Codex endpoint with driver as the hosting model.
func (b *Backend) generate(ctx context.Context, creds chatgpt.Credentials, driver, apiModel string, req modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	body, err := json.Marshal(map[string]any{
		"model":        driver,
		"instructions": instructions,
		"input":        []map[string]any{{"role": "user", "content": content(req)}},
		"tools":        []map[string]any{{"type": "image_generation", "model": apiModel, "size": aspectsize.OpenAI(req.AspectRatio), "output_format": "png"}},
		"tool_choice":  map[string]any{"type": "image_generation"},
		"stream":       true,
		"store":        false,
	})
	if err != nil {
		return modeladapter.ImageResult{}, fmt.Errorf("marshal %s request: %w", vendor, err)
	}
	r, err := b.newRequest(ctx, http.MethodPost, b.baseURL+"/responses", bytes.NewReader(body), creds)
	if err != nil {
		return modeladapter.ImageResult{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	r.Header.Set("OpenAI-Beta", "responses=experimental")
	r.Header.Set("session_id", uuid.NewString())

	res, err := b.client.Do(r)
	if err != nil {
		return modeladapter.ImageResult{}, &modeladapter.ProviderError{Vendor: vendor, Err: httpclient.RedactURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		return modeladapter.ImageResult{}, httpError(res, data)
	}
	stalled := newStallReader(res.Body, b.stall)
	defer stalled.stop()
	return parseStream(io.LimitReader(stalled, maxStreamBytes))
}

// newRequest is a request of the Codex endpoint signed with the ChatGPT credentials, as Codex signs its own.
func (b *Backend) newRequest(ctx context.Context, method, url string, body io.Reader, creds chatgpt.Credentials) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", vendor, err)
	}
	r.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	if creds.AccountID != "" {
		r.Header.Set("ChatGPT-Account-Id", creds.AccountID)
	}
	r.Header.Set("originator", originator)
	r.Header.Set("User-Agent", userAgent)
	return r, nil
}

func content(req modeladapter.ImageRequest) []map[string]any {
	parts := []map[string]any{{"type": "input_text", "text": req.Prompt}}
	for _, in := range req.Inputs {
		parts = append(parts, map[string]any{
			"type": "input_image", "image_url": "data:" + in.MimeType + ";base64," + base64.StdEncoding.EncodeToString(in.Data),
		})
	}
	return parts
}

// httpError maps a non-2xx answer. A Cloudflare challenge usually means the session is no longer accepted.
func httpError(res *http.Response, body []byte) error {
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(body, &env)
	msg := env.Error.Message
	if msg == "" {
		msg = env.Detail
	}
	if msg == "" {
		msg = httpclient.Snippet(body)
	}
	if res.StatusCode == http.StatusForbidden && bytes.Contains(bytes.ToLower(body), []byte("cloudflare")) {
		msg = "blocked by Cloudflare: sign in again from the settings screen"
	}
	return &modeladapter.ProviderError{
		Vendor: vendor, Status: res.StatusCode, Message: msg, RetryAfter: httpclient.ParseRetryAfter(res.Header.Get("Retry-After")),
	}
}

// event is the part of each streamed event that is read.
type event struct {
	Type string `json:"type"`
	Item *struct {
		Type         string `json:"type"`
		Result       string `json:"result"`
		OutputFormat string `json:"output_format"`
	} `json:"item"`
	Response *struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	} `json:"response"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// parseStream returns the first finished image_generation_call. A failed or incomplete response (a moderation stop
// arrives as incomplete) and a stream that ends with no image are errors.
func parseStream(r io.Reader) (modeladapter.ImageResult, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadString('\n')
		if data, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "data:"); ok {
			if res, done, derr := handle(strings.TrimSpace(data)); done {
				return res, derr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return modeladapter.ImageResult{}, &modeladapter.ProviderError{Vendor: vendor, Status: http.StatusOK, Message: "the stream ended with no image"}
			}
			return modeladapter.ImageResult{}, &modeladapter.ProviderError{Vendor: vendor, Err: err}
		}
	}
}

func handle(data string) (modeladapter.ImageResult, bool, error) {
	if data == "" || data == "[DONE]" {
		return modeladapter.ImageResult{}, false, nil
	}
	var ev event
	if json.Unmarshal([]byte(data), &ev) != nil {
		return modeladapter.ImageResult{}, false, nil
	}
	switch {
	case ev.Type == "response.output_item.done" && ev.Item != nil && ev.Item.Type == "image_generation_call" && ev.Item.Result != "":
		img, err := base64.StdEncoding.DecodeString(ev.Item.Result)
		if err != nil || len(img) == 0 {
			return modeladapter.ImageResult{}, true, &modeladapter.ProviderError{Vendor: vendor, Status: http.StatusOK, Message: "the image is not valid base64"}
		}
		zero := 0.0 // the plan pays, not an invoice: the ledger records one row at 0 USD
		return modeladapter.ImageResult{Data: img, MimeType: mimeOf(ev.Item.OutputFormat), ActualCost: &zero}, true, nil
	case ev.Error != nil && ev.Error.Message != "":
		return modeladapter.ImageResult{}, true, &modeladapter.ProviderError{Vendor: vendor, Status: http.StatusBadRequest, Message: ev.Error.Message}
	case ev.Response != nil && ev.Response.Error != nil && ev.Response.Error.Message != "":
		return modeladapter.ImageResult{}, true, &modeladapter.ProviderError{Vendor: vendor, Status: http.StatusBadRequest, Message: ev.Response.Error.Message}
	case ev.Response != nil && ev.Response.IncompleteDetails != nil && ev.Response.IncompleteDetails.Reason != "":
		return modeladapter.ImageResult{}, true, &modeladapter.ProviderError{Vendor: vendor, Status: http.StatusBadRequest, Message: "image generation stopped: " + ev.Response.IncompleteDetails.Reason}
	}
	return modeladapter.ImageResult{}, false, nil
}

func mimeOf(format string) string {
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	}
	return "image/png"
}

// stallReader closes the body after idle of silence, which unblocks a read that would hang for ever.
type stallReader struct {
	rc      io.ReadCloser
	timer   *time.Timer
	idle    time.Duration
	stalled atomic.Bool
}

// ErrStalled is a stream that stayed silent for the stall time.
var ErrStalled = errors.New("the stream stalled: no data for the stall time")

func newStallReader(rc io.ReadCloser, idle time.Duration) *stallReader {
	s := &stallReader{rc: rc, idle: idle}
	s.timer = time.AfterFunc(idle, func() {
		s.stalled.Store(true)
		_ = rc.Close()
	})
	return s
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.rc.Read(p)
	if n > 0 {
		s.timer.Reset(s.idle)
	}
	if err != nil && s.stalled.Load() {
		return n, ErrStalled
	}
	return n, err
}

func (s *stallReader) stop() { s.timer.Stop() }
