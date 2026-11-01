package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/audiosummary"
)

func TestEditorService_AssetPathNeverLeavesTheAssetsDirectory(t *testing.T) {
	base := t.TempDir()
	assets := filepath.Join(base, "assets")
	require.NoError(t, os.MkdirAll(filepath.Join(assets, "p1"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(assets, "p1", "a.mp4"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(base, "secret.mp4"), []byte("x"), 0o600))
	s := &EditorService{core: &core{assetsDir: assets}}

	assert.Equal(t, filepath.Join(assets, "p1", "a.mp4"), s.assetPath("p1/a.mp4"))
	assert.True(t, s.fileExists("p1/a.mp4"))
	for _, rel := range []string{"", ".", "../secret.mp4", "p1/../../secret.mp4", "../../../etc/passwd", ".."} {
		assert.Empty(t, s.assetPath(rel), rel)
		assert.False(t, s.fileExists(rel), rel)
	}
}

func TestTakeSources_AFileIsProbedOnceHoweverManyClipsPlayIt(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(bin, "probes.log")
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		script := "#!/bin/sh\necho x >> " + log + "\necho '{\"format\":{\"duration\":\"3.5\"},\"streams\":[{\"codec_type\":\"video\",\"width\":1920,\"height\":1080,\"r_frame_rate\":\"24\"}]}'\n"
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700)) // #nosec G306 -- a test executable
	}
	t.Setenv("PATH", bin)
	tools, err := media.Look()
	require.NoError(t, err)
	assets := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(assets, "p1"), 0o750))
	for _, f := range []string{"a.mp4", "b.mp4"} {
		require.NoError(t, os.WriteFile(filepath.Join(assets, "p1", f), []byte("x"), 0o600))
	}
	s := &EditorService{core: &core{assetsDir: assets}, tools: &tools}
	ts := &takeSources{s: s, probed: map[string]probeResult{}, takes: map[string]repository.TakeDetail{
		"t1": {TakeInfo: domain.TakeInfo{Take: domain.Take{ID: "t1", ShotID: "sh1", FilePath: "p1/a.mp4"}}},
		"t2": {TakeInfo: domain.TakeInfo{Take: domain.Take{ID: "t2", ShotID: "sh1", FilePath: "p1/b.mp4"}}},
		"t3": {TakeInfo: domain.TakeInfo{Take: domain.Take{ID: "t3", ShotID: "sh1", FilePath: "p1/gone.mp4"}}},
	}}

	for _, id := range []string{"t1", "t1", "t2", "t1", "t3", "t3", "unknown"} {
		src, ok := ts.take(context.Background(), id)
		assert.Equal(t, id == "t1" || id == "t2", ok, id)
		if ok {
			assert.InDelta(t, 3.5, src.seconds, 0)
		}
	}

	probes, err := os.ReadFile(log)
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(probes), "x"), "t1 and t2 are probed once each; a missing file is not probed")
}

func TestAudioSummary_SkippedLayersAreListedAlsoWhenThereIsNoMix(t *testing.T) {
	skipped := []domain.SkippedAudio{
		{Layer: domain.LayerBGM, Prompt: "静かな曲", Reason: domain.SkipError, Detail: "API キーがありません"},
		{Layer: domain.LayerSE, Reason: domain.SkipPolicy},
	}
	for name, mix := range map[string]string{"no mix": "", "with mix": "audio/mix.wav"} {
		got := audiosummary.Of(domain.AudioTimeline{MixPath: mix, Skipped: skipped}, false)
		assert.Contains(t, got, "作れなかった音", name)
		assert.Contains(t, got, "BGM「静かな曲」（生成に失敗: API キーがありません）", name)
		assert.NotContains(t, got, "SE", name, "a layer the Quality Policy chose not to pay for is not a failure")
	}
	assert.NotContains(t, audiosummary.Of(domain.AudioTimeline{}, false), "作れなかった音")
}
