package audiotest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

// asset registers a file as an asset of the Project, the way an attachment of the chat is stored.
func (r *rig) asset(project string, typ domain.AssetType, ext, mime string, data []byte) string {
	r.T.Helper()
	id := domain.NewID()
	rel := filepath.Join(project, id+ext)
	require.NoError(r.T, os.MkdirAll(filepath.Join(r.GT.AssetsDir, project), 0o700))
	require.NoError(r.T, os.WriteFile(filepath.Join(r.GT.AssetsDir, rel), data, 0o600))
	require.NoError(r.T, r.GT.Repo.AddProjectAsset(context.Background(), domain.Asset{
		ID: id, ProjectID: project, Type: typ, FilePath: filepath.ToSlash(rel), MimeType: mime, SizeBytes: int64(len(data)),
	}))
	return id
}

// projectBatch is r.s2vBatch with the sound taken from an asset of a Project (source project_asset).
func (r *rig) projectBatch(project, assetID string) map[string]any {
	r.T.Helper()
	args := r.s2vBatch(gentesttest.S2VModel, "")
	args["references"] = append(args["references"].([]map[string]any), map[string]any{"source": "project_asset", "project_id": project, "id": assetID, "role": "audio"})
	return args
}

// An audio asset of a Project is the sound of a batch (source project_asset, role audio): it reaches the model with the type of its bytes, and the batch records
// where it came from. A picture of the Project, an asset of another Project and a missing one are refused with the words that say why, and nothing is sent.
func TestRunTakesTheSoundFromAnAudioAssetOfAProject(t *testing.T) {
	r := newRig(t)
	project := r.GT.NewProject()
	data := audiofiletest.OGGOpus(312, 48000)
	assetID := r.asset(project, domain.AssetAudio, ".ogg", audiofile.OGG, data)
	args := r.projectBatch(project, assetID)

	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)
	args["estimate_id"], args["confirmed_total_usd"] = est.ID, est.TotalUSD
	var out mcpgentest.RunOutput
	r.CallOK("run_generation_test", args, &out)
	r.WaitJob(out.JobID, 20*time.Second, 10*time.Millisecond)

	calls := r.GT.Fake.CallsOf(gentesttest.S2VModel)
	require.Len(t, calls, 1)
	require.NotNil(t, calls[0].Video.Audio)
	assert.Equal(t, data, calls[0].Video.Audio.Data)
	assert.Equal(t, audiofile.OGG, calls[0].Video.Audio.MimeType)

	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	require.Len(t, b.Results, 1)
	var got mcpgentest.ResultOutput
	r.CallOK("get_generation_result", map[string]any{"result_id": b.Results[0].ID}, &got)
	origins := map[string]string{}
	for _, in := range got.Inputs {
		origins[in.Role] = in.Origin.Kind
	}
	assert.Equal(t, "project_asset", origins["audio"])

	before := len(r.GT.Fake.CallsOf(gentesttest.S2VModel))
	for name, bad := range map[string]map[string]any{
		"a picture of the Project":       r.projectBatch(project, r.asset(project, domain.AssetImage, ".png", "image/png", gentesttest.PNG)),
		"an asset of another Project":    r.projectBatch(r.GT.NewProject(), assetID),
		"an asset that is not there":     r.projectBatch(project, "nope"),
		"a Library entry as the sound":   r.projectBatch(project, assetID),
		"no Project for a Project asset": r.projectBatch("", assetID),
	} {
		if name == "a Library entry as the sound" {
			bad["references"].([]map[string]any)[1]["source"] = "reference_item"
		}
		text := r.CallError("estimate_generation_test", bad)
		assert.Contains(t, text, "validation", name)
	}
	assert.Len(t, r.GT.Fake.CallsOf(gentesttest.S2VModel), before, "nothing was sent")
}
