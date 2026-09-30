package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ErrConflict is returned when a row is not in the state the write requires
// (for example, an approval card that was already answered).
var ErrConflict = errors.New("repository: conflict")

// ProjectRepository persists Projects with their Brief, Assets and chat thread.
// All writes go through DB.Write.
type ProjectRepository struct {
	db  *DB
	now func() time.Time
}

// NewProjectRepository returns a ProjectRepository backed by db.
func NewProjectRepository(db *DB) *ProjectRepository {
	return &ProjectRepository{db: db, now: time.Now}
}

// NewProject is everything written when a Project is created, in one transaction.
type NewProject struct {
	Project  domain.Project
	Brief    domain.Brief
	Assets   []domain.Asset
	Messages []domain.ChatMessage
}

// Create inserts the Project, its Brief, its uploaded Assets and its first messages.
func (r *ProjectRepository) Create(ctx context.Context, np NewProject) error {
	now := formatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		p := np.Project
		if err := insertProject(ctx, tx, p, np.Brief, now); err != nil {
			return err
		}
		for _, a := range np.Assets {
			if err := insertAsset(ctx, tx, a, now); err != nil {
				return err
			}
		}
		for _, m := range np.Messages {
			if err := insertMessage(ctx, tx, m, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	return nil
}

// insertProject inserts the project row (a Project outside any Series) and its Brief.
func insertProject(ctx context.Context, tx *sql.Tx, p domain.Project, brief domain.Brief, now string) error {
	briefJSON, err := json.Marshal(brief)
	if err != nil {
		return fmt.Errorf("marshal brief: %w", err)
	}
	checkpoints, err := json.Marshal(nonNilCheckpoints(p.CustomCheckpoints))
	if err != nil {
		return fmt.Errorf("marshal checkpoints: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO project (id, state, mode, confirmation_level, custom_checkpoints, production_template, quality_policy, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.State, p.Mode, p.ConfirmationLevel, string(checkpoints), p.ProductionTemplate, p.QualityPolicy, now); err != nil {
		return fmt.Errorf("insert project: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO brief (id, project_id, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		domain.NewID(), p.ID, string(briefJSON), now, now); err != nil {
		return fmt.Errorf("insert brief: %w", err)
	}
	return nil
}

// ApprovalUpdate records the user's answer to an approval card.
type ApprovalUpdate struct {
	MessageID string
	Status    domain.ApprovalStatus
}

// Confirmation is the Confirmation Level setting (F-03).
type Confirmation struct {
	Level       domain.ConfirmationLevel
	Checkpoints []domain.Checkpoint
}

// Turn is one atomic change of a Project: it appends messages and optionally
// updates the Brief, the state, an approval card and the Confirmation Level
// together, so the thread never disagrees with the Project.
type Turn struct {
	ProjectID string
	Assets    []domain.Asset // uploaded files the messages refer to
	Messages  []domain.ChatMessage
	Brief     *domain.Brief
	State     *domain.ProjectState
	Approval  *ApprovalUpdate
	// Confirmation replaces the Confirmation Level setting.
	Confirmation *Confirmation
	// Mode switches AUTO / DIRECTOR (F-02).
	Mode *domain.Mode
	// ResumeState, when set, moves the Project out of AWAITING_APPROVAL into that state
	// after the other changes, but only if no blocking card is left unanswered.
	ResumeState *domain.ProjectState
}

// Apply runs the Turn in one transaction. It returns ErrNotFound when the Project does
// not exist and ErrConflict when Approval targets a card that is not pending.
func (r *ProjectRepository) Apply(ctx context.Context, t Turn) error {
	var briefJSON, checkpoints []byte
	var err error
	if t.Brief != nil {
		if briefJSON, err = json.Marshal(t.Brief); err != nil {
			return fmt.Errorf("marshal brief: %w", err)
		}
	}
	if t.Confirmation != nil {
		if checkpoints, err = json.Marshal(nonNilCheckpoints(t.Confirmation.Checkpoints)); err != nil {
			return fmt.Errorf("marshal checkpoints: %w", err)
		}
	}
	now := formatTime(r.now())
	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM project WHERE id = ?`, t.ProjectID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if t.Approval != nil {
			res, err := tx.ExecContext(ctx,
				`UPDATE chat_message SET approval_status = ? WHERE id = ? AND project_id = ? AND kind = 'approval' AND approval_status = 'pending'`,
				t.Approval.Status, t.Approval.MessageID, t.ProjectID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n != 1 {
				return ErrConflict
			}
		}
		for _, a := range t.Assets {
			if err := insertAsset(ctx, tx, a, now); err != nil {
				return err
			}
		}
		for _, m := range t.Messages {
			m.ProjectID = t.ProjectID
			if err := insertMessage(ctx, tx, m, now); err != nil {
				return err
			}
		}
		if briefJSON != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE brief SET content = ?, updated_at = ? WHERE project_id = ?`,
				string(briefJSON), now, t.ProjectID); err != nil {
				return err
			}
		}
		if t.Confirmation != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE project SET confirmation_level = ?, custom_checkpoints = ? WHERE id = ?`,
				t.Confirmation.Level, string(checkpoints), t.ProjectID); err != nil {
				return err
			}
		}
		if t.Mode != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE project SET mode = ? WHERE id = ?`, *t.Mode, t.ProjectID); err != nil {
				return err
			}
		}
		if t.State != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE project SET state = ? WHERE id = ?`, *t.State, t.ProjectID); err != nil {
				return err
			}
		}
		if t.ResumeState != nil {
			_, err := tx.ExecContext(ctx,
				`UPDATE project SET state = ? WHERE id = ? AND state = 'AWAITING_APPROVAL' AND `+noBlockingCard, *t.ResumeState, t.ProjectID)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("apply turn: %w", err)
	}
	return nil
}

// noBlockingCard is true for a project row without an unanswered approval card or
// escalation card. An escalation is answered by any reply to it.
const noBlockingCard = `NOT EXISTS (
	SELECT 1 FROM chat_message m WHERE m.project_id = project.id AND (
		(m.kind = 'approval' AND m.approval_status = 'pending' AND COALESCE(m.ref_type, '') <> 'note_promotion')
		OR (m.kind = 'escalation' AND NOT EXISTS (SELECT 1 FROM chat_message r WHERE r.reply_to_message_id = m.id))))`

const projectSelect = `
SELECT p.id, p.state, p.mode, p.confirmation_level, p.custom_checkpoints, p.production_template, p.quality_policy, p.created_at,
       COALESCE(json_extract(b.content, '$.subject'), ''),
       COALESCE((SELECT m.content FROM chat_message m WHERE m.project_id = p.id AND m.role = 'user' ORDER BY m.id LIMIT 1), ''),
       COALESCE(p.series_id, ''), COALESCE(p.episode_number, 0), COALESCE(s.name, '')
FROM project p LEFT JOIN brief b ON b.project_id = p.id LEFT JOIN series s ON s.id = p.series_id`

func scanProject(row rowScanner) (domain.Project, error) {
	var (
		p                     domain.Project
		checkpoints, created  string
		subject, firstMessage string
		seriesName            string
	)
	if err := row.Scan(&p.ID, &p.State, &p.Mode, &p.ConfirmationLevel, &checkpoints, &p.ProductionTemplate,
		&p.QualityPolicy, &created, &subject, &firstMessage, &p.SeriesID, &p.EpisodeNumber, &seriesName); err != nil {
		return domain.Project{}, err
	}
	if err := json.Unmarshal([]byte(checkpoints), &p.CustomCheckpoints); err != nil {
		return domain.Project{}, fmt.Errorf("decode custom_checkpoints: %w", err)
	}
	t, err := parseTime(created)
	if err != nil {
		return domain.Project{}, err
	}
	p.CreatedAt = t
	p.Name = domain.ProjectName(subject, firstMessage)
	if p.Name == domain.UntitledProject && p.SeriesID != "" {
		p.Name = domain.EpisodeName(seriesName, p.EpisodeNumber)
	}
	return p, nil
}

// Get returns the Project or ErrNotFound.
func (r *ProjectRepository) Get(ctx context.Context, id string) (domain.Project, error) {
	p, err := scanProject(r.db.Reader().QueryRowContext(ctx, projectSelect+` WHERE p.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Project{}, ErrNotFound
	}
	if err != nil {
		return domain.Project{}, fmt.Errorf("get project: %w", err)
	}
	return p, nil
}

// List returns every Project, newest first.
func (r *ProjectRepository) List(ctx context.Context) ([]domain.Project, error) {
	rows, err := r.db.Reader().QueryContext(ctx, projectSelect+` ORDER BY p.created_at DESC, p.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var projects []domain.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// GetBrief returns the Brief of a Project or ErrNotFound.
func (r *ProjectRepository) GetBrief(ctx context.Context, projectID string) (domain.Brief, error) {
	var content string
	err := r.db.Reader().QueryRowContext(ctx, `SELECT content FROM brief WHERE project_id = ?`, projectID).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Brief{}, ErrNotFound
	}
	if err != nil {
		return domain.Brief{}, fmt.Errorf("get brief: %w", err)
	}
	var b domain.Brief
	if err := json.Unmarshal([]byte(content), &b); err != nil {
		return domain.Brief{}, fmt.Errorf("decode brief: %w", err)
	}
	return b, nil
}

func nonNilCheckpoints(c []domain.Checkpoint) []domain.Checkpoint {
	if c == nil {
		return []domain.Checkpoint{}
	}
	return c
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", s, err)
	}
	return t, nil
}
