package localcfg

import "errors"

// EndpointField names the field of an endpoint a problem is about.
type EndpointField string

// The fields of an endpoint, as the keys of models.json spell them.
const (
	FieldName           EndpointField = "name"
	FieldKind           EndpointField = "kind"
	FieldBaseURL        EndpointField = "base_url"
	FieldMaxConcurrency EndpointField = "max_concurrency"
	FieldTimeoutSeconds EndpointField = "timeout_seconds"
)

// Limits of the numbers of an endpoint (0 is "the default" and always allowed).
const (
	MaxConcurrency    = 64
	MaxTimeoutSeconds = 24 * 3600
)

// Why a base_url is refused (CheckBaseURL). The text is for logs and the settings file's check; a screen words them itself.
var (
	ErrBaseURLShape     = errors.New("must be a URL such as http://127.0.0.1:8188")
	ErrBaseURLScheme    = errors.New("must start with http:// or https://")
	ErrBaseURLParts     = errors.New("must not contain a user name, query or fragment")
	ErrBaseURLLinkLocal = errors.New("link-local, unspecified and multicast addresses cannot be used")
)

// EndpointError is one problem of an endpoint (ValidateEndpoint): the field it is about and, for a base_url, the reason
// (one of the ErrBaseURL values, found with errors.Is). Error is the English line of the settings file's check.
type EndpointError struct {
	Field  EndpointField
	Reason error
	text   string
}

func (e *EndpointError) Error() string { return e.text }

// Unwrap is the reason of a refused base_url, nil for the other fields.
func (e *EndpointError) Unwrap() error { return e.Reason }
