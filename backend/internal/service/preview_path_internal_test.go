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
)

func TestPreviewPath_StaysDirectlyInsideThePreviewDirectory(t *testing.T) {
	assets := filepath.Join(t.TempDir(), "assets")
	c := &core{assetsDir: assets}

	assert.Equal(t, filepath.Join(assets, previewDir, "0b9f1a52.mp4"), c.previewPath("0b9f1a52.mp4"))
	for _, name := range []string{
		"a/../../x.mp4", "../x.mp4", "../../../x.mp4", "sub/x.mp4", "edit-a/../../../x.mp4", "",
	} {
		assert.Empty(t, c.previewPath(name), name)
	}
}

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
	s := &EditorService{core: &core{assetsDir: assets}, tools: &tools}

	_, _, err = s.rough(context.Background(), domain.EditTimelineVersion{ID: "a/../../../x"})

	assert.ErrorIs(t, err, ErrNotFound)
	assert.NoFileExists(t, log, "no ffmpeg / ffprobe run for an unsafe ID")
	_, statErr := os.Stat(filepath.Join(data, "x.mp4"))
	assert.True(t, os.IsNotExist(statErr))
}
