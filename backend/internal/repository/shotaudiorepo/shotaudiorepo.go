// Package shotaudiorepo ties an audio asset of the Project to a Shot (Issue #1153): it writes the "audio" key of shot.shot_spec (domain/shotaudio) and lists the audio assets a
// Shot may be tied to. It is a sub-package of repository, which is at its line limit. Every rule of the tie is judged here, in one transaction with the write, so that two
// requests cannot both pass a check and leave a Shot tied to an asset of another Project, to a file that is no audio, or changed under a LOCK.
package shotaudiorepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/shotaudio"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ErrNotAudio means the asset exists in the Project but is not an audio file (an image, a video, a reference).
var ErrNotAudio = errors.New("shotaudiorepo: the asset is not an audio file")

// Repository writes the audio of Shots.
type Repository struct{ db *repository.DB }

// New returns a Repository backed by db.
func New(db *repository.DB) *Repository { return &Repository{db: db} }

// Set ties the Shot to the audio asset, or removes the tie when assetID is "". It returns the asset ID the Shot had (""). The rules, in this order:
//   - an unknown Shot, or an asset that is not in the Shot's Project (also one of another Project), is repository.ErrNotFound;
//   - a Shot that is being generated or repaired is repository.ErrShotBusy: the generation reads the Shot's audio and a change in the middle would not be the one used;
//   - an asset that is not an audio file is ErrNotAudio;
//   - a Shot whose Prompt is LOCKed is a *repository.LockedParametersError (Is repository.ErrLocked): the audio is part of what the clip is made from, as the Canonical
//     Shot Spec is, and a hand edit of the spec is refused the same way.
//
// Clearing follows the same rules (a LOCKed Shot keeps its audio).
func (r *Repository) Set(ctx context.Context, shotID, assetID string) (previous string, err error) {
	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var (
			raw, state, projectID string
		)
		err := tx.QueryRowContext(ctx,
			`SELECT sh.shot_spec, sh.state, s.project_id FROM shot sh JOIN scene s ON s.id = sh.scene_id WHERE sh.id = ?`, shotID).Scan(&raw, &state, &projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return repository.ErrNotFound
		}
		if err != nil {
			return err
		}
		if !domain.ShotState(state).CanEditSpec() {
			return repository.ErrShotBusy
		}
		if assetID != "" {
			var typ string
			err := tx.QueryRowContext(ctx, `SELECT type FROM asset WHERE id = ? AND project_id = ?`, assetID, projectID).Scan(&typ)
			if errors.Is(err, sql.ErrNoRows) {
				return repository.ErrNotFound
			}
			if err != nil {
				return err
			}
			if domain.AssetType(typ) != domain.AssetAudio {
				return ErrNotAudio
			}
		}
		var mode string
		err = tx.QueryRowContext(ctx,
			`SELECT mode FROM lock WHERE target_type = ? AND target_id = ? AND parameter_name = ?`, domain.LockTargetShot, shotID, domain.ParamPrompt).Scan(&mode)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if domain.LockMode(mode) == domain.LockLocked {
			return &repository.LockedParametersError{Parameters: []domain.LockParameter{domain.ParamPrompt}}
		}
		previous = shotaudio.ID([]byte(raw))
		if previous == assetID {
			return nil
		}
		next, err := shotaudio.With([]byte(raw), assetID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE shot SET shot_spec = ? WHERE id = ?`, string(next), shotID)
		return err
	})
	if errors.Is(err, repository.ErrNotFound) || errors.Is(err, repository.ErrShotBusy) || errors.Is(err, ErrNotAudio) || errors.Is(err, repository.ErrLocked) {
		return previous, err
	}
	if err != nil {
		return "", fmt.Errorf("set shot audio: %w", err)
	}
	return previous, nil
}

// Choices are the audio assets of the Project a Shot may be tied to, newest first.
func (r *Repository) Choices(ctx context.Context, projectID string) ([]domain.Asset, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT id, project_id, type, file_path, COALESCE(mime_type, ''), COALESCE(size_bytes, 0), created_at FROM asset WHERE project_id = ? AND type = ? ORDER BY created_at DESC, id DESC`,
		projectID, domain.AssetAudio)
	if err != nil {
		return nil, fmt.Errorf("list audio assets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Asset
	for rows.Next() {
		var (
			a       domain.Asset
			typ, at string
		)
		if err := rows.Scan(&a.ID, &a.ProjectID, &typ, &a.FilePath, &a.MimeType, &a.SizeBytes, &at); err != nil {
			return nil, fmt.Errorf("scan audio asset: %w", err)
		}
		a.Type = domain.AssetType(typ)
		if a.CreatedAt, err = repository.ParseTime(at); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list audio assets: %w", err)
	}
	return out, nil
}
