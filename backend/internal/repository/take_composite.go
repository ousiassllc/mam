package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// NewComposite is a Take made from segments of other Takes of the Shot (Partial Take Use).
type NewComposite struct {
	ProjectID string
	ShotID    string
	// BaseTakeID is the first segment's Take: the composed Take takes its prompt version and model.
	BaseTakeID string
	Parameters json.RawMessage
	Asset      domain.Asset // Type video; ID and FilePath are set by the caller
	// Candidate leaves the Shot's selection alone: the Take is only added as a candidate (a Partial
	// Regeneration is the AI's proposal, not the user's choice).
	Candidate bool
}

// inheritLineage copies what the base Take was generated from (the World State version and the
// References, domain.TakeLineage) and how its model was chosen (the Multi-model routing decision)
// into the parameters of a composed Take, which is made from the base Take's prompt version and model
// and so from the same World State, References and routing. A base Take that predates a record has
// none to copy: the Generation Trace falls back as it does for that Take.
func inheritLineage(params json.RawMessage, base string) (json.RawMessage, error) {
	var from, to map[string]json.RawMessage
	if json.Unmarshal([]byte(base), &from) != nil {
		return params, nil
	}
	if err := json.Unmarshal(params, &to); err != nil {
		return nil, fmt.Errorf("decode composite parameters: %w", err)
	}
	for _, key := range []string{"world_state_version", "references", "routing"} {
		if v, ok := from[key]; ok {
			to[key] = v
		}
	}
	out, err := json.Marshal(to)
	if err != nil {
		return nil, fmt.Errorf("encode composite parameters: %w", err)
	}
	return out, nil
}

// CreateComposite stores the composed Take with its video asset. Unless it is a Candidate it is the
// user's choice: it becomes the Shot's selected Take with the status "selected". It returns the Take
// ID, or ErrNotFound when the base Take does not belong to the Shot.
func (r *TakeRepository) CreateComposite(ctx context.Context, nc NewComposite) (string, error) {
	takeID := domain.NewID()
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var promptVersionID, model, baseParams string
		err := tx.QueryRowContext(ctx,
			`SELECT t.prompt_version_id, t.model_name, COALESCE(t.model_parameters, '') FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id
			 WHERE t.id = ? AND pv.shot_id = ?`, nc.BaseTakeID, nc.ShotID).Scan(&promptVersionID, &model, &baseParams)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		params, err := inheritLineage(nc.Parameters, baseParams)
		if err != nil {
			return err
		}
		now := FormatTime(r.now())
		if err := insertAsset(ctx, tx, nc.Asset, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			takeID, promptVersionID, model, string(params), nc.Asset.FilePath, domain.TakeCandidate, now); err != nil {
			return fmt.Errorf("insert take: %w", err)
		}
		if nc.Candidate {
			return nil
		}
		return adoptTake(ctx, tx, nc.ShotID, takeID, domain.TakeSelected)
	})
	if errors.Is(err, ErrNotFound) {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("create composite take: %w", err)
	}
	return takeID, nil
}
