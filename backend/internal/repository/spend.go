package repository

import (
	"context"
	"fmt"
	"math"
	"sync"
)

// inflightSpend is the estimated cost of generations that have been ordered but whose cost is not
// in the cost ledger yet (the ledger is written when the vendor returns the output, minutes later).
// Every budget decision counts it next to the ledger, so concurrent generations (the job queue
// runs several workers) cannot each see the whole budget as free. It lives in the DB handle because
// the ledger readers (GenerationRepository.Budget, CostRepository.ProjectBudget) share it.
type inflightSpend struct {
	// settle is held shared while reserve checks the ledger and exclusively while a Take's cost goes
	// from its hold to the ledger (CreateReservedTake): the cost is then never counted in both.
	settle  sync.RWMutex
	mu      sync.Mutex
	project map[string]float64
	shot    map[string]float64
}

// add changes the reservation of the Project and the Shot by delta. The caller holds mu.
func (s *inflightSpend) add(projectID, shotID string, delta float64) {
	if s.project == nil {
		s.project, s.shot = map[string]float64{}, map[string]float64{}
	}
	if s.project[projectID] += delta; s.project[projectID] <= 1e-12 {
		delete(s.project, projectID)
	}
	if shotID == "" {
		return
	}
	if s.shot[shotID] += delta; s.shot[shotID] <= 1e-12 {
		delete(s.shot, shotID)
	}
}

// of is what is reserved for the Project and for the Shot. The caller holds mu.
func (s *inflightSpend) of(projectID, shotID string) (project, shot float64) {
	return s.project[projectID], s.shot[shotID]
}

// reserved is of under the lock. Readers of the ledger call it BEFORE they read the ledger: a
// reservation is released only after its cost is in the ledger, so the cost is then counted in at
// least one of the two (twice at worst, which only makes the decision more careful).
func (s *inflightSpend) reserved(projectID, shotID string) (project, shot float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.of(projectID, shotID)
}

// Remaining is the money left under the tighter of the two ceilings (+Inf when neither is set).
func (b Budget) Remaining() float64 {
	left := math.Inf(1)
	if b.ProjectCeiling != nil {
		left = math.Min(left, *b.ProjectCeiling-b.ProjectSpent)
	}
	if b.ShotCeiling != nil {
		left = math.Min(left, *b.ShotCeiling-b.ShotSpent)
	}
	return left
}

// reserve holds cost for the Project and the Shot (shotID "": the Project only) when it fits under
// remaining(spent) — the money left once the in-flight reservations are counted next to what the
// ledger holds (ledger reads it under the lock) — and tells whether it did.
func (s *inflightSpend) reserve(projectID, shotID string, cost float64, remaining func(reservedProject, reservedShot float64) (float64, error)) (release func(), ok bool, err error) {
	if cost <= 0 {
		return func() {}, true, nil
	}
	s.settle.RLock()
	defer s.settle.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	left, err := remaining(s.of(projectID, shotID))
	if err != nil {
		return nil, false, err
	}
	if left < cost-1e-9 {
		return nil, false, nil
	}
	s.add(projectID, shotID, cost)
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.add(projectID, shotID, -cost)
		})
	}, true, nil
}

// ShotSpends is Budget(projectID, id).ShotSpent of each of shotIDs from one ledger query (0 for a Shot with nothing on it).
func (r *GenerationRepository) ShotSpends(ctx context.Context, projectID string, shotIDs []string) (map[string]float64, error) {
	spends := make(map[string]float64, len(shotIDs))
	for _, id := range shotIDs {
		_, spends[id] = r.db.inflight.reserved(projectID, id) // before the ledger: see inflightSpend.reserved
	}
	type shotSum struct {
		id  string
		sum float64
	}
	rows, err := queryAll(ctx, r.db.Reader(), func(row rowScanner) (s shotSum, err error) { return s, row.Scan(&s.id, &s.sum) },
		`SELECT shot_id, SUM(COALESCE(actual_cost, estimated_cost)) FROM cost_ledger_entry WHERE project_id = ? AND shot_id IS NOT NULL GROUP BY shot_id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("sum cost ledger by shot: %w", err)
	}
	for _, s := range rows {
		if _, ok := spends[s.id]; ok {
			spends[s.id] += s.sum
		}
	}
	return spends, nil
}

// Reserve holds cost (the estimate of one generation about to be ordered) for the Project and the
// Shot until release is called, and reports ok=false when it does not fit under the ceilings next to
// the ledger and the other reservations. The check and the reservation are one step, so of several
// generations that start at the same time only those that fit under the ceiling go on. Call
// release once the cost is in the ledger or the generation has ended without one; it is safe to
// call more than once.
func (r *GenerationRepository) Reserve(ctx context.Context, projectID, shotID string, cost float64) (release func(), ok bool, err error) {
	return r.db.inflight.reserve(projectID, shotID, cost, func(reservedProject, reservedShot float64) (float64, error) {
		b, err := r.ledgerBudget(ctx, projectID, shotID)
		b.ProjectSpent += reservedProject
		b.ShotSpent += reservedShot
		return b.Remaining(), err // reserve drops the value when err != nil
	})
}

// Reserve holds cost (the estimate of one Audio Director call about to be paid) for the Project
// until release is called, and reports ok=false when it does not fit under the Project budget
// ceiling next to the ledger and the other reservations (those of generations included). Like
// GenerationRepository.Reserve, the check and the reservation are one step, so audio runs of one
// Project that go on at the same time cannot together pass the ceiling. Call release once the cost
// is in the ledger or the call has ended without one; it is safe to call more than once.
func (r *CostRepository) Reserve(ctx context.Context, projectID string, cost float64) (release func(), ok bool, err error) {
	return r.db.inflight.reserve(projectID, "", cost, func(reservedProject, _ float64) (float64, error) {
		b, err := (&GenerationRepository{db: r.db}).ledgerBudget(ctx, projectID, "")
		return Budget{ProjectCeiling: b.ProjectCeiling, ProjectSpent: b.ProjectSpent + reservedProject}.Remaining(), err // err: reserve drops the value
	})
}

// CreateReservedTake is CreateTake for a Take whose cost Reserve held: release gives the hold back as
// the ledger entry commits, so that a Reserve in between does not count the cost twice (in the ledger
// and in the hold) and refuse a generation that fits. When the Take is not created, release is not called.
func (r *GenerationRepository) CreateReservedTake(ctx context.Context, nt NewTake, release func()) (string, error) {
	r.db.inflight.settle.Lock()
	defer r.db.inflight.settle.Unlock()
	id, err := r.CreateTake(ctx, nt)
	if err == nil {
		release()
	}
	return id, err
}
