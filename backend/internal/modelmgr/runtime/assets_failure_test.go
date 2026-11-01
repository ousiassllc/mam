package runtime_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

func TestEnsureAssetsResumesAnInterruptedDownload(t *testing.T) {
	r := newAssetRig(t)
	r.host.cutOnce[weightPath] = true
	err := r.EnsureAssets(context.Background(), nil)
	var ae *rtm.AssetError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "weight", ae.Name)
	part := filepath.Join(r.Layout.EnvDir(), filepath.FromSlash(weightDest)) + ".part"
	st, statErr := os.Stat(part)
	require.NoError(t, statErr, "the part file is kept")

	need := r.AssetNeedBytes()
	assert.Less(t, need, r.AssetSet.TotalSize()+int64(len(dictBody))*3, "the part of the weight that is there needs no room again")

	require.NoError(t, r.EnsureAssets(context.Background(), nil))
	reqs := r.host.requests(weightPath)
	require.Len(t, reqs, 2)
	assert.Equal(t, "bytes="+strconv.FormatInt(st.Size(), 10)+"-", reqs[1], "the second run continues where the first one stopped")
	assert.Equal(t, rtm.AssetOK, r.states()["weight"])
}

func TestEnsureAssetsStopsWhenCancelled(t *testing.T) {
	r := newAssetRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	err := r.EnsureAssets(ctx, func(rtm.Phase, string, int64, int64) { cancel() })
	require.ErrorIs(t, err, context.Canceled)
	assert.NotEmpty(t, r.NotReady())
	require.NoError(t, r.EnsureAssets(context.Background(), nil))
	assert.Empty(t, r.NotReady())
}

func TestEnsureAssetsRefusesAWrongChecksumAndDeletesIt(t *testing.T) {
	r := newAssetRig(t)
	r.AssetSet.Assets[0].SHA256 = strings.Repeat("0", 64)
	err := r.EnsureAssets(context.Background(), nil)
	require.ErrorIs(t, err, fetch.ErrChecksum)
	var ae *rtm.AssetError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "weight", ae.Name, "the asset that failed is named")
	assert.NoFileExists(t, filepath.Join(r.Layout.EnvDir(), filepath.FromSlash(weightDest)))
	assert.NoFileExists(t, filepath.Join(r.Layout.EnvDir(), filepath.FromSlash(weightDest))+".part")
	assert.Equal(t, rtm.AssetMissing, r.states()["weight"])
}

func TestEnsureAssetsReportsASourceThatIsGone(t *testing.T) {
	r := newAssetRig(t)
	r.host.status[weightPath] = http.StatusNotFound
	err := r.EnsureAssets(context.Background(), nil)
	var he *fetch.HTTPError
	require.ErrorAs(t, err, &he)
	assert.Equal(t, http.StatusNotFound, he.Status)
	assert.True(t, he.Definitive())
	var ae *rtm.AssetError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "weight", ae.Name)
}

func TestEnsureAssetsRefusesWithoutFreeSpaceAndSendsNothing(t *testing.T) {
	r := newAssetRig(t)
	r.FreeBytes = func(string) (uint64, error) { return 100, nil }
	err := r.EnsureAssets(context.Background(), nil)
	var ns *fetch.NoSpaceError
	require.ErrorAs(t, err, &ns)
	assert.Greater(t, ns.Need, uint64(len(weightBody)))
	assert.Equal(t, uint64(100), ns.Free)
	assert.Zero(t, r.host.total())
	require.ErrorAs(t, r.CheckAssetSpace(), &ns)
}

func TestEnsureAssetsRefusesAnArchiveThatIsNotWhatTheCatalogDescribes(t *testing.T) {
	r := newAssetRig(t)
	r.AssetSet.Assets[2].Members[0].Entry = "pkg/not-there.txt"
	err := r.EnsureAssets(context.Background(), nil)
	require.Error(t, err)
	var ae *rtm.AssetError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "zip", ae.Name)
	assert.NoFileExists(t, filepath.Join(r.Layout.EnvDir(), "runtime", "dict", "dict.txt"), "nothing of it is left behind")

	r = newAssetRig(t)
	r.AssetSet.Assets[2].Members[1].SHA256 = strings.Repeat("1", 64)
	require.ErrorIs(t, r.EnsureAssets(context.Background(), nil), exeinstall.ErrMismatch)
}

func TestRemoveForgetsWhatWasVerified(t *testing.T) {
	r := newAssetRig(t)
	require.NoError(t, r.EnsureAssets(context.Background(), nil))
	require.NotEmpty(t, r.Store.Get().Files)
	require.NoError(t, r.Remove())
	assert.Empty(t, r.Store.Get().Files)
	assert.Equal(t, rtm.AssetMissing, r.states()["weight"])
}

func TestARuntimeWithoutAssetsHasNothingToCheck(t *testing.T) {
	r := newAssetRig(t)
	r.AssetSet = catalog.ResourceSet{}
	assert.Empty(t, r.Assets())
	assert.Empty(t, r.NotReady())
	require.NoError(t, r.EnsureAssets(context.Background(), nil))
}

// A new environment replaces the one whose files were verified: its files have to be checked again.
func TestInstallingThePackageForgetsWhatWasVerified(t *testing.T) {
	r := newRig(t, "20250604")
	require.NoError(t, r.Store.Update(func(s *store.RuntimeState) {
		s.Files = map[string]store.FileStamp{"models/a/model.bin": {Size: 1, ModTime: 2}}
	}))
	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))
	assert.Empty(t, r.Store.Get().Files)
}
