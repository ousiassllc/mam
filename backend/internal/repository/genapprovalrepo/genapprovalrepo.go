// Package genapprovalrepo stores what the Orchestrator started of the generation of a Project (spec/spec.md §4.3): the stages it claimed for a Shot
// (generation_launch), and reads the generation approval cards that v0.1.34 left behind (chat_message with ref_type = generation, and the scope each
// stood for in generation_approval). Since #1115 AUTO asks for no approval, so nothing writes a card or a scope any more; the legacy rows are read
// to settle a card that is still pending and to honour a card the user declined.
// It sits beside the per-aggregate repositories of the parent package, which are over their line limit.
package genapprovalrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Stage is a step of the generation of a Shot the Orchestrator starts.
type Stage string

// Stages.
const (
	StageReferencePack Stage = "reference_pack"
	StageGenerate      Stage = "generate"
)

// Outcome is what became of a claimed stage.
type Outcome string

// Outcomes. A stage that was refused (the budget ceiling) or failed to start stays claimed, so it is not tried again by itself.
const (
	OutcomeStarted Outcome = "started"
	OutcomeBudget  Outcome = "budget"
	OutcomeFailed  Outcome = "failed"
)

// Approval is what a legacy approval card of the generation checkpoint stood for (read-only).
type Approval struct {
	MessageID, ProjectID string
	// Scope is the JSON of the planned Shots shown on the card, each with its estimate.
	Scope json.RawMessage
}

// Card is the newest legacy approval card of the generation checkpoint.
type Card struct {
	ID     string
	Status domain.ApprovalStatus
}

// Launch is a stage of a Shot that the Orchestrator claimed. JobID is "" until the job exists, and for a stage that was refused.
type Launch struct {
	ShotID  string
	Stage   Stage
	JobID   string
	Outcome Outcome
}

// Repository reads the legacy generation approval cards and writes generation_launch.
type Repository struct{ db *repository.DB }

// New returns a Repository over db.
func New(db *repository.DB) *Repository { return &Repository{db: db} }

// LatestCard returns the newest legacy generation approval card of the Project, or nil when it has none. "Newest" is the order this instance
// issued the rows in (rowid), never the message ID or created_at: a bundle brings both with it, so a card with a huge ID would stay "the newest" for ever
// (spec/spec.md §4.3). A bundle cannot set the rowid (the import refuses a column the schema does not have).
func (r *Repository) LatestCard(ctx context.Context, projectID string) (*Card, error) {
	var c Card
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT id, COALESCE(approval_status, '') FROM chat_message
		  WHERE project_id = ? AND kind = 'approval' AND ref_type = ? ORDER BY rowid DESC LIMIT 1`,
		projectID, domain.CheckpointGeneration.RefType()).Scan(&c.ID, &c.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get generation approval card: %w", err)
	}
	return &c, nil
}

// Approval returns the record of the card; ok is false when it has none.
func (r *Repository) Approval(ctx context.Context, messageID string) (a Approval, ok bool, err error) {
	var scope string
	err = r.db.Reader().QueryRowContext(ctx,
		`SELECT message_id, project_id, scope FROM generation_approval WHERE message_id = ?`, messageID).
		Scan(&a.MessageID, &a.ProjectID, &scope)
	if errors.Is(err, sql.ErrNoRows) {
		return Approval{}, false, nil
	}
	if err != nil {
		return Approval{}, false, fmt.Errorf("get generation approval: %w", err)
	}
	a.Scope = json.RawMessage(scope)
	return a, true, nil
}

// Unrecorded reports whether m is a generation approval card that has no record of what it showed in the Project. A bundle does not carry
// generation_approval, so every such card of an imported Project is unrecorded; it is never taken as an approval (spec/spec.md §4.3). Any other message is not.
func (r *Repository) Unrecorded(ctx context.Context, projectID string, m domain.ChatMessage) (bool, error) {
	if m.Kind != domain.KindApproval || m.RefType != domain.CheckpointGeneration.RefType() {
		return false, nil
	}
	a, ok, err := r.Approval(ctx, m.ID)
	return !ok || a.ProjectID != projectID, err
}

// LaunchProject returns the Project of a Shot that has a claimed stage: the Shots started by hand have none. ok is false when it has none.
func (r *Repository) LaunchProject(ctx context.Context, shotID string) (projectID string, ok bool, err error) {
	err = r.db.Reader().QueryRowContext(ctx,
		`SELECT sc.project_id FROM generation_launch l JOIN shot sh ON sh.id = l.shot_id JOIN scene sc ON sc.id = sh.scene_id WHERE l.shot_id = ? LIMIT 1`, shotID).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find the project of a generation launch: %w", err)
	}
	return projectID, true, nil
}

// Launches returns every stage of the Shots of the Project that was claimed, by Shot.
func (r *Repository) Launches(ctx context.Context, projectID string) (map[string]map[Stage]Launch, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT l.shot_id, l.stage, COALESCE(l.job_id, ''), l.outcome FROM generation_launch l
		   JOIN shot sh ON sh.id = l.shot_id JOIN scene sc ON sc.id = sh.scene_id WHERE sc.project_id = ?`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list generation launches: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]map[Stage]Launch{}
	for rows.Next() {
		var l Launch
		if err := rows.Scan(&l.ShotID, &l.Stage, &l.JobID, &l.Outcome); err != nil {
			return nil, fmt.Errorf("list generation launches: %w", err)
		}
		if out[l.ShotID] == nil {
			out[l.ShotID] = map[Stage]Launch{}
		}
		out[l.ShotID][l.Stage] = l
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list generation launches: %w", err)
	}
	return out, nil
}

// Claim takes the stage of the Shot. It reports whether this call took it: false means it was taken before (a start is never repeated).
func (r *Repository) Claim(ctx context.Context, shotID string, stage Stage) (bool, error) {
	var claimed bool
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO generation_launch (shot_id, stage) VALUES (?, ?)`, shotID, stage)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		claimed = n > 0
		return err
	})
	if err != nil {
		return false, fmt.Errorf("claim generation launch: %w", err)
	}
	return claimed, nil
}

// Started records the job of a claimed stage.
func (r *Repository) Started(ctx context.Context, shotID string, stage Stage, jobID string) error {
	return r.update(ctx, `UPDATE generation_launch SET job_id = ? WHERE shot_id = ? AND stage = ?`, jobID, shotID, stage)
}

// Stopped marks a claimed stage that did not start: it stays claimed, so it is not tried again.
func (r *Repository) Stopped(ctx context.Context, shotID string, stage Stage, outcome Outcome) error {
	return r.update(ctx, `UPDATE generation_launch SET outcome = ? WHERE shot_id = ? AND stage = ?`, outcome, shotID, stage)
}

// Release gives a claimed stage back, because nothing was started (the Shot was busy): the next look tries it again.
func (r *Repository) Release(ctx context.Context, shotID string, stage Stage) error {
	return r.update(ctx, `DELETE FROM generation_launch WHERE shot_id = ? AND stage = ? AND job_id IS NULL`, shotID, stage)
}

func (r *Repository) update(ctx context.Context, query string, args ...any) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("update generation launch: %w", err)
		}
		return nil
	})
}

// AskedAgainAfter reports whether the user wrote a message in the thread after the message afterID that is not a reply to it: after a
// declined legacy approval, that is the user telling the Orchestrator to go on (spec/spec.md §4.3). "After" is the order this instance issued the
// rows in (rowid, as in LatestCard), so that the ID of an imported card does not decide it.
func (r *Repository) AskedAgainAfter(ctx context.Context, projectID, afterID string) (bool, error) {
	var n int
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chat_message WHERE project_id = ? AND role = 'user' AND rowid > (SELECT rowid FROM chat_message WHERE id = ?) AND COALESCE(reply_to_message_id, '') <> ?`,
		projectID, afterID, afterID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("find a message after the approval: %w", err)
	}
	return n > 0, nil
}
