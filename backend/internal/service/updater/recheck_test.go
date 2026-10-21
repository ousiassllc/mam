package updater_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/service/updater"
)

// sequencedGate refuses from the (block+1)-th call on: it models work starting between the
// pre-download gate evaluation and the post-download one.
type sequencedGate struct {
	allowed int
	calls   int
}

func (g *sequencedGate) SafeToUpdate(context.Context) (bool, updater.BlockReason) {
	g.calls++
	if g.calls > g.allowed {
		return false, updater.BlockReason{Kind: updater.BlockJobs, Detail: "a job started"}
	}
	return true, updater.BlockReason{}
}

// A gate condition that becomes true while the installer downloads must stop the install: the
// result is not Ready and the status is Blocked.
func TestChecker_CheckForUpdate_GateReevaluatedAfterDownload(t *testing.T) {
	setVersion(t, "v0.1.0")
	checksums := fmt.Sprintf("%s  %s\n", installerChecksum(t), installerName)
	server := newGitHubMock(t, "v0.2.0", checksums)

	gate := &sequencedGate{allowed: 1} // a job starts during the download
	checker := newChecker(server, gate)

	result, err := checker.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if result.Ready {
		t.Fatal("Ready = true, want false when the gate fails after the download")
	}
	status := checker.Status()
	if !status.Blocked || status.BlockedKind != updater.BlockJobs || status.Ready {
		t.Fatalf("Status = %+v, want Blocked with BlockJobs and not Ready", status)
	}
	if gate.calls != 2 {
		t.Fatalf("gate evaluated %d times, want 2 (before and after the download)", gate.calls)
	}

	// The next check, with the job finished, is Ready.
	gate.allowed = 100
	result, err = checker.CheckForUpdate(context.Background())
	if err != nil || !result.Ready {
		t.Fatalf("CheckForUpdate (retry) = %+v, %v; want Ready", result, err)
	}
}
