package voicerefsvc

import (
	"context"
	"os"
	"path/filepath"
)

// RemoveForSeries removes the reference voice of a canonical Character (a new version of the canon; Episodes
// keep theirs). RemoveForProject does the same for a Character row of the Project. A Character that has none is
// left as it is.
func (s *Service) RemoveForSeries(ctx context.Context, seriesID, characterID string) error {
	return s.remove(ctx, owner{id: seriesID, series: true}, characterID)
}

// RemoveForProject removes the reference voice of a Character of the Project.
func (s *Service) RemoveForProject(ctx context.Context, projectID, characterID string) error {
	return s.remove(ctx, owner{id: projectID}, characterID)
}

func (s *Service) remove(ctx context.Context, o owner, characterID string) error {
	if err := s.exists(ctx, o); err != nil {
		return err
	}
	have, err := s.current(ctx, o, characterID)
	if err != nil || have == nil {
		return err
	}
	previous, err := s.write(ctx, o, characterID, nil)
	if err != nil {
		return err
	}
	s.discard(ctx, previous, nil)
	return nil
}

// Playback is the file of a reference voice, for the screen and the MCP download to serve.
type Playback struct {
	Path     string
	MimeType string
	// Name is a file name for a download.
	Name string
}

// PlaybackForSeries locates the recording of a canonical Character; PlaybackForProject that of a Character row
// of the Project. A Character without one, or whose file is gone, is ErrNotFound.
func (s *Service) PlaybackForSeries(ctx context.Context, seriesID, characterID string) (Playback, error) {
	return s.playback(ctx, owner{id: seriesID, series: true}, characterID)
}

// PlaybackForProject locates the recording of a Character row of the Project.
func (s *Service) PlaybackForProject(ctx context.Context, projectID, characterID string) (Playback, error) {
	return s.playback(ctx, owner{id: projectID}, characterID)
}

func (s *Service) playback(ctx context.Context, o owner, characterID string) (Playback, error) {
	ref, err := s.current(ctx, o, characterID)
	if err != nil {
		return Playback{}, err
	}
	if ref == nil {
		return Playback{}, ErrNotFound
	}
	abs := s.path(ref.FilePath)
	if _, err := os.Stat(abs); abs == "" || err != nil {
		return Playback{}, ErrNotFound
	}
	return Playback{Path: abs, MimeType: ref.MimeType, Name: "voice-reference-" + characterID + filepath.Ext(ref.FilePath)}, nil
}
