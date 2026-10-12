package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/previewcache"
)

func TestEditorService_RoughCutOfAHostileEditTimelineIDWritesNothingOutsidePreviews(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(bin, "ran.log")
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		script := "#!/bin/sh\necho x >> " + log + "\nexit 1\n"
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700)) // #nosec G306 -- a test executable
	}
	t.Setenv("PATH", bin)
	tools, err := media.Look()
	require.NoError(t, err)
	data := t.TempDir()
	assets := filepath.Join(data, "assets")
	require.NoError(t, os.MkdirAll(assets, 0o750))
	s := &EditorService{core: &core{assetsDir: assets, previews: previewcache.New(nil, assets, 0)}, tools: &tools}

	_, err = s.rough(context.Background(), domain.EditTimelineVersion{ID: "a/../../../x"})

	assert.ErrorIs(t, err, ErrNotFound)
	assert.NoFileExists(t, log, "no ffmpeg / ffprobe run for an unsafe ID")
	assert.NoFileExists(t, filepath.Join(data, "x.mp4"))
}
