package voicetrain

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/media"
)

// noFFprobe is the refusal of a clip while ffprobe, which measures its length, is not found.
func noFFprobe() error {
	return conflictf("音声の長さを測る ffprobe が見つからないため、学習に使う音声を受け付けられません。モデルマネージャーの「付随ソフト」からダウンロードするか、インストールしてください")
}

// length is the length of the clip file abs in seconds (name is what the user called it). ffprobe can be set up while mam runs: one that
// was not found when mam started and still is not (media.ErrUnavailable) is the same refusal as having none.
func (s *Service) length(ctx context.Context, abs, name string) (float64, error) {
	secs, err := s.Probe.AudioDuration(ctx, abs)
	switch {
	case errors.Is(err, media.ErrUnavailable):
		return 0, noFFprobe()
	case err != nil:
		return 0, invalidf("音声として読み取れませんでした（%s の長さを測れません）", filepath.Base(name))
	}
	return secs, nil
}
