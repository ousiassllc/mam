package locallipsync_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/musetalk"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editortest"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lipsync"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/locallipsync"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// manager is the runtime manager as the lip sync sees it: it makes the clip it is given the video of the mouth.
type manager struct {
	mu         sync.Mutex
	prepareErr error
	prepared   int
	requests   []lipsync.Request
}

func (m *manager) Prepare(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prepared++
	return m.prepareErr
}

func (m *manager) LipSync(_ context.Context, req lipsync.Request) (lipsync.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, req)
	m.mu.Unlock()
	data, err := os.ReadFile(req.VideoPath)
	if err != nil {
		return lipsync.Response{}, err
	}
	return lipsync.Response{OutputPath: req.OutputPath}, os.WriteFile(req.OutputPath, data, 0o600)
}

// atAudio is the editor fixture with the local lip sync (over the fake manager), a script of a narration and a line of ユイ, the quality
// policy MAX QUALITY and the Project at AUDIO.
func atAudio(t *testing.T) (*editortest.Fixture, *manager) {
	t.Helper()
	tools, err := media.Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	m := &manager{}
	f := editortest.New(t, func(*mcptest.Env, *mcpkit.Registrar) {}, func(d *service.Deps) {
		d.Audio.LocalLipSync = musetalk.New(locallipsync.Runner{Runtime: m}, musetalk.Config{FFmpegPath: tools.FFmpegPath(), WorkDir: t.TempDir()})
	})
	f.WithScript()
	f.Exec(`UPDATE project SET quality_policy = 'max_quality' WHERE id = ?`, f.ProjectID)
	ctx := context.Background()
	v, err := f.Services.Editor.RoughCut(ctx, f.ProjectID, "")
	require.NoError(t, err)
	_, err = f.Services.Editor.JudgeTimeline(ctx, v.Timeline.ID)
	require.NoError(t, err)
	require.Equal(t, "AUDIO", f.State())
	return f, m
}

func ledger(t *testing.T, f *editortest.Fixture) map[string][]float64 {
	t.Helper()
	rows, err := f.DB.Reader().QueryContext(context.Background(), `SELECT provider, COALESCE(estimated_cost, 0) FROM cost_ledger_entry WHERE project_id = ?`, f.ProjectID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	out := map[string][]float64{}
	for rows.Next() {
		var p string
		var c float64
		require.NoError(t, rows.Scan(&p, &c))
		out[p] = append(out[p], c)
	}
	require.NoError(t, rows.Err())
	return out
}

func latest(t *testing.T, f *editortest.Fixture) domain.AudioTimeline {
	t.Helper()
	v, err := f.Editors().LatestAudioTimeline(context.Background(), f.ProjectID, "")
	require.NoError(t, err)
	return v.Timeline
}

// The local lip sync makes the clip with ユイ's line, the Audio Timeline records it where Sync Labs' goes, the ledger keeps it at no cost,
// and the export plays that video in the place of the Take: the file is the one Export reads (it refuses when the file is gone).
func TestEditor_TheLocalLipSyncMakesTheVideoAndExportPlaysIt(t *testing.T) {
	f, m := atAudio(t)
	ctx := context.Background()

	_, err := f.Services.Editor.GenerateAudioWith(ctx, f.ProjectID, "", service.AudioOptions{LipSyncProvider: localvoice.LipSyncLocal})

	require.NoError(t, err)
	assert.Equal(t, 1, m.prepared, "the runtime is started once")
	require.Len(t, m.requests, 1, "only the clip with a character's line")
	tl := latest(t, f)
	require.Len(t, tl.LipSync, 1)
	ls := tl.LipSync[0]
	assert.Equal(t, domain.ModelMuseTalk, ls.Model)
	assert.Equal(t, 1, ls.ClipIndex)
	assert.FileExists(t, filepath.Join(f.AssetsDir, filepath.FromSlash(ls.Path)))
	got := ledger(t, f)
	assert.Equal(t, []float64{0}, got[string(domain.ModelMuseTalk)])
	assert.NotContains(t, got, string(domain.ModelSyncLabs))

	_, err = f.Services.Editor.FinalQC(ctx, f.ProjectID, "")
	require.NoError(t, err)
	_, err = f.Services.Editor.Export(ctx, f.ProjectID, service.ExportInput{})
	require.NoError(t, err, "the export is made with the video of the mouth")

	require.NoError(t, os.Remove(filepath.Join(f.AssetsDir, filepath.FromSlash(ls.Path))))
	_, err = f.Services.Editor.Export(ctx, f.ProjectID, service.ExportInput{})
	require.Error(t, err, "the export reads the video of the local lip sync, as it does Sync Labs'")
	assert.Contains(t, err.Error(), "Lip Sync の映像ファイルが見つかりません")
}

// A runtime that is not installed is told in Skipped, once; the audio is made and nothing is recorded for the lip sync.
func TestEditor_ARuntimeThatIsNotInstalledLeavesTheLipSyncOutWithItsReason(t *testing.T) {
	f, m := atAudio(t)
	m.prepareErr = &localvoice.UnavailableError{Kind: localvoice.KindNotInstalled, Vendor: localvoice.VendorMuseTalk, Detail: "MuseTalk は導入されていません"}

	_, err := f.Services.Editor.GenerateAudioWith(context.Background(), f.ProjectID, "", service.AudioOptions{LipSyncProvider: localvoice.LipSyncLocal})

	require.NoError(t, err)
	tl := latest(t, f)
	assert.Empty(t, tl.LipSync)
	assert.NotEmpty(t, tl.MixPath, "the audio is made")
	var lip []domain.SkippedAudio
	for _, s := range tl.Skipped {
		if s.Layer == domain.LayerLipSync {
			lip = append(lip, s)
		}
	}
	require.Len(t, lip, 1)
	assert.Contains(t, lip[0].Detail, "導入されていません")
	assert.Empty(t, m.requests)
	assert.NotContains(t, ledger(t, f), string(domain.ModelMuseTalk))
}

// A run that does not choose is Sync Labs, as before: the local runtime is never asked (with no Sync Labs here, the step says so).
func TestEditor_WithoutAChoiceTheLipSyncIsSyncLabsAndTheLocalRuntimeIsLeftAlone(t *testing.T) {
	f, m := atAudio(t)

	_, err := f.Services.Editor.GenerateAudio(context.Background(), f.ProjectID, "")

	require.NoError(t, err)
	assert.Zero(t, m.prepared)
	assert.Empty(t, m.requests)
	tl := latest(t, f)
	assert.Empty(t, tl.LipSync)
	var detail string
	for _, s := range tl.Skipped {
		if s.Layer == domain.LayerLipSync {
			detail = s.Detail
		}
	}
	assert.Contains(t, detail, "Lip Sync のプロバイダーが設定されていません")
}

func TestEditor_AnUnknownLipSyncProviderIsAValidationError(t *testing.T) {
	f, m := atAudio(t)

	_, err := f.Services.Editor.GenerateAudioWith(context.Background(), f.ProjectID, "", service.AudioOptions{LipSyncProvider: "azure"})

	var ve *service.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, "lip_sync_provider", ve.Field)
	assert.Zero(t, m.prepared)
	assert.Empty(t, ledger(t, f), "nothing was started or paid")
	assert.Equal(t, "AUDIO", f.State())
}
