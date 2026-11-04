// Package editorerrtest checks that a failure of ffmpeg in the Rough Cut, the export and the composing of a Take reaches the screens and the
// tools as the common error (#1010): the code, a cause and a remedy in Japanese, and the message of ffmpeg only as the detail.
package editorerrtest_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editortest"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// syncBuffer is a log destination that the services (other goroutines) and the test both use.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// previewDir is the folder of the Rough Cuts in the assets directory (previewcache).
const previewDir = ".previews"

// atReady brings the Project to READY (the export can run) with the log written to the returned buffer.
func atReady(t *testing.T) (*editortest.Fixture, *syncBuffer) {
	t.Helper()
	logs := &syncBuffer{}
	f := editortest.New(t, func(*mcptest.Env, *mcpkit.Registrar) {}, func(d *service.Deps) {
		d.Logger = slog.New(slog.NewTextHandler(logs, nil))
	})
	f.WithScript()
	ctx := context.Background()
	v, err := f.Services.Editor.RoughCut(ctx, f.ProjectID, "")
	require.NoError(t, err)
	_, err = f.Services.Editor.JudgeTimeline(ctx, v.Timeline.ID)
	require.NoError(t, err)
	_, err = f.Services.Editor.GenerateAudioWith(ctx, f.ProjectID, "", service.AudioOptions{})
	require.NoError(t, err)
	_, err = f.Services.Editor.FinalQC(ctx, f.ProjectID, "")
	require.NoError(t, err)
	require.Equal(t, "READY", f.State())
	return f, logs
}

// blockExports puts a file where the folder of the exports goes: the Takes are readable and ffmpeg runs, but cannot write its output.
func blockExports(t *testing.T, f *editortest.Fixture) {
	t.Helper()
	dir := filepath.Join(f.AssetsDir, f.ProjectID)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "exports"), []byte("a file, not a folder"), 0o600))
}

// An export whose ffmpeg run fails tells the cause and the remedy in Japanese with the code media_failed, an error ID that the log has, and
// what ffmpeg printed only as the detail; the tool error has the same code in its second line (the first line keeps the form it always had).
func TestExport_AFailedFFmpegRunIsTheCommonError(t *testing.T) {
	f, logs := atReady(t)
	blockExports(t, f)

	_, err := f.Services.Editor.Export(context.Background(), f.ProjectID, service.ExportInput{})

	var ai *service.AIError
	require.ErrorAs(t, err, &ai)
	e, ok := apperr.As(err)
	require.True(t, ok)
	assert.Equal(t, apperr.CodeMediaFailed, e.Code)
	require.NotEmpty(t, e.ID)
	assert.Contains(t, ai.Reason, "動画の書き出しに失敗しました")
	assert.Contains(t, ai.Reason, "もう一度実行してください")
	assert.Contains(t, ai.Reason, e.ID)
	assert.NotEmpty(t, e.Detail, "what ffmpeg printed")
	assert.Contains(t, logs.String(), "error_id="+e.ID, "the log line has the ID of the screen")

	te := mcpkit.MapError(err)
	assert.Equal(t, "upstream", te.Code)
	require.NotNil(t, te.Failure)
	assert.Equal(t, apperr.CodeMediaFailed, te.Failure.Code)
	assert.Equal(t, e.ID, te.Failure.ErrorID)
	assert.Equal(t, e.Detail, te.Failure.Detail)
	assert.Contains(t, te.Failure.Remedy, "素材の動画が壊れていないか")
	_, second, found := strings.Cut(te.Error(), "\n")
	require.True(t, found, "the second line is the structure")
	assert.Contains(t, second, `"code":"media_failed"`)
	assert.True(t, strings.HasPrefix(te.Error(), "upstream: "), "the first line is <class>: <message>")
}

// The Rough Cut that the preview plays is made by the same ffmpeg and fails the same way.
func TestPreview_AFailedRoughCutIsTheCommonError(t *testing.T) {
	f, _ := atReady(t)
	latest, err := f.Editors().LatestEditTimeline(context.Background(), f.ProjectID, "")
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(filepath.Join(f.AssetsDir, previewDir)))
	require.NoError(t, os.WriteFile(filepath.Join(f.AssetsDir, previewDir), []byte("a file, not a folder"), 0o600))

	_, err = f.Services.Editor.Preview(context.Background(), latest.ID)

	e, ok := apperr.As(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, apperr.CodeMediaFailed, e.Code)
	var ai *service.AIError
	require.ErrorAs(t, err, &ai)
	assert.Contains(t, ai.Reason, "Rough Cut の書き出しに失敗しました")
	assert.False(t, errors.Is(err, service.ErrUnavailable))
}
