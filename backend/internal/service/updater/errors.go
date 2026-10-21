package updater

import (
	"errors"
	"fmt"
)

// ErrorKind classifies why a check failed, so the Settings panel can tell
// the operator what went wrong without exposing the raw error text.
// The zero value means "no error".
type ErrorKind string

const (
	// ErrorNetwork means the GitHub API or an asset download could not be
	// reached, timed out, the transfer broke off, or GitHub answered with
	// a server-side error status.
	ErrorNetwork ErrorKind = "network"
	// ErrorRateLimit means GitHub rejected the release lookup or an asset
	// download for rate limiting.
	ErrorRateLimit ErrorKind = "rate_limit"
	// ErrorAccess means GitHub refused the release lookup or an asset download -
	// 401/403, or 404 on an asset (removed). A 404 on the release lookup
	// itself is not an error but Status.NoRelease.
	ErrorAccess ErrorKind = "access"
	// ErrorVerification means a downloaded asset was rejected - checksum
	// mismatch or missing entry, a missing/invalid checksums.txt.sig
	// signature, size limit, or a URL outside the
	// repository's release-download path.
	ErrorVerification ErrorKind = "verification"
	// ErrorRelease means the release was fetched but its content is unusable -
	// undecodable JSON, non-semver tag, or missing installer / checksums
	// asset.
	ErrorRelease ErrorKind = "release"
	// ErrorOther means anything else (e.g. local temp-file failures).
	ErrorOther ErrorKind = "other"
)

// kindedError tags err with its ErrorKind; Unwrap keeps errors.Is/As
// working through it.
type kindedError struct {
	kind ErrorKind
	err  error
}

func (e *kindedError) Error() string { return e.err.Error() }
func (e *kindedError) Unwrap() error { return e.err }

// Permanent implements the optional interface service/appupdate.Runner
// consults: a broken release, a rejected asset or a refused
// lookup stays broken until the release or the configuration changes, so
// retrying it every backoff step (and re-downloading the whole installer
// for a checksum mismatch) is wasted work; the next scheduled check covers
// the case that it was fixed meanwhile. Network, rate-limit and other
// errors may clear by themselves and are retried with backoff.
func (e *kindedError) Permanent() bool {
	switch e.kind {
	case ErrorAccess, ErrorVerification, ErrorRelease:
		return true
	default:
		return false
	}
}

func withKind(kind ErrorKind, err error) error {
	if err == nil {
		return nil
	}
	return &kindedError{kind: kind, err: err}
}

func kindErrorf(kind ErrorKind, format string, args ...any) error {
	return &kindedError{kind: kind, err: fmt.Errorf(format, args...)}
}

// errorKind returns the outermost ErrorKind err carries, ErrorOther when
// it carries none.
func errorKind(err error) ErrorKind {
	var ke *kindedError
	if errors.As(err, &ke) {
		return ke.kind
	}
	return ErrorOther
}
