package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// CostRepository reads the cost ledger (cost_ledger_entry) of a Project.
type CostRepository struct{ db *DB }

// NewCostRepository returns a CostRepository backed by db.
func NewCostRepository(db *DB) *CostRepository { return &CostRepository{db: db} }

// Summary returns the estimated and the actual cost of the Project by Shot, Scene and Project.
// Every Scene and Shot of the Project is listed, also when nothing was spent on it yet.
func (r *CostRepository) Summary(ctx context.Context, projectID string) (domain.CostSummary, error) {
	var (
		sum                 domain.CostSummary
		projectCap, shotCap sql.NullFloat64
	)
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT project_budget_ceiling, shot_budget_ceiling FROM project WHERE id = ?`, projectID).Scan(&projectCap, &shotCap)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CostSummary{}, ErrNotFound
	}
	if err != nil {
		return domain.CostSummary{}, fmt.Errorf("get ceilings: %w", err)
	}
	sum.ProjectCeiling, sum.ShotCeiling = floatOrNil(projectCap), floatOrNil(shotCap)

	perShot, unassigned, err := r.ledgerByShot(ctx, projectID)
	if err != nil {
		return domain.CostSummary{}, err
	}
	sum.Unassigned = unassigned
	sum.Project = unassigned

	type sceneShot struct {
		scene domain.SceneCost
		shot  domain.ShotCost
	}
	rows, err := queryAll(ctx, r.db.Reader(), func(row rowScanner) (sceneShot, error) {
		var r sceneShot
		sc, sh := &r.scene, &r.shot
		if err := row.Scan(&sc.SceneID, &sc.BranchID, &sc.BranchName, &sc.OrderIndex, &sc.Title, &sh.ShotID, &sh.OrderIndex, &sh.Objective); err != nil {
			return r, fmt.Errorf("scan scene: %w", err)
		}
		return r, nil
	}, `SELECT s.id, COALESCE(s.branch_id, ''), COALESCE(b.name, ''), s.order_index, s.title,
		        COALESCE(sh.id, ''), COALESCE(sh.order_index, 0), COALESCE(sh.objective, '')
		 FROM scene s LEFT JOIN branch b ON b.id = s.branch_id LEFT JOIN shot sh ON sh.scene_id = s.id
		 WHERE s.project_id = ?
		 ORDER BY COALESCE(s.branch_id, ''), s.order_index, s.id, sh.order_index`, projectID)
	if err != nil {
		return domain.CostSummary{}, fmt.Errorf("list scenes: %w", err)
	}
	for _, row := range rows {
		if n := len(sum.Scenes); n == 0 || sum.Scenes[n-1].SceneID != row.scene.SceneID {
			sum.Scenes = append(sum.Scenes, row.scene)
		}
		if row.shot.ShotID == "" {
			continue
		}
		sh := row.shot
		scene := &sum.Scenes[len(sum.Scenes)-1]
		sh.Totals = perShot[sh.ShotID]
		scene.Shots = append(scene.Shots, sh)
		scene.Totals = scene.Totals.Add(sh.Totals)
	}
	for _, sc := range sum.Scenes {
		sum.Project = sum.Project.Add(sc.Totals)
	}
	return sum, nil
}

// ledgerByShot sums the ledger of the Project per Shot; entries without a Shot are returned separately.
func (r *CostRepository) ledgerByShot(ctx context.Context, projectID string) (map[string]domain.CostTotals, domain.CostTotals, error) {
	type shotTotals struct {
		shotID string
		totals domain.CostTotals
	}
	rows, err := queryAll(ctx, r.db.Reader(), func(row rowScanner) (shotTotals, error) {
		var st shotTotals
		t := &st.totals
		return st, row.Scan(&st.shotID, &t.Entries, &t.Estimated, &t.ActualEntries, &t.Actual)
	}, `SELECT COALESCE(shot_id, ''), COUNT(*), COALESCE(SUM(estimated_cost), 0), COUNT(actual_cost), COALESCE(SUM(actual_cost), 0)
		 FROM cost_ledger_entry WHERE project_id = ? GROUP BY shot_id`, projectID)
	if err != nil {
		return nil, domain.CostTotals{}, fmt.Errorf("sum cost ledger: %w", err)
	}
	perShot := map[string]domain.CostTotals{}
	var unassigned domain.CostTotals
	for _, st := range rows {
		if st.shotID == "" {
			unassigned = st.totals
			continue
		}
		perShot[st.shotID] = st.totals
	}
	return perShot, unassigned, nil
}

// ProjectBudget returns the Project budget ceiling in USD (nil when none is set) and the money the
// cost ledger holds for the Project: the actual cost of an entry when the vendor reported one, else
// the estimate; plus what generations in flight have reserved (Reserve). It is what the Audio
// Director checks its spending against (F-22).
func (r *CostRepository) ProjectBudget(ctx context.Context, projectID string) (*float64, float64, error) {
	reserved, _ := r.db.inflight.reserved(projectID, "") // before the ledger: see inflightSpend.reserved
	ceiling, spent, err := r.ledgerProjectBudget(ctx, projectID)
	return ceiling, spent + reserved, err
}

// ledgerProjectBudget is ProjectBudget without the reservations.
func (r *CostRepository) ledgerProjectBudget(ctx context.Context, projectID string) (*float64, float64, error) {
	var ceiling sql.NullFloat64
	err := r.db.Reader().QueryRowContext(ctx, `SELECT project_budget_ceiling FROM project WHERE id = ?`, projectID).Scan(&ceiling)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("get ceiling: %w", err)
	}
	var spent float64
	if err := r.db.Reader().QueryRowContext(ctx,
		`SELECT COALESCE(SUM(COALESCE(actual_cost, estimated_cost)), 0) FROM cost_ledger_entry WHERE project_id = ?`, projectID).Scan(&spent); err != nil {
		return nil, 0, fmt.Errorf("sum cost ledger: %w", err)
	}
	return floatOrNil(ceiling), spent, nil
}

// AddCost appends an entry to the cost ledger.
func (r *CostRepository) AddCost(ctx context.Context, e domain.CostEntry) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := projectExists(ctx, tx, e.ProjectID); err != nil {
			return err
		}
		return insertCost(ctx, tx, e, FormatTime(time.Now()))
	})
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("add cost: %w", err)
	}
	return nil
}
