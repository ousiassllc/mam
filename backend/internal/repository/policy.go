package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// PolicyRepository persists the Production Template, the Quality/Cost Policy and the budget
// ceilings of a Project (F-21, F-22). All writes go through DB.Write.
type PolicyRepository struct{ db *DB }

// NewPolicyRepository returns a PolicyRepository backed by db.
func NewPolicyRepository(db *DB) *PolicyRepository { return &PolicyRepository{db: db} }

// Get returns the policy of the Project or ErrNotFound.
func (r *PolicyRepository) Get(ctx context.Context, projectID string) (domain.ProjectPolicy, error) {
	var (
		p                                     = domain.ProjectPolicy{ProjectID: projectID}
		projectCap, shotCap                   sql.NullFloat64
		retries, seconds, acceptance, axisMin sql.NullInt64
	)
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT p.production_template, p.quality_policy, p.project_budget_ceiling, p.shot_budget_ceiling,
		        pp.max_retries, pp.max_generation_seconds, pp.min_acceptance_score, pp.min_axis_score
		 FROM project p LEFT JOIN project_policy pp ON pp.project_id = p.id WHERE p.id = ?`, projectID).
		Scan(&p.Template, &p.Quality, &projectCap, &shotCap, &retries, &seconds, &acceptance, &axisMin)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProjectPolicy{}, ErrNotFound
	}
	if err != nil {
		return domain.ProjectPolicy{}, fmt.Errorf("get policy: %w", err)
	}
	p.ProjectBudgetCeiling = floatOrNil(projectCap)
	p.ShotBudgetCeiling = floatOrNil(shotCap)
	p.Limits = domain.PolicyLimits{
		MaxRetries: intOrNil(retries), MaxGenerationSeconds: intOrNil(seconds),
		MinAcceptanceScore: intOrNil(acceptance), MinAxisScore: intOrNil(axisMin),
	}
	return p, nil
}

func floatOrNil(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}

func intOrNil(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

// updateProject runs one UPDATE of the project row and maps "no row" to ErrNotFound.
func (r *PolicyRepository) updateProject(ctx context.Context, what, query string, args ...any) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("set %s: %w", what, err)
	}
	return nil
}

// SetProductionTemplate switches the Production Template of the Project.
func (r *PolicyRepository) SetProductionTemplate(ctx context.Context, projectID string, t domain.ProductionTemplate) error {
	return r.updateProject(ctx, "production template", `UPDATE project SET production_template = ? WHERE id = ?`, t, projectID)
}

// SetQualityPolicy changes the Quality Policy of the Project. limits, when not nil, replaces the
// overrides of the policy (a nil field inside it clears that override).
func (r *PolicyRepository) SetQualityPolicy(ctx context.Context, projectID string, q domain.QualityPolicy, limits *domain.PolicyLimits) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE project SET quality_policy = ? WHERE id = ?`, q, projectID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if limits == nil {
			return nil
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO project_policy (project_id, max_retries, max_generation_seconds, min_acceptance_score, min_axis_score)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT (project_id) DO UPDATE SET max_retries = excluded.max_retries,
			   max_generation_seconds = excluded.max_generation_seconds,
			   min_acceptance_score = excluded.min_acceptance_score, min_axis_score = excluded.min_axis_score`,
			projectID, limits.MaxRetries, limits.MaxGenerationSeconds, limits.MinAcceptanceScore, limits.MinAxisScore)
		return err
	})
	if err != nil {
		return fmt.Errorf("set quality policy: %w", err)
	}
	return nil
}

// SetBudget sets the budget ceilings in USD of the Project and of one Shot; nil removes a ceiling.
func (r *PolicyRepository) SetBudget(ctx context.Context, projectID string, project, shot *float64) error {
	return r.updateProject(ctx, "budget", `UPDATE project SET project_budget_ceiling = ?, shot_budget_ceiling = ? WHERE id = ?`,
		project, shot, projectID)
}
