package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// AnalyticsRepository records the human interventions of a Project (human_intervention) and
// computes the MVP Success Metrics (spec/spec.md §28) from them and from the tables of the
// production: Take, Judge Result, Repair Action, Cost Ledger, Export and the Edit Timeline. Nothing
// leaves the local database (spec/requirements/non-functional.md §5). Writes go through DB.Write.
type AnalyticsRepository struct {
	db  *DB
	now func() time.Time
}

// NewAnalyticsRepository returns an AnalyticsRepository backed by db.
func NewAnalyticsRepository(db *DB) *AnalyticsRepository {
	return &AnalyticsRepository{db: db, now: time.Now}
}

// Record stores one human intervention. The Project is taken from the Shot, or from the Take's
// Shot, when in.ProjectID is empty; ErrNotFound means it names none.
func (r *AnalyticsRepository) Record(ctx context.Context, in domain.Intervention) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		projectID, shotID := in.ProjectID, in.ShotID
		if in.TakeID != "" && shotID == "" {
			err := tx.QueryRowContext(ctx,
				`SELECT pv.shot_id FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE t.id = ?`, in.TakeID).Scan(&shotID)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
		}
		if projectID == "" {
			err := tx.QueryRowContext(ctx,
				`SELECT sc.project_id FROM shot sh JOIN scene sc ON sc.id = sh.scene_id WHERE sh.id = ?`, shotID).Scan(&projectID)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO human_intervention (id, project_id, shot_id, take_id, kind, override, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			domain.NewID(), projectID, nullIfEmpty(shotID), nullIfEmpty(in.TakeID), in.Kind, in.Override, formatTime(r.now()))
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("record intervention: %w", err)
	}
	return nil
}

// ShotState tells whether a Take was ever made for the Shot, and whether one of its Takes that was
// not rejected passed the Judge (its latest Judge result is a pass).
func (r *AnalyticsRepository) ShotState(ctx context.Context, shotID string) (hasTakes, passed bool, err error) {
	err = r.db.Reader().QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE pv.shot_id = ?1),
		        EXISTS (SELECT 1 FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id
		                 WHERE pv.shot_id = ?1 AND t.status <> 'rejected'
		                   AND (SELECT j.result FROM judge_result j WHERE j.take_id = t.id ORDER BY j.created_at DESC, j.rowid DESC LIMIT 1) = 'pass')`,
		shotID).Scan(&hasTakes, &passed)
	if err != nil {
		return false, false, fmt.Errorf("read shot state: %w", err)
	}
	return hasTakes, passed, nil
}

// scopedProjects is the CTE the metric queries share: the Projects in scope (?1 is the Project ID,
// "" for all of them) with what the metrics need to know of each. A Project is Completed when it
// reached READY or EXPORTED or has an Export; Switched when it was ever taken over in DIRECTOR MODE.
const scopedProjects = `
WITH p AS (
  SELECT pr.id, pr.state, pr.mode, pr.created_at,
         EXISTS (SELECT 1 FROM export e WHERE e.project_id = pr.id) AS exported,
         (pr.state IN ('READY', 'EXPORTED') OR EXISTS (SELECT 1 FROM export e WHERE e.project_id = pr.id)) AS completed,
         (SELECT COUNT(*) FROM human_intervention h WHERE h.project_id = pr.id AND h.kind <> 'switch_to_director') AS interventions,
         EXISTS (SELECT 1 FROM human_intervention h WHERE h.project_id = pr.id AND h.kind = 'switch_to_director') AS switched
  FROM project pr
  WHERE ?1 = '' OR pr.id = ?1
)`

// Metrics returns the MVP Success Metrics of the Project, or of every Project when projectID is "".
// A Project that does not exist is ErrNotFound.
func (r *AnalyticsRepository) Metrics(ctx context.Context, projectID string) (domain.ProductionMetrics, error) {
	if projectID != "" {
		var one int
		err := r.db.Reader().QueryRowContext(ctx, `SELECT 1 FROM project WHERE id = ?`, projectID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ProductionMetrics{}, ErrNotFound
		}
		if err != nil {
			return domain.ProductionMetrics{}, fmt.Errorf("get project: %w", err)
		}
	}
	var m domain.ProductionMetrics
	steps := []func(context.Context, string, *domain.ProductionMetrics) error{r.projectMetrics, r.shotMetrics, r.judgeMetrics, r.costMetrics}
	for _, step := range steps {
		if err := step(ctx, projectID, &m); err != nil {
			return domain.ProductionMetrics{}, err
		}
	}
	return m, nil
}

func (r *AnalyticsRepository) projectMetrics(ctx context.Context, projectID string, m *domain.ProductionMetrics) error {
	var (
		autoStarted, autoDone, clean                int
		interventions, overrides, roughCuts, finals int
		roughSeconds, finalSeconds                  float64
	)
	err := r.db.Reader().QueryRowContext(ctx, scopedProjects+`
SELECT COUNT(*),
       COALESCE(SUM(state <> 'DRAFT'), 0),
       COALESCE(SUM(completed), 0),
       COALESCE(SUM(exported), 0),
       COALESCE(SUM(state <> 'DRAFT' AND (mode = 'auto' OR switched)), 0),
       COALESCE(SUM(state <> 'DRAFT' AND (mode = 'auto' OR switched) AND completed AND NOT switched), 0),
       COALESCE(SUM(completed AND NOT switched AND interventions = 0), 0),
       COALESCE(SUM(CASE WHEN completed THEN interventions END), 0),
       (SELECT COUNT(*) FROM human_intervention h JOIN p ON p.id = h.project_id WHERE h.override = 1),
       COUNT(rc), COALESCE(SUM(rc), 0), COUNT(fin), COALESCE(SUM(fin), 0)
FROM (
  SELECT p.*,
         (strftime('%s', (SELECT MIN(e.created_at) FROM edit_timeline_version e WHERE e.project_id = p.id)) - strftime('%s', p.created_at)) AS rc,
         (strftime('%s', (SELECT MIN(x.exported_at) FROM export x WHERE x.project_id = p.id)) - strftime('%s', p.created_at)) AS fin
  FROM p
) p`, projectID).Scan(&m.Projects, &m.Started, &m.Completed, &m.Exported, &autoStarted, &autoDone, &clean,
		&interventions, &overrides, &roughCuts, &roughSeconds, &finals, &finalSeconds)
	if err != nil {
		return fmt.Errorf("project metrics: %w", err)
	}
	m.AutoCompletion = domain.Ratio{Numerator: float64(autoDone), Denominator: float64(autoStarted)}
	m.NorthStar = domain.Ratio{Numerator: float64(clean), Denominator: float64(m.Started)}
	m.ExportCompletion = domain.Ratio{Numerator: float64(m.Exported), Denominator: float64(m.Completed)}
	m.InterventionsPerFinished = domain.Ratio{Numerator: float64(interventions), Denominator: float64(m.Completed)}
	m.TimeToFirstRoughCut = domain.Ratio{Numerator: roughSeconds, Denominator: float64(roughCuts)}
	m.TimeToFinalVideo = domain.Ratio{Numerator: finalSeconds, Denominator: float64(finals)}
	m.OverrideEvents = overrides
	return nil
}

// shotMetrics reads the Shots with at least one Take. The first Take of a Shot is its oldest one.
// A Shot is accepted when it has a selected Take (the AI's, once it passed the Judge, or the user's).
func (r *AnalyticsRepository) shotMetrics(ctx context.Context, projectID string, m *domain.ProductionMetrics) error {
	var (
		accepted, judgedFirst, firstPass, retakes, overridden int
		seconds                                               float64
	)
	err := r.db.Reader().QueryRowContext(ctx, scopedProjects+`,
s AS (
  SELECT sh.id, sh.selected_take_id,
         (SELECT t.id FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id
           WHERE pv.shot_id = sh.id ORDER BY t.created_at, t.rowid LIMIT 1) AS first_take,
         COALESCE(NULLIF((SELECT json_extract(t.model_parameters, '$.duration_seconds') FROM take t WHERE t.id = sh.selected_take_id), 0),
                  sh.duration_seconds, 0) AS seconds
  FROM shot sh JOIN scene sc ON sc.id = sh.scene_id JOIN p ON p.id = sc.project_id
),
f AS (
  SELECT s.*,
         (SELECT j.result FROM judge_result j WHERE j.take_id = s.first_take ORDER BY j.created_at, j.rowid LIMIT 1) AS first_verdict,
         (SELECT t.status FROM take t WHERE t.id = s.first_take) AS first_status
  FROM s WHERE s.first_take IS NOT NULL
)
SELECT COUNT(*),
       COALESCE(SUM(selected_take_id IS NOT NULL), 0),
       COALESCE(SUM(CASE WHEN selected_take_id IS NOT NULL THEN seconds END), 0),
       COALESCE(SUM(first_verdict IS NOT NULL), 0),
       COALESCE(SUM(first_verdict = 'pass' AND first_status <> 'rejected'), 0),
       (SELECT COUNT(*) FROM repair_action ra
          JOIN take t ON t.id = ra.resulting_take_id JOIN prompt_version pv ON pv.id = t.prompt_version_id
          JOIN shot sh ON sh.id = pv.shot_id JOIN scene sc ON sc.id = sh.scene_id JOIN p ON p.id = sc.project_id)
       + (SELECT COUNT(*) FROM human_intervention h JOIN p ON p.id = h.project_id WHERE h.kind = 'regenerate'),
       (SELECT COUNT(DISTINCT h.shot_id) FROM human_intervention h JOIN f ON f.id = h.shot_id WHERE h.override = 1)
FROM f`, projectID).Scan(&m.Shots, &accepted, &seconds, &judgedFirst, &firstPass, &retakes, &overridden)
	if err != nil {
		return fmt.Errorf("shot metrics: %w", err)
	}
	m.FinalShotAcceptance = domain.Ratio{Numerator: float64(accepted), Denominator: float64(m.Shots)}
	m.FirstPassShotAcceptance = domain.Ratio{Numerator: float64(firstPass), Denominator: float64(judgedFirst)}
	m.AverageRetakesPerShot = domain.Ratio{Numerator: float64(retakes), Denominator: float64(m.Shots)}
	m.UserOverride = domain.Ratio{Numerator: float64(overridden), Denominator: float64(m.Shots)}
	m.CostPerAcceptedSecond.Denominator = seconds
	return nil
}

// issueOf is the SQL condition "the latest Judge result has a medium or high Issue of one of these
// dimensions". The dimensions are constants of the domain, never user input.
func issueOf(dims []domain.JudgeDimension) string {
	quoted := make([]string, len(dims))
	for i, d := range dims {
		quoted[i] = "'" + string(d) + "'"
	}
	return `EXISTS (SELECT 1 FROM json_each(l.issues) i WHERE json_extract(i.value, '$.dimension') IN (` + strings.Join(quoted, ", ") +
		`) AND json_extract(i.value, '$.severity') IN ('medium', 'high'))`
}

// judgeMetrics reads the latest Judge result of every judged Take.
func (r *AnalyticsRepository) judgeMetrics(ctx context.Context, projectID string, m *domain.ProductionMetrics) error {
	var judged, character, product, prompt int
	err := r.db.Reader().QueryRowContext(ctx, scopedProjects+`,
l AS (
  SELECT j.issues
  FROM judge_result j
  JOIN take t ON t.id = j.take_id JOIN prompt_version pv ON pv.id = t.prompt_version_id
  JOIN shot sh ON sh.id = pv.shot_id JOIN scene sc ON sc.id = sh.scene_id JOIN p ON p.id = sc.project_id
  WHERE j.rowid = (SELECT j2.rowid FROM judge_result j2 WHERE j2.take_id = j.take_id ORDER BY j2.created_at DESC, j2.rowid DESC LIMIT 1)
)
SELECT COUNT(*),
       COALESCE(SUM(`+issueOf(domain.CharacterContinuityDimensions)+`), 0),
       COALESCE(SUM(`+issueOf(domain.ProductAccuracyDimensions)+`), 0),
       COALESCE(SUM(`+issueOf(domain.PromptAdherenceDimensions)+`), 0)
FROM l`, projectID).Scan(&judged, &character, &product, &prompt)
	if err != nil {
		return fmt.Errorf("judge metrics: %w", err)
	}
	m.CharacterContinuityFail = domain.Ratio{Numerator: float64(character), Denominator: float64(judged)}
	m.ProductAccuracyFail = domain.Ratio{Numerator: float64(product), Denominator: float64(judged)}
	m.PromptAdherence = domain.Ratio{Numerator: float64(judged - prompt), Denominator: float64(judged)}
	return nil
}

// costMetrics sums the Cost Ledger of the scope: the actual cost when the vendor reported one, else
// the estimate. Every generation counts, accepted or not.
func (r *AnalyticsRepository) costMetrics(ctx context.Context, projectID string, m *domain.ProductionMetrics) error {
	var cost float64
	err := r.db.Reader().QueryRowContext(ctx, scopedProjects+`
SELECT COALESCE(SUM(COALESCE(c.actual_cost, c.estimated_cost)), 0) FROM cost_ledger_entry c JOIN p ON p.id = c.project_id`, projectID).Scan(&cost)
	if err != nil {
		return fmt.Errorf("cost metrics: %w", err)
	}
	m.CostPerAcceptedSecond.Numerator = cost
	return nil
}
