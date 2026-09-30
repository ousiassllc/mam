package domain

import "time"

// VersionEntry is one saved version of something that can be restored (F-28): a Shot's
// prompt_version, a world_state_version or a branch_version.
type VersionEntry struct {
	Number    int
	Summary   string
	CreatedAt time.Time
}
