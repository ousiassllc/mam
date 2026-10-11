package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// NewPromotion is a proposal to promote a note version into the World State.
type NewPromotion struct {
	NoteVersionID string
	// Proposed are the changes that can be applied, Conflicts those left out because the field is LOCKed.
	Proposed  domain.StateDiff
	Conflicts domain.StateDiff
}

// CreatePromotion stores the proposal for a version of a Project note together with its Approval
// card (chat_message with ref_type note_promotion; the caller sets Content), in one transaction. The
// card is non-blocking: the Project's state is not touched. It returns ErrNotFound for an unknown
// version or one of a Series note, and ErrConflict when the version was proposed before (a version
// is proposed once).
func (r *NoteRepository) CreatePromotion(ctx context.Context, p NewPromotion, card domain.ChatMessage) (domain.NotePromotion, domain.ChatMessage, error) {
	proposed, err := json.Marshal(nonNilDiff(p.Proposed))
	if err != nil {
		return domain.NotePromotion{}, domain.ChatMessage{}, err
	}
	conflicts, err := json.Marshal(nonNilDiff(p.Conflicts))
	if err != nil {
		return domain.NotePromotion{}, domain.ChatMessage{}, err
	}
	id := domain.NewID()
	now := formatTime(r.now())
	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var projectID string
		err := tx.QueryRowContext(ctx,
			`SELECT n.project_id FROM note_version v JOIN note n ON n.id = v.note_id WHERE v.id = ? AND n.project_id IS NOT NULL`, p.NoteVersionID).Scan(&projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := exists(ctx, tx, `SELECT 1 FROM note_promotion WHERE note_version_id = ?`, p.NoteVersionID); err == nil {
			return ErrConflict
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO note_promotion (id, note_version_id, proposed_diff, locked_conflicts, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, p.NoteVersionID, string(proposed), string(conflicts), domain.PromotionPending, now); err != nil {
			return fmt.Errorf("insert note promotion: %w", err)
		}
		card.ID, card.ProjectID = domain.NewID(), projectID
		card.Role, card.Kind = domain.RoleAssistant, domain.KindApproval
		card.RefType, card.RefID, card.ApprovalStatus = domain.RefNotePromotion, id, domain.ApprovalPending
		return insertMessage(ctx, tx, card, now)
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
		return domain.NotePromotion{}, domain.ChatMessage{}, err
	}
	if err != nil {
		return domain.NotePromotion{}, domain.ChatMessage{}, fmt.Errorf("create note promotion: %w", err)
	}
	promo, err := r.Promotion(ctx, id)
	return promo, card, err
}

func nonNilDiff(d domain.StateDiff) domain.StateDiff {
	if d.Changes == nil {
		d.Changes = []domain.StateChange{}
	}
	return d
}

// Promotion returns a promotion (ErrNotFound for an unknown one).
func (r *NoteRepository) Promotion(ctx context.Context, id string) (domain.NotePromotion, error) {
	return readPromotion(ctx, r.db.Reader(), id)
}

func readPromotion(ctx context.Context, q queryer, id string) (domain.NotePromotion, error) {
	var (
		p                      domain.NotePromotion
		proposed, conflicts    string
		worldVersion, resolved sql.NullString
		createdAt              string
	)
	err := q.QueryRowContext(ctx,
		`SELECT p.id, p.note_version_id, v.note_id, COALESCE(n.project_id, ''), v.version_number, p.proposed_diff, p.locked_conflicts, p.status, p.world_state_version_id, p.created_at, p.resolved_at
		 FROM note_promotion p JOIN note_version v ON v.id = p.note_version_id JOIN note n ON n.id = v.note_id WHERE p.id = ?`, id).
		Scan(&p.ID, &p.NoteVersionID, &p.NoteID, &p.ProjectID, &p.VersionNumber, &proposed, &conflicts, &p.Status, &worldVersion, &createdAt, &resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, fmt.Errorf("get note promotion: %w", err)
	}
	if err := json.Unmarshal([]byte(proposed), &p.Proposed); err != nil {
		return p, fmt.Errorf("decode proposed diff: %w", err)
	}
	if err := json.Unmarshal([]byte(conflicts), &p.Conflicts); err != nil {
		return p, fmt.Errorf("decode locked conflicts: %w", err)
	}
	p.WorldStateVersionID = worldVersion.String
	if p.CreatedAt, err = parseTime(createdAt); err != nil {
		return p, err
	}
	if resolved.Valid {
		t, err := parseTime(resolved.String)
		if err != nil {
			return p, err
		}
		p.ResolvedAt = &t
	}
	return p, nil
}

// ResolvePromotion answers a pending promotion. Rejecting only marks it rejected. Approving writes
// its changes into the Project's trunk World State and appends a world_state_version, in one
// transaction (F-31, spec/spec.md §9.4): the LOCK is judged again on the data being written, and a
// change whose field is LOCKed now, whose entity is gone, or that the field already has, is left
// out (never an error). A promotion that leaves nothing to write appends no version. It returns
// ErrNotFound for an unknown promotion and ErrConflict for one that was answered already.
func (r *NoteRepository) ResolvePromotion(ctx context.Context, id string, approve bool) (domain.PromotionResult, error) {
	var res domain.PromotionResult
	now := formatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		p, err := readPromotion(ctx, tx, id)
		if err != nil {
			return err
		}
		if p.Status != domain.PromotionPending {
			return ErrConflict
		}
		if !approve {
			_, err := tx.ExecContext(ctx, `UPDATE note_promotion SET status = ?, resolved_at = ? WHERE id = ?`, domain.PromotionRejected, now, id)
			return err
		}
		var versionID any
		if res, versionID, err = r.applyPromotion(ctx, tx, p); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE note_promotion SET status = ?, world_state_version_id = ?, resolved_at = ? WHERE id = ?`,
			domain.PromotionApplied, versionID, now, id)
		return err
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
		return domain.PromotionResult{}, err
	}
	if err != nil {
		return domain.PromotionResult{}, fmt.Errorf("resolve note promotion: %w", err)
	}
	return res, nil
}

func (r *NoteRepository) applyPromotion(ctx context.Context, tx *sql.Tx, p domain.NotePromotion) (domain.PromotionResult, any, error) {
	res := domain.PromotionResult{Applied: domain.StateDiff{Changes: []domain.StateChange{}}, Skipped: domain.StateDiff{Changes: []domain.StateChange{}}}
	before, err := readWorld(ctx, tx, p.ProjectID, "")
	if err != nil {
		return res, nil, err
	}
	locks, err := readLocks(ctx, tx, p.ProjectID)
	if err != nil {
		return res, nil, err
	}
	for _, c := range p.Proposed.Changes {
		old, found := before.Current(c.Target, c.ID, c.Field)
		switch {
		case !domain.IsEditable(c.Target, c.Field) || domain.ValidateFieldValue(c.Target, c.Field, c.To) != nil:
			continue // not something a promotion may write
		case !found || locks.Locked(c.Target, c.ID, c.Field):
			res.Skipped.Changes = append(res.Skipped.Changes, c)
			continue
		case domain.SameValue(old, c.To):
			continue
		}
		var text string
		if err := json.Unmarshal(c.To, &text); err != nil {
			continue
		}
		if err := setField(ctx, tx, worldTables[c.Target], c.ID, c.Field, text); err != nil {
			return res, nil, err
		}
		c.From = old
		res.Applied.Changes = append(res.Applied.Changes, c)
	}
	if len(res.Applied.Changes) == 0 {
		return res, nil, nil
	}
	after, err := readWorld(ctx, tx, p.ProjectID, "")
	if err != nil {
		return res, nil, err
	}
	versionID, number, err := r.world.appendVersion(ctx, tx, p.ProjectID, "", after)
	if err != nil {
		return res, nil, err
	}
	res.Version = number
	return res, versionID, nil
}
