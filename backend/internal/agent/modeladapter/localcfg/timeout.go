package localcfg

import "time"

// CallLimit is the longest one call to the endpoint may take, in seconds. For a ComfyUI endpoint it is the
// ceiling MaxTimeoutSeconds, which only stops a runaway (a server that reports progress for ever, or a job that
// waits in the server's queue for ever): what ends a job that is stuck is StallLimit. For any other kind the
// timeout is the limit of the whole call.
func (e Endpoint) CallLimit() int {
	if e.Kind == KindComfyUI {
		return MaxTimeoutSeconds
	}
	return e.Timeout()
}

// StallLimit is how long a ComfyUI job may show no sign of progress before it is cancelled: the endpoint's
// timeout. 0 for any other kind (its timeout bounds the whole call, see CallLimit).
func (e Endpoint) StallLimit() time.Duration {
	if e.Kind != KindComfyUI {
		return 0
	}
	return time.Duration(e.Timeout()) * time.Second
}

// TimeoutError is the error of a call to an endpoint the user registered that ran past the endpoint's limit: the call
// itself (After is MaxTimeoutSeconds for ComfyUI, the timeout for any other kind), or, when Stalled, a ComfyUI job that
// showed no progress for After (the endpoint's timeout).
// It wraps the Model Adapter's own timeout error, so errors.Is(err, modeladapter.ErrTimeout) still holds; what it adds
// is that the call was local, where nothing is billed per call and the job can be stopped on the server.
type TimeoutError struct {
	After   time.Duration
	Stalled bool
	Err     error
}

func (e *TimeoutError) Error() string { return e.Err.Error() }
func (e *TimeoutError) Unwrap() error { return e.Err }

// MarkTimeout wraps err, the timeout of a call to the vendor, as a *TimeoutError when the vendor is a registered endpoint
// (see IsVendorKey); any other vendor's err is returned as it is.
func MarkTimeout(vendor string, after time.Duration, err error) error {
	if !IsVendorKey(vendor) {
		return err
	}
	return &TimeoutError{After: after, Err: err}
}

// MarkStall wraps err, the end of a ComfyUI job that showed no progress for after, as a *TimeoutError.
func MarkStall(after time.Duration, err error) error {
	return &TimeoutError{After: after, Stalled: true, Err: err}
}
