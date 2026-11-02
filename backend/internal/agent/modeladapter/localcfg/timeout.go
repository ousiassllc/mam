package localcfg

import "time"

// TimeoutError is the error of a call to an endpoint the user registered that ran past the endpoint's timeout (After).
// It wraps the Model Adapter's own timeout error, so errors.Is(err, modeladapter.ErrTimeout) still holds; what it adds
// is that the call was local, where nothing is billed per call and the job can be stopped on the server.
type TimeoutError struct {
	After time.Duration
	Err   error
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
