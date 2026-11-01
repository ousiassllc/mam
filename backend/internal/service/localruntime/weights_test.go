package localruntime_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

const (
	weightURLPath = "/w/org/repo/resolve/0123456789abcdef0123456789abcdef01234567/model.bin"
	weightDest    = "models/model.bin"
	extraURLPath  = "/w/org/repo/resolve/0123456789abcdef0123456789abcdef01234567/extra.bin"
	extraDest     = "models/extra.bin"
)

var (
	weightBytes = bytes.Repeat([]byte("w"), 5000)
	extraBytes  = bytes.Repeat([]byte("e"), 700)
)

func sum256(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// withWeights gives the runtime two weights served by the fake host: a required one and an optional one with an unclear license.
func withWeights() option {
	return func(r *rig, d *localruntime.Deps) {
		r.host.files[weightURLPath], r.host.files[extraURLPath] = weightBytes, extraBytes
		c := d.Catalog
		c.ResourceSets = []catalog.ResourceSet{{
			Runtime: "gpt-sovits",
			Licenses: []catalog.ResourceLicense{
				{ID: "mit", Name: "MIT", URL: "https://example.com/mit", Summary: "商用可", CommercialUse: catalog.CommercialYes},
				{ID: "odd", Name: "不明", URL: "https://example.com/odd", Summary: "表示なし", CommercialUse: catalog.CommercialUnclear, Notice: "商用で使う前に確認してください"},
			},
			Assets: []catalog.Resource{
				{
					Name: "model", Description: "本体の重み", Dest: weightDest, URL: r.host.URL + weightURLPath, SHA256: sum256(weightBytes), Size: int64(len(weightBytes)), Required: true,
					Source: catalog.ResourceSource{Name: "org/repo", URL: "https://huggingface.co/org/repo"}, License: "mit",
				},
				{
					Name: "extra", Description: "補助", Dest: extraDest, URL: r.host.URL + extraURLPath, SHA256: sum256(extraBytes), Size: int64(len(extraBytes)), Required: false,
					Source: catalog.ResourceSource{Name: "x", URL: "https://example.com/x", Note: "原本は x。2026-11-01 に SHA-256 の一致を確認した"}, License: "odd",
				},
			},
		}}
	}
}

func (r *rig) weightPath(dest string) string {
	return filepath.Join(r.layout.EnvDir(), filepath.FromSlash(dest))
}

func (r *rig) view(t *testing.T) localruntime.View {
	t.Helper()
	v, err := r.svc.Status(ctx(), "gpt-sovits")
	require.NoError(t, err)
	return v
}

func TestAnInstallFetchesTheWeightsAfterThePackageAndStartWaitsForThem(t *testing.T) {
	r := newRig(t, withWeights())
	before := r.view(t)
	assert.Equal(t, localruntime.WeightsNone, before.Weights.State, "nothing to check before the package is there")
	require.Len(t, before.Weights.Assets, 2)
	assert.Equal(t, 1, before.Weights.Unclear)
	assert.Equal(t, int64(len(weightBytes)+len(extraBytes)), before.Weights.TotalBytes)

	var phases []string
	var percents []int
	cancel := r.svc.Watch(func(e localruntime.Event) {
		if e.Job == nil {
			return
		}
		if n := len(phases); n == 0 || phases[n-1] != e.Job.Update.Phase {
			phases = append(phases, e.Job.Update.Phase)
		}
		percents = append(percents, e.Job.Update.Percent)
	})
	defer cancel()
	r.install()
	assert.Equal(t, []string{"download", "extract", "weights", "done"}, phases, "the weights come last, as a step of the same job")
	assert.IsNonDecreasing(t, percents, "the progress never goes back")

	got, err := os.ReadFile(r.weightPath(weightDest))
	require.NoError(t, err)
	assert.Equal(t, weightBytes, got)
	v := r.view(t)
	assert.Equal(t, localruntime.WeightsReady, v.Weights.State)
	assert.Zero(t, v.Weights.NotReady)
	assert.Empty(t, v.Weights.Message)
	for _, a := range v.Weights.Assets {
		assert.Equal(t, rtm.AssetOK, a.State, a.Name)
	}
	assert.Equal(t, "odd", v.Weights.Assets[1].License.ID)
	assert.Equal(t, catalog.CommercialUnclear, v.Weights.Assets[1].License.CommercialUse)
	assert.Equal(t, "https://huggingface.co/org/repo", v.Weights.Assets[0].Source.URL)

	st, err := r.svc.Start(ctx(), "gpt-sovits")
	require.NoError(t, err)
	assert.Equal(t, proc.StateRunning, st.State)
}

func TestARuntimeWhoseWeightIsMissingSaysSoBeforeItStartsAndInstallWeightsRepairsIt(t *testing.T) {
	r := newRig(t, withWeights())
	r.install()
	require.NoError(t, os.Remove(r.weightPath(weightDest)))

	v := r.view(t)
	assert.True(t, v.Installed)
	assert.Equal(t, localruntime.WeightsMissing, v.Weights.State)
	assert.Equal(t, 1, v.Weights.NotReady, "the optional file was not asked for")
	assert.Contains(t, v.Weights.Message, "導入済み（重みが不足）")
	assert.Equal(t, int64(len(weightBytes)), v.Weights.NeedBytes)

	_, err := r.svc.Start(ctx(), "gpt-sovits")
	require.ErrorIs(t, err, localruntime.ErrWeightsMissing)
	var we *localruntime.WeightsError
	require.ErrorAs(t, err, &we)
	assert.Equal(t, "gpt-sovits", we.Runtime)
	assert.Equal(t, "model", we.Assets[0].Name)
	assert.Contains(t, err.Error(), "重みが不足しています（model）")
	assert.Contains(t, err.Error(), "重みを導入")
	assert.ErrorIs(t, r.svc.Prepare(ctx(), "gpt-sovits"), localruntime.ErrWeightsMissing)
	assert.Equal(t, proc.StateStopped, r.view(t).Process.State, "the server was never started")

	ref, err := r.svc.InstallWeights(ctx(), "gpt-sovits")
	require.NoError(t, err)
	again, err := r.svc.InstallWeights(ctx(), "gpt-sovits")
	require.NoError(t, err)
	assert.Equal(t, ref.JobID, again.JobID)
	assert.True(t, again.Existing, "the same work is not queued twice")
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Equal(t, localruntime.WeightsReady, r.view(t).Weights.State)
	require.NoError(t, r.svc.Prepare(ctx(), "gpt-sovits"))
}

func TestFilesThatAreThereButNotCheckedAreCheckedByInstallWeightsWithoutADownload(t *testing.T) {
	r := newRig(t, withWeights())
	r.install()
	// Another process wrote the file: the same size, but nobody has hashed it.
	require.NoError(t, os.WriteFile(r.weightPath(weightDest), weightBytes, 0o600))
	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(r.weightPath(weightDest), later, later))

	v := r.view(t)
	assert.Equal(t, localruntime.WeightsUnverified, v.Weights.State)
	assert.Contains(t, v.Weights.Message, "確かめていません")
	_, err := r.svc.Start(ctx(), "gpt-sovits")
	require.ErrorIs(t, err, localruntime.ErrWeightsMissing)
	assert.Contains(t, err.Error(), "重みの確認が済んでいません")

	requests := len(r.host.Ranges())
	ref, err := r.svc.InstallWeights(ctx(), "gpt-sovits")
	require.NoError(t, err)
	require.Equal(t, domain.JobSucceeded, r.wait(ref.JobID).State)
	assert.Len(t, r.host.Ranges(), requests, "the file was hashed in place, not fetched")
	assert.Equal(t, localruntime.WeightsReady, r.view(t).Weights.State)
}

func TestInstallWeightsIsRefusedWhereTheRuntimeIsNotInstalledOrCannotRun(t *testing.T) {
	r := newRig(t, withWeights())
	_, err := r.svc.InstallWeights(ctx(), "gpt-sovits")
	assert.ErrorIs(t, err, localruntime.ErrNotInstalled)
	_, err = r.svc.InstallWeights(ctx(), "nope")
	assert.ErrorIs(t, err, localruntime.ErrNotFound)

	mac := newRig(t, withWeights(), platform("darwin", "arm64"))
	_, err = mac.svc.InstallWeights(ctx(), "gpt-sovits")
	var se *localruntime.SupportError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, "os", string(se.Support.Code))
	assert.Equal(t, localruntime.WeightsNone, mac.view(t).Weights.State)
	assert.Equal(t, 1, mac.view(t).Weights.Unclear, "the licenses are shown on a machine that cannot run it too")
}

func TestAWeightWithoutFreeSpaceIsRefusedWithTheReasonAndNothingIsFetched(t *testing.T) {
	r := newRig(t, withWeights())
	r.install()
	require.NoError(t, os.Remove(r.weightPath(weightDest)))
	r.free = 100
	requests := len(r.host.Ranges())
	_, err := r.svc.InstallWeights(ctx(), "gpt-sovits")
	var ns *fetch.NoSpaceError
	require.ErrorAs(t, err, &ns)
	assert.Equal(t, uint64(len(weightBytes)), ns.Need)
	assert.Len(t, r.host.Ranges(), requests)
}

func TestAWeightWithTheWrongHashFailsTheJobWithAReasonAndIsDeleted(t *testing.T) {
	r := newRig(t, withWeights())
	r.install()
	require.NoError(t, os.Remove(r.weightPath(weightDest)))
	r.host.files[weightURLPath] = bytes.Repeat([]byte("z"), len(weightBytes)) // the same size, other content

	ref, err := r.svc.InstallWeights(ctx(), "gpt-sovits")
	require.NoError(t, err)
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "SHA-256 が一致しません")
	assert.Contains(t, st.Error, "model", "the file is named")
	assert.NoFileExists(t, r.weightPath(weightDest))
	assert.NoFileExists(t, r.weightPath(weightDest)+".part")
	assert.Equal(t, localruntime.WeightsMissing, r.view(t).Weights.State)
}

func TestAWeightWhoseSourceIsGoneFailsTheJobAndSaysWhereTheOriginalIs(t *testing.T) {
	r := newRig(t, withWeights())
	r.install()
	require.NoError(t, os.Remove(r.weightPath(extraDest)))
	require.NoError(t, os.Remove(r.weightPath(weightDest)))
	r.host.mu.Lock()
	delete(r.host.files, extraURLPath) // a mirror that went away
	r.host.mu.Unlock()

	ref, err := r.svc.InstallWeights(ctx(), "gpt-sovits")
	require.NoError(t, err)
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "取得元が消えています（extra、HTTP 404）")
	assert.Contains(t, st.Error, "原本は x。2026-11-01 に SHA-256 の一致を確認した", "the note of the mirror says where the original is")
	v := r.view(t)
	assert.Zero(t, v.Weights.NotReady, "the required weight, which came before it, was fetched and checked")
	assert.Equal(t, rtm.AssetMissing, v.Weights.Assets[1].State)
}

func TestRemovingTheRuntimeRemovesItsWeights(t *testing.T) {
	r := newRig(t, withWeights())
	r.install()
	require.NoError(t, r.svc.Remove(ctx(), "gpt-sovits"))
	assert.NoFileExists(t, r.weightPath(weightDest))
	v := r.view(t)
	assert.False(t, v.Installed)
	assert.Equal(t, localruntime.WeightsNone, v.Weights.State)
}
