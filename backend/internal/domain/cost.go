package domain

import "fmt"

// CostTotals is the sum of cost_ledger_entry rows in USD. Estimated is the estimate recorded when the
// generation ran. Actual is the sum of the costs the vendors reported (cost_ledger_entry.actual_cost);
// ActualEntries of the Entries have one, so Actual is complete only when both are equal.
type CostTotals struct {
	Estimated     float64
	Actual        float64
	Entries       int
	ActualEntries int
}

// ActualKnown reports whether every entry has a reported actual cost (and there is at least one).
func (c CostTotals) ActualKnown() bool { return c.Entries > 0 && c.ActualEntries == c.Entries }

// ShotCost is the cost of one Shot.
type ShotCost struct {
	ShotID     string
	OrderIndex int
	Objective  string
	Totals     CostTotals
}

// SceneCost is the cost of one Scene with its Shots.
type SceneCost struct {
	SceneID string
	// BranchID and BranchName are empty for a Scene of the trunk; a Branch numbers its own Scenes from the first.
	BranchID   string
	BranchName string
	OrderIndex int
	Title      string
	Totals     CostTotals
	Shots      []ShotCost
}

// CostSummary is the cost view of a Project by Shot, Scene and Project (spec/requirements/non-functional.md §6).
type CostSummary struct {
	Project CostTotals
	Scenes  []SceneCost
	// Unassigned is the cost that belongs to no Shot (for example, entries of deleted Shots).
	Unassigned CostTotals
	// ProjectCeiling and ShotCeiling are the budget ceilings in USD; nil means none.
	ProjectCeiling, ShotCeiling *float64
}

// StoredTake is a Take whose video file is stored, with the size of the file.
type StoredTake struct {
	TakeID     string
	ShotID     string
	SceneTitle string
	SceneIndex int // 0-based order of the Scene
	ShotIndex  int // 0-based order of the Shot in its Scene
	Status     TakeStatus
	ModelName  string
	SizeBytes  int64
	// Unneeded: archiving the Take loses no choice (it is Rejected, or another Take of its Shot is Selected, and no
	// Edit Timeline or Lip Sync uses it). Archivable: it may be archived (not Selected, no Timeline uses it); Unneeded implies it.
	Unneeded, Archivable bool
}

// StorageSummary is the disk view of a Project (spec/requirements/non-functional.md §4).
type StorageSummary struct {
	// Takes lists the stored Takes, the unneeded ones first.
	Takes []StoredTake
	// AssetBytes is the size of every file the Project owns (uploads, references, Take videos).
	AssetBytes int64
	// FreeBytes is the free space of the volume that holds the assets directory; FreeKnown is false when it could not be read.
	FreeBytes uint64
	FreeKnown bool
}

// UnneededTakes returns how many stored Takes are unneeded and their total size.
func (s StorageSummary) UnneededTakes() (count int, bytes int64) {
	for _, t := range s.Takes {
		if t.Unneeded {
			count++
			bytes += t.SizeBytes
		}
	}
	return count, bytes
}

// Add returns the sum of two totals.
func (c CostTotals) Add(o CostTotals) CostTotals {
	return CostTotals{
		Estimated: c.Estimated + o.Estimated, Actual: c.Actual + o.Actual,
		Entries: c.Entries + o.Entries, ActualEntries: c.ActualEntries + o.ActualEntries,
	}
}

// FormatBytes renders a size for people: "12.3 MB" (binary units, one decimal).
func FormatBytes[T ~int64 | ~uint64](n T) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := float64(unit), 0
	for v := float64(n) / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/div, "KMGTP"[exp])
}
