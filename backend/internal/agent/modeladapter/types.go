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
}

// ImageResult is a generated image.
type ImageResult struct {
	Data     []byte
	MimeType string
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
}

// VideoResult is a generated video file.
type VideoResult struct {
	Data     []byte
	MimeType string
}

// VideoBackend calls one video model: it submits the job, waits for it and downloads the file.
type VideoBackend interface {
	GenerateVideo(ctx context.Context, apiModel string, req VideoRequest) (VideoResult, error)
}

// ErrNoAPIKey means no API key is stored for the vendor.
var ErrNoAPIKey = errors.New("modeladapter: API key is not configured")

// ErrTimeout means a model call exceeded the vendor's timeout.
var ErrTimeout = errors.New("modeladapter: model call timed out")

// ErrCircuitOpen means every model of the route was skipped because its circuit breaker is open.
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

// unavailable reports whether err says the vendor is temporarily unusable: a timeout, a network
// failure, 429 or a 5xx. Only those count for the circuit breaker; a 4xx or a missing key is a
// problem of the request or the setup.
func unavailable(err error) bool {
	if errors.Is(err, ErrTimeout) {
		return true
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe.Status == 0 || pe.Status == http.StatusTooManyRequests || pe.Status >= 500
	}
	return false
}

// retryable is unavailable without timeouts: after a timeout the job may still be running (and
// billed) at the vendor, so it is not submitted a second time; the alternative model is used instead.
func retryable(err error) bool { return unavailable(err) && !errors.Is(err, ErrTimeout) }

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
	VendorGoogle   Vendor = "google"
	VendorOpenAI   Vendor = "openai"
	VendorFlux     Vendor = "flux"
	VendorKling    Vendor = "kling"
	VendorSeedance Vendor = "seedance"
)

// VendorOf returns the vendor of a model.
func VendorOf(m domain.ModelName) Vendor {
	switch m {
	case domain.ModelImagen4, domain.ModelNanoBanana:
		return VendorGoogle
	case domain.ModelGPTImage2:
		return VendorOpenAI
	case domain.ModelFlux2:
		return VendorFlux
	case domain.ModelKling3:
		return VendorKling
	case domain.ModelSeedance2:
		return VendorSeedance
	}
	return Vendor(m)
}

// Vendors lists every vendor that has a backend.
func Vendors() []Vendor {
	return []Vendor{VendorGoogle, VendorOpenAI, VendorFlux, VendorKling, VendorSeedance}
}
