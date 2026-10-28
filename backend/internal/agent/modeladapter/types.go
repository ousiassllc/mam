// Package modeladapter is the Model Adapter: the layer through which Agents generate images and
// videos (spec/spec.md §7.2, spec/architecture/model-adapters.md §3, §4). It turns the
// model-independent Canonical Shot Spec into the prompt and parameters of one video model, calls
// the model behind a circuit breaker with exponential backoff, routes to the alternative model when
// the primary is unavailable (§7) and limits concurrent calls per vendor according to the
// rate limits the vendor reports (spec/requirements/non-functional.md §4).
//
// Which model serves which purpose comes from Config (never from Agent code). The Reasoning
// LLM Adapter (package llm) is a separate layer.
package modeladapter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Input is an image handed to a model (a reference or a first / last frame).
type Input struct {
	Data     []byte
	MimeType string
}

// ImageRequest is one image generation. Inputs are reference images; a model that cannot use them
// generates from the prompt alone.
type ImageRequest struct {
	Prompt      string
	AspectRatio string // "16:9", "9:16", "1:1"
	Inputs      []Input
	// NegativePrompt is for a model that has the field (a ComfyUI workflow with a negative prompt); others ignore it.
	NegativePrompt string
	Seed           *int64 // for a model that takes one (SupportsSeed); nil lets the vendor choose
}

// ImageResult is a generated image.
type ImageResult struct {
	Data     []byte
	MimeType string
	// ActualCost is the cost in USD the vendor reported for the call; nil when it reports none.
	ActualCost *float64
}

func withSeed(body map[string]any, seed *int64) map[string]any {
	if seed != nil {
		body["seed"] = *seed
	}
	return body
}

// ImageBackend calls one image model. apiModel is the vendor's model identifier from Config.
// Failures of the vendor or the network MUST be *ProviderError so they can be classified.
type ImageBackend interface {
	GenerateImage(ctx context.Context, apiModel string, req ImageRequest) (ImageResult, error)
}

// VideoRequest is one video generation for one model.
type VideoRequest struct {
	Prompt         string
	NegativePrompt string
	// DurationSeconds is a length the model supports (Capabilities.Durations).
	DurationSeconds float64
	Resolution      string // "480p", "720p", "1080p"
	AspectRatio     string
	FirstFrame      *Input
	LastFrame       *Input
	References      []Input
	Seed            *int64 // for a model that takes one (SupportsSeed); nil lets the vendor choose
}

// VideoResult is a generated video file. The caller owns File and MUST store or discard it.
type VideoResult struct {
	File     Blob
	MimeType string
	// ActualCost is the cost in USD the vendor reported for the call; nil when it reports none.
	ActualCost *float64
}

// VideoBackend calls one video model: it submits the job, waits for it and downloads the file.
type VideoBackend interface {
	GenerateVideo(ctx context.Context, apiModel string, req VideoRequest) (VideoResult, error)
}

// ErrNoAPIKey means no API key is stored for the vendor.
var ErrNoAPIKey = errors.New("modeladapter: API key is not configured")

// ErrTimeout means a model call exceeded the vendor's timeout.
var ErrTimeout = errors.New("modeladapter: model call timed out")

// ErrTimeLimit means the model calls of a job together exceeded VideoJob.MaxTime.
var ErrTimeLimit = errors.New("modeladapter: the time limit of the job was exceeded")

// ErrCircuitOpen means the model was skipped because its circuit breaker is open. The error of a whole
// route matches it only when every model of the route was skipped.
var ErrCircuitOpen = errors.New("modeladapter: circuit breaker is open")

// ErrRejected means the vendor refused the request itself (a 4xx other than 429), for example a
// prompt that violates its content policy. Retrying or waiting does not help.
var ErrRejected = errors.New("modeladapter: the request was rejected")

// ProviderError is a failed call of a vendor. Status is the HTTP status, 0 for a network failure.
type ProviderError struct {
	Vendor     string
	Status     int
	Message    string
	RetryAfter time.Duration
	Err        error
}

func (e *ProviderError) Error() string {
	switch {
	case e.Status != 0:
		return fmt.Sprintf("modeladapter: %s returned HTTP %d: %s", e.Vendor, e.Status, e.Message)
	case e.Err != nil:
		return fmt.Sprintf("modeladapter: %s request failed: %v", e.Vendor, e.Err)
	}
	return fmt.Sprintf("modeladapter: %s failed: %s", e.Vendor, e.Message)
}

// Unwrap returns the underlying network error.
func (e *ProviderError) Unwrap() error { return e.Err }

// Is lets errors.Is(err, ErrRejected) recognise a refusal of the request itself.
func (e *ProviderError) Is(target error) bool {
	return target == ErrRejected && e.Status >= 400 && e.Status < 500 && e.Status != http.StatusTooManyRequests
}

// RateLimited reports whether err is (or wraps) a 429 answer.
func RateLimited(err error) bool {
	var pe *ProviderError
	return errors.As(err, &pe) && pe.Status == http.StatusTooManyRequests
}

// IsUnavailable reports whether err says the vendor is temporarily unusable: a timeout, a network
// failure, 429 or a 5xx. Only those count for the circuit breaker; a 4xx or a missing key is a
// problem of the request or the setup.
func IsUnavailable(err error) bool {
	if errors.Is(err, ErrTimeout) {
		return true
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe.Status == 0 || pe.Status == http.StatusTooManyRequests || pe.Status >= 500
	}
	return false
}

// ErrAfterSubmit marks an error that happened after the vendor accepted the job (while waiting
// for it or downloading its result).
var ErrAfterSubmit = errors.New("after the job was submitted")

type submittedError struct{ err error }

func (e *submittedError) Error() string { return e.err.Error() }
func (e *submittedError) Unwrap() error { return e.err }

// Is lets errors.Is(err, ErrAfterSubmit) recognise the marker; the wrapped error stays reachable
// through Unwrap, so it still counts for the circuit breaker.
func (e *submittedError) Is(target error) bool { return target == ErrAfterSubmit }

// AfterSubmit marks err as one that happened after the job was submitted (nil stays nil).
func AfterSubmit(err error) error {
	if err == nil {
		return nil
	}
	return &submittedError{err: err}
}

// retryable is unavailable without the cases where submitting the job again would be billed twice:
// after a timeout the job may still be running at the vendor, and after the vendor accepted the job
// (ErrAfterSubmit) it is running there already. The alternative model is used instead.
func retryable(err error) bool {
	return IsUnavailable(err) && !errors.Is(err, ErrTimeout) && !errors.Is(err, ErrAfterSubmit)
}

func retryAfter(err error) time.Duration {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe.RetryAfter
	}
	return 0
}

// Vendor names the account behind one or more models: the credential.Store key of its API key,
// and the unit of the circuit breaker and the concurrency limit.
type Vendor string

// Vendors.
const (
	VendorGoogle Vendor = "google"
	VendorOpenAI Vendor = "openai"
	VendorFlux   Vendor = "flux"
	VendorKling  Vendor = "kling"
	// VendorKling4 is Kling 4.0 on the new API (https://api-singapore.klingai.com, one API key as the Bearer token). Kling 3.0
	// stays on the old one (VendorKling: ACCESS_KEY:SECRET_KEY, JWT), so the two keys and base URLs cannot share a vendor.
	VendorKling4   Vendor = "kling4"
	VendorSeedance Vendor = "seedance"
	VendorRunway   Vendor = "runway"
)

// VendorOf returns the vendor of a model.
func VendorOf(m domain.ModelName) Vendor {
	switch m {
	case domain.ModelNanoBanana, domain.ModelNanoBananaLite, domain.ModelGeminiOmniFlash:
		return VendorGoogle
	case domain.ModelGPTImage2:
		return VendorOpenAI
	case domain.ModelFlux2:
		return VendorFlux
	case domain.ModelKling3:
		return VendorKling
	case domain.ModelKling4:
		return VendorKling4
	case domain.ModelSeedance2:
		return VendorSeedance
	case domain.ModelRunwayGen45:
		return VendorRunway
	case chatgptid.Model:
		return Vendor(chatgptid.Vendor)
	}
	if endpoint, ok := localcfg.EndpointOf(string(m)); ok {
		return Vendor(localcfg.VendorKey(endpoint))
	}
	return Vendor(m)
}

// Vendors lists every vendor that has a backend.
func Vendors() []Vendor {
	return []Vendor{VendorGoogle, VendorOpenAI, VendorFlux, VendorKling, VendorKling4, VendorSeedance, VendorRunway}
}

// ImageCost is the most one image of the model can cost (its dearest size): what a budget check holds.
func (c Config) ImageCost(m domain.ModelName) float64 { return c.ImageCostFor(m, "") }

// ImageCostFor is the estimated cost of one image of the aspect ratio ("" for the dearest size): the price the user set,
// else for GPT Image 2 the price of its quality profile at the size.
func (c Config) ImageCostFor(m domain.ModelName, aspect string) float64 {
	mc := c.ModelSettings(m)
	if m == domain.ModelGPTImage2 && mc.CostPerImage == 0 && gptimage.Priced(mc.Model) {
		return gptimage.Estimate(mc.Quality, aspect)
	}
	return mc.CostPerImage
}
