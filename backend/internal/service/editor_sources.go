package service

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// cutSource is what a clip needs of its Take.
type cutSource struct {
	takeID  string
	path    string // relative to the assets directory
	seconds float64
}

// takeSources is what one editing operation knows of the Takes it works on: the Takes come from one
// query however many clips there are, and a file is probed at most once (two clips of one Take
// share the result). It is made per operation and never shared between goroutines.
type takeSources struct {
	s      *EditorService
	takes  map[string]repository.TakeDetail
	probed map[string]probeResult // by the path relative to the assets directory
}

type probeResult struct {
	seconds float64
	ok      bool
}

// loadSources reads the Takes (empty IDs are ignored; unknown ones are simply not there).
func (s *EditorService) loadSources(ctx context.Context, takeIDs []string) (*takeSources, error) {
	ids := make([]string, 0, len(takeIDs))
	seen := make(map[string]bool, len(takeIDs))
	for _, id := range takeIDs {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	takes, err := s.takes.GetMany(ctx, ids)
	if err != nil {
		return nil, err
	}
	return &takeSources{s: s, takes: takes, probed: map[string]probeResult{}}, nil
}

// clipTakeIDs are the Takes the clips play.
func clipTakeIDs(clips []domain.EditClip) []string {
	ids := make([]string, len(clips))
	for i, c := range clips {
		ids[i] = c.TakeID
	}
	return ids
}

// selectedTakeIDs are the Selected Takes of the Shots.
func selectedTakeIDs(shots []repository.ShotRef) []string {
	ids := make([]string, len(shots))
	for i, r := range shots {
		ids[i] = r.Shot.SelectedTakeID
	}
	return ids
}

// take reads the Take and its length. The second result is false when the Take is unknown, its
// file is gone (or outside the assets directory) or ffprobe cannot read it.
func (ts *takeSources) take(ctx context.Context, takeID string) (cutSource, bool) {
	d, ok := ts.takes[takeID]
	if !ok {
		return cutSource{}, false
	}
	src := cutSource{takeID: takeID, path: d.FilePath, seconds: d.DurationSeconds}
	if !ts.s.fileExists(d.FilePath) {
		return src, false
	}
	if ts.s.tools == nil {
		return src, true
	}
	p, done := ts.probed[d.FilePath]
	if !done {
		info, err := ts.s.tools.Probe(ctx, ts.s.assetPath(d.FilePath))
		p = probeResult{seconds: info.Duration, ok: err == nil} // unreadable: as good as gone
		ts.probed[d.FilePath] = p
	}
	if !p.ok {
		return src, false
	}
	src.seconds = p.seconds
	return src, true
}

// selected reads the Selected Take of the Shot. The second result is false when the Shot has none
// or its file is gone.
func (ts *takeSources) selected(ctx context.Context, sh domain.Shot) (cutSource, bool) {
	return ts.take(ctx, sh.SelectedTakeID)
}

// shotOf is the Shot the Take belongs to ("" for an unknown Take).
func (ts *takeSources) shotOf(takeID string) string { return ts.takes[takeID].ShotID }
