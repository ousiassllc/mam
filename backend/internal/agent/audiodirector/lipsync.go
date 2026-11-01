package audiodirector

import (
	"context"
	"errors"
)

// SyncCredentialProvider is the credential.Store key of the Sync Labs API key (model identifier
// sync-labs of spec/architecture/model-adapters.md §6).
const SyncCredentialProvider = "synclabs"

// ErrNoSyncLabsKey means no Sync Labs API key is stored.
var ErrNoSyncLabsKey = errors.New("audiodirector: Sync Labs API key is not configured")

// ChargedError wraps the failure of a Lip Sync that Sync Labs had already accepted: the vendor
// runs (and bills) the generation, so the caller records its cost although there is no video. A
// failure before the job exists (no key, create refused) and a FAILED / REJECTED generation are
// not charged and are returned unwrapped.
type ChargedError struct{ Err error }

func (e *ChargedError) Error() string { return e.Err.Error() }
func (e *ChargedError) Unwrap() error { return e.Err }

// LipSyncRequest is one Lip Sync: the video whose mouth is redone and the speech it must follow.
type LipSyncRequest struct {
	// Model is the Sync Labs model ("lipsync-2", "sync-3", ...); the local provider has none.
	Model string
	// Seconds is the length of the video (the local provider's time limit follows it).
	Seconds float64
	Video   Sample
	Audio   Sample
}

// LipSyncer redoes the mouth of a video to a speech and returns the new video (mp4).
// *synclabs.Client implements it (and *musetalk.Client, the local one, which never charges). A failure after Sync Labs accepted the job
// is a *ChargedError.
type LipSyncer interface {
	Sync(ctx context.Context, req LipSyncRequest) ([]byte, error)
}
