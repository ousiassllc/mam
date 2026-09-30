package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// BranchPointType is what a Branch starts from (F-17, spec/spec.md §16). The values are the
// branch_point_type of the branch table.
type BranchPointType string

// Branch point types.
const (
	// BranchAtScene branches at the Scene of a Shot.
	BranchAtScene BranchPointType = "scene"
	// BranchAtShot branches after a Shot.
	BranchAtShot BranchPointType = "shot"
	// BranchAtFrame branches at a moment inside a Shot (Branch.PointOffsetSeconds).
	BranchAtFrame BranchPointType = "frame"
)

// BranchPointTypes lists the types in display order.
func BranchPointTypes() []BranchPointType {
	return []BranchPointType{BranchAtScene, BranchAtShot, BranchAtFrame}
}

// Valid reports whether t is a branch point type.
func (t BranchPointType) Valid() bool {
	for _, v := range BranchPointTypes() {
		if t == v {
			return true
		}
	}
	return false
}

// MaxBranchNameRunes bounds the name of a Branch.
const MaxBranchNameRunes = 100

// MaxBranchesPerRequest bounds how many Branches one direction ("別展開を3つ作って") creates.
const MaxBranchesPerRequest = 5

// Branch is a row of branch: another development of the story that starts at a Branch Point and
// keeps its own World State and derived tree of Scenes.
type Branch struct {
	ID        string
	ProjectID string
	// ParentID is the Branch the Branch Point lies in; "" when it lies in the trunk.
	ParentID  string
	PointType BranchPointType
	// PointSceneID is set for BranchAtScene, PointShotID for BranchAtShot and BranchAtFrame.
	PointSceneID string
	PointShotID  string
	// PointSceneNumber and PointShotNumber are the 1-based numbers of the Scene and the Shot of the
	// point in the series they lie in (PointShotNumber is 0 for BranchAtScene).
	PointSceneNumber, PointShotNumber int
	// PointOffsetSeconds is the position inside the Shot of a BranchAtFrame.
	PointOffsetSeconds float64
	Name               string
	// WorldStateVersionID is the world_state_version the Branch refers to: the Snapshot taken when
	// it was created, until the reference is updated.
	WorldStateVersionID string
	// WorldStateVersion is the number of that version in the Branch's own series (0 when none).
	WorldStateVersion int
	// Version is the number of the latest branch_version.
	Version   int
	CreatedAt time.Time
}

// BranchVersion is a row of branch_version: one state of the Branch itself.
type BranchVersion struct {
	ID                  string
	BranchID            string
	Number              int
	Name                string
	WorldStateVersionID string
	// WorldStateVersion is the number of the referenced world_state_version (0 when none).
	WorldStateVersion int
	CreatedAt         time.Time
}

// PointText says where the Branch starts, in the numbers of the series it hangs from ("Scene 2 / Shot 1 の後").
func (b Branch) PointText() string {
	switch b.PointType {
	case BranchAtScene:
		return fmt.Sprintf("Scene %d の頭", b.PointSceneNumber)
	case BranchAtFrame:
		return fmt.Sprintf("Scene %d / Shot %d の %.1f 秒", b.PointSceneNumber, b.PointShotNumber, b.PointOffsetSeconds)
	}
	return fmt.Sprintf("Scene %d / Shot %d の後", b.PointSceneNumber, b.PointShotNumber)
}

// CleanBranchName trims a Branch name and reports whether it is usable: not empty and at most
// MaxBranchNameRunes characters.
func CleanBranchName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	return name, n > 0 && n <= MaxBranchNameRunes
}

// TruncateBranchName shortens a name (a direction of the user or of the AI) to MaxBranchNameRunes.
func TruncateBranchName(name string) string {
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > MaxBranchNameRunes {
		return string(r[:MaxBranchNameRunes])
	}
	return name
}
