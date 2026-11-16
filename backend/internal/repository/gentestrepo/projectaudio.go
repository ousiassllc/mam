package gentestrepo

import (
	"context"
	"time"
)

// ProjectAudio is an audio file of a Project the generation test screen can take as the audio input of a speech-to-video model: an asset of type audio (Issue #1153).
type ProjectAudio struct {
	AssetID   string
	MimeType  string
	SizeBytes int64
	CreatedAt time.Time
}

// ProjectAudios lists the audio assets of a Project, newest first. It reads the Project's tables (read only).
func (r *Repository) ProjectAudios(ctx context.Context, projectID string) ([]ProjectAudio, error) {
	return queryAll(ctx, r, func(s scanner) (ProjectAudio, error) {
		var (
			a       ProjectAudio
			created string
		)
		if err := s.Scan(&a.AssetID, &a.MimeType, &a.SizeBytes, &created); err != nil {
			return ProjectAudio{}, err
		}
		var err error
		a.CreatedAt, err = parse(created)
		return a, err
	}, `SELECT id, COALESCE(mime_type, ''), COALESCE(size_bytes, 0), created_at
		FROM asset WHERE project_id = ? AND type = 'audio'
		ORDER BY created_at DESC, id DESC`, projectID)
}
