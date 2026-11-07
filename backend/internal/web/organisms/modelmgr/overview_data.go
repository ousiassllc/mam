package modelmgr

import (
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/mmcat"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// OverviewID is the overview (GET /model-manager): it polls itself while an install is running anywhere.
const OverviewID = "mm-overview"

// The data-state of the summary of a category: the words say it, the state is what the tests and the tone follow. The engine's line carries the phase of its card
// (view.EnginePhase: unsupported, readonly, installing, broken, missing, installed) and the face-check line the state of its section (installed, unverified, missing).
const (
	StateEmpty      = "empty"
	StatePartial    = "partial"
	StateFull       = "full"
	StateInstalling = "installing"
	StateAttention  = "attention"
	// StateReady is the line of "ローカル LLM": the server the calls go to can be used.
	StateReady = "ready"
)

// CategoryCard is the card of a category on the overview: where it leads and how it stands, in words (what is read of the category now: nothing is stored).
type CategoryCard struct {
	mmcat.Category
	Summary string
	State   string
}

// Overview is GET /model-manager: one card for each category that this build has, in the order of mmcat.All.
type Overview struct {
	Cards []CategoryCard
	// Installing says an install is running in any category: the overview asks for itself again.
	Installing bool
	// UpdatedAt is the time of day it was read ("15:04:05"), beside the refresh button.
	UpdatedAt string
}

// overviewURL is what the overview and its refresh button read.
func overviewURL() string { return paths.ModelManagerPart("overview") }
