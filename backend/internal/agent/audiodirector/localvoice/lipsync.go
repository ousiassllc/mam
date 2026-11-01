package localvoice

import "errors"

// LipSyncProvider is who redoes the mouths of a run (MAX QUALITY only).
type LipSyncProvider string

// The lip-sync providers. The zero value is Sync Labs: a run that does not choose behaves as it did before the local one existed.
const (
	LipSyncSyncLabs LipSyncProvider = "synclabs"
	LipSyncLocal    LipSyncProvider = "local"
)

// Valid reports whether p is a lip-sync provider of the Audio Director ("" selects Sync Labs).
func (p LipSyncProvider) Valid() bool { return p == "" || p == LipSyncSyncLabs || p == LipSyncLocal }

// InvalidProvider names the option of a run that is not a provider of the Audio Director ("voice_provider" or "lip_sync_provider"), and
// is "" when both are.
func InvalidProvider(voice Provider, lipSync LipSyncProvider) string {
	switch {
	case !voice.Valid():
		return "voice_provider"
	case !lipSync.Valid():
		return "lip_sync_provider"
	}
	return ""
}

// ErrUnknownLipSyncProvider is a run that names a lip-sync provider the Audio Director does not have.
var ErrUnknownLipSyncProvider = errors.New("audiodirector: unknown lip sync provider")

// LipSyncReason is the sentence for the user of the failure of a local lip sync, as it is listed in Skipped: Reason of the error
// (told as the lip sync whatever its Vendor) when it is an *UnavailableError, else its text.
func LipSyncReason(err error) string {
	var un *UnavailableError
	if !errors.As(err, &un) {
		return err.Error()
	}
	lip := *un
	lip.Vendor = VendorMuseTalk
	return Reason(&lip)
}
