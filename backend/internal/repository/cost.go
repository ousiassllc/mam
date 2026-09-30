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

	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT s.id, s.order_index, s.title, COALESCE(sh.id, ''), COALESCE(sh.order_index, 0), COALESCE(sh.objective, '')
		 FROM scene s LEFT JOIN shot sh ON sh.scene_id = s.id
		 WHERE s.project_id = ? ORDER BY s.order_index, sh.order_index`, projectID)
	if err != nil {
		return domain.CostSummary{}, fmt.Errorf("list scenes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var sc domain.SceneCost
		var sh domain.ShotCost
		if err := rows.Scan(&sc.SceneID, &sc.OrderIndex, &sc.Title, &sh.ShotID, &sh.OrderIndex, &sh.Objective); err != nil {
			return domain.CostSummary{}, fmt.Errorf("scan scene: %w", err)
		}
		if n := len(sum.Scenes); n == 0 || sum.Scenes[n-1].SceneID != sc.SceneID {
			sum.Scenes = append(sum.Scenes, sc)
		}
		if sh.ShotID == "" {
			continue
		}
		scene := &sum.Scenes[len(sum.Scenes)-1]
		sh.Totals = perShot[sh.ShotID]
		scene.Shots = append(scene.Shots, sh)
		scene.Totals = scene.Totals.Add(sh.Totals)
	}
	if err := rows.Err(); err != nil {
		return domain.CostSummary{}, err
	}
	for _, sc := range sum.Scenes {
		sum.Project = sum.Project.Add(sc.Totals)
	}
	return sum, nil
}

// ledgerByShot sums the ledger of the Project per Shot; entries without a Shot are returned separately.
func (r *CostRepository) ledgerByShot(ctx context.Context, projectID string) (map[string]domain.CostTotals, domain.CostTotals, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT COALESCE(shot_id, ''), COUNT(*), COALESCE(SUM(estimated_cost), 0), COUNT(actual_cost), COALESCE(SUM(actual_cost), 0)
		 FROM cost_ledger_entry WHERE project_id = ? GROUP BY shot_id`, projectID)
	if err != nil {
		return nil, domain.CostTotals{}, fmt.Errorf("sum cost ledger: %w", err)
	}
	defer func() { _ = rows.Close() }()
	perShot := map[string]domain.CostTotals{}
	var unassigned domain.CostTotals
	for rows.Next() {
		var shotID string
		var t domain.CostTotals
		if err := rows.Scan(&shotID, &t.Entries, &t.Estimated, &t.ActualEntries, &t.Actual); err != nil {
			return nil, domain.CostTotals{}, fmt.Errorf("scan cost ledger: %w", err)
		}
		if shotID == "" {
			unassigned = t
			continue
		}
		perShot[shotID] = t
	}
	return perShot, unassigned, rows.Err()
}

// ProjectBudget returns the Project budget ceiling in USD (nil when none is set) and the money the
// cost ledger holds for the Project: the actual cost of an entry when the vendor reported one, else
// the estimate. It is what the Audio Director checks its spending against (F-22).
func (r *CostRepository) ProjectBudget(ctx context.Context, projectID string) (*float64, float64, error) {
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
		return insertCost(ctx, tx, e, formatTime(time.Now()))
	})
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("add cost: %w", err)
	}
	return nil
}
