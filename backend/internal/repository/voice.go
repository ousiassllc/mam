package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// VoiceRepository persists the cloned voices of a Project (voice_clone).
type VoiceRepository struct {
	db  *DB
	now func() time.Time
}

// NewVoiceRepository returns a VoiceRepository backed by db.
func NewVoiceRepository(db *DB) *VoiceRepository {
	return &VoiceRepository{db: db, now: time.Now}
}

// Save stores the cloned voice of the speaker. A speaker has one voice: a new clone replaces the
// old one. A Project that does not exist is ErrNotFound.
func (r *VoiceRepository) Save(ctx context.Context, projectID, speaker, name, voiceID string) (domain.VoiceClone, error) {
	clone := domain.VoiceClone{ID: domain.NewID(), ProjectID: projectID, Speaker: speaker, Name: name, VoiceID: voiceID, CreatedAt: r.now().UTC()}
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := projectExists(ctx, tx, projectID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM voice_clone WHERE project_id = ? AND speaker = ?`, projectID, speaker); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO voice_clone (id, project_id, speaker, name, voice_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			clone.ID, projectID, speaker, name, voiceID, formatTime(clone.CreatedAt))
		return err
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.VoiceClone{}, err
		}
		return domain.VoiceClone{}, fmt.Errorf("save voice clone: %w", err)
	}
	return clone, nil
}

// List returns the cloned voices of the Project, by speaker.
func (r *VoiceRepository) List(ctx context.Context, projectID string) ([]domain.VoiceClone, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT id, speaker, name, voice_id, created_at FROM voice_clone WHERE project_id = ? ORDER BY speaker`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list voice clones: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.VoiceClone
	for rows.Next() {
		c := domain.VoiceClone{ProjectID: projectID}
		var created string
		if err := rows.Scan(&c.ID, &c.Speaker, &c.Name, &c.VoiceID, &created); err != nil {
			return nil, fmt.Errorf("scan voice clone: %w", err)
		}
		if c.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
