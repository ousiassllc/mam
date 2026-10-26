// Package localcfg is the settings schema of the local and alternative image / video models: the
// endpoints a user registered (a ComfyUI server, an OpenAI-compatible image server) and the models
// defined on them (spec/architecture/local-models.md). It is the leaf the Model Adapter's Config holds
// (the keys "endpoints" and "local_models" of models.json) and the backends and the settings service
// read; it depends on nothing of the application but the standard library.
//
// A model is identified, in take.model_name and cost_ledger_entry.provider, by
// "<kind>:<endpoint>/<name>" (ID). Endpoint and model names are slugs and never change after creation.
package localcfg

import (
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
)

// Kind is the protocol of an endpoint.
type Kind string

// Endpoint kinds.
const (
	// KindComfyUI is a ComfyUI server: it runs a workflow (image and video).
	KindComfyUI Kind = "comfyui"
	// KindOpenAICompat is an OpenAI-compatible image API (/v1/images/generations and /v1/images/edits).
	KindOpenAICompat Kind = "openai-compat"
)

// Kinds lists the endpoint kinds.
func Kinds() []Kind { return []Kind{KindComfyUI, KindOpenAICompat} }

// Valid reports whether k is a known endpoint kind.
func (k Kind) Valid() bool { return k == KindComfyUI || k == KindOpenAICompat }

// Media is what a model generates.
type Media string

// Media kinds.
const (
	MediaImage Media = "image"
	MediaVideo Media = "video"
)

// Defaults of an endpoint that leaves a number out. One GPU rarely runs two jobs at once, and a video
// takes minutes to tens of minutes on a local GPU.
const (
	DefaultConcurrency    = 1
	DefaultTimeoutComfyUI = 3600
	DefaultTimeoutCompat  = 900
)

// Endpoint is a server the user registered. BaseURL may be plain http and may name localhost, a LAN
// address or a tailnet address (100.x.y.z or a MagicDNS name).
type Endpoint struct {
	Kind    Kind   `json:"kind"`
	BaseURL string `json:"base_url"`
	// MaxConcurrency is the most calls in flight at once (0: DefaultConcurrency).
	MaxConcurrency int `json:"max_concurrency,omitempty"`
	// TimeoutSeconds bounds one call (0: 3600 for ComfyUI, 900 for an OpenAI-compatible server).
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// Concurrency is MaxConcurrency with the default applied.
func (e Endpoint) Concurrency() int {
	if e.MaxConcurrency <= 0 {
		return DefaultConcurrency
	}
	return e.MaxConcurrency
}

// Timeout is TimeoutSeconds with the default applied.
func (e Endpoint) Timeout() int {
	switch {
	case e.TimeoutSeconds > 0:
		return e.TimeoutSeconds
	case e.Kind == KindComfyUI:
		return DefaultTimeoutComfyUI
	}
	return DefaultTimeoutCompat
}

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// ValidName reports whether s can be an endpoint or model name: lower-case letters, digits, "." "_" "-",
// at most 63 characters, never ":" or "/" (they separate the parts of an ID).
func ValidName(s string) bool { return namePattern.MatchString(s) }

// ID is the identifier of a model in take.model_name and cost_ledger_entry.provider.
func ID(kind Kind, endpoint, name string) string { return string(kind) + ":" + endpoint + "/" + name }

// Key is the key of a model in the "local_models" object.
func Key(endpoint, name string) string { return endpoint + "/" + name }

// ParseID splits the identifier of a local model. ok is false for a built-in identifier and for
// anything that is not "<kind>:<endpoint>/<name>" with valid names.
func ParseID(id string) (kind Kind, endpoint, name string, ok bool) {
	k, rest, found := strings.Cut(id, ":")
	if !found || !Kind(k).Valid() {
		return "", "", "", false
	}
	endpoint, name, found = strings.Cut(rest, "/")
	if !found || !ValidName(endpoint) || !ValidName(name) {
		return "", "", "", false
	}
	return Kind(k), endpoint, name, true
}

// SplitKey splits a key of "local_models" into the endpoint and the model name.
func SplitKey(key string) (endpoint, name string, ok bool) {
	endpoint, name, ok = strings.Cut(key, "/")
	return endpoint, name, ok && ValidName(endpoint) && ValidName(name)
}

// VendorKey is the vendor of an endpoint: the unit of the circuit breaker and the concurrency limit of
// the Model Adapter, and the credential.Store key of the endpoint's API key.
func VendorKey(endpoint string) string { return "endpoint:" + endpoint }

// ValidateEndpoint checks one endpoint of the settings; a nil error means it can be used. Each problem is an
// *EndpointError.
func ValidateEndpoint(name string, e Endpoint) []error {
	var errs []error
	where := "endpoints " + name + ": "
	problem := func(field EndpointField, reason error, text string) {
		errs = append(errs, &EndpointError{Field: field, Reason: reason, text: where + text})
	}
	if !ValidName(name) {
		problem(FieldName, nil, "the name must be 1-63 characters of a-z, 0-9, '.', '_' or '-'")
	}
	if !e.Kind.Valid() {
		problem(FieldKind, nil, fmt.Sprintf("kind %q is not comfyui or openai-compat", e.Kind))
	}
	if err := CheckBaseURL(e.BaseURL); err != nil {
		problem(FieldBaseURL, err, "base_url: "+err.Error())
	}
	if e.MaxConcurrency < 0 || e.MaxConcurrency > MaxConcurrency {
		problem(FieldMaxConcurrency, nil, fmt.Sprintf("max_concurrency must be within 0-%d", MaxConcurrency))
	}
	if e.TimeoutSeconds < 0 || e.TimeoutSeconds > MaxTimeoutSeconds {
		problem(FieldTimeoutSeconds, nil, fmt.Sprintf("timeout_seconds must be within 0-%d", MaxTimeoutSeconds))
	}
	return errs
}

// CheckBaseURL accepts an http or https URL with a host and no user name, query or fragment; the refusal is one of the
// ErrBaseURL values. A literal link-local address is refused here for a clear message; the connection itself is guarded
// at connect time (safefetch.NewEndpointClient), which also catches a name that resolves to one.
func CheckBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ErrBaseURLShape
	}
	switch {
	case u.Scheme != "http" && u.Scheme != "https":
		return ErrBaseURLScheme
	case u.User != nil || u.RawQuery != "" || u.Fragment != "":
		return ErrBaseURLParts
	}
	if ip, err := netip.ParseAddr(strings.Trim(u.Hostname(), "[]")); err == nil {
		if ip = ip.Unmap(); ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
			return ErrBaseURLLinkLocal
		}
	}
	return nil
}
