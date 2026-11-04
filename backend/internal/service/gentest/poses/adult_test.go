package poses_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses/posestest"
)

func idsOf(t *testing.T, l *poses.Library) []string {
	t.Helper()
	all, err := l.Poses()
	require.NoError(t, err)
	return ids(all)
}

// While the age gate is closed the suggestive series are nowhere: not installed (nothing is fetched, and the message says only where the gate is),
// not offered in the status, not in the list, not found by ID or by name, and their pictures are answered like pictures that do not exist.
func TestWhileTheAgeGateIsClosedTheSuggestiveSeriesAreNotInstalledOfferedListedOrServed(t *testing.T) {
	r := posestest.New(t)
	r.Install(t)
	before := r.Requests.Load()

	err := r.Lib.InstallAdult(context.Background(), true, nil)
	assert.ErrorIs(t, err, poses.ErrAdultGateClosed)
	assert.Equal(t, before, r.Requests.Load(), "a refused install fetches nothing")
	assert.NoDirExists(t, r.AdultDir, "and writes nothing")

	assert.Nil(t, r.Lib.Status().Adult, "the offer is not made while the gate is closed")
	assert.Equal(t, []string{"P001", "P002", "U001"}, idsOf(t, r.Lib))
	_, ok := r.Lib.Pose("S001")
	assert.False(t, ok)
	_, _, _, ok = r.Lib.ImageOwner("sexy_S001_front")
	assert.False(t, ok)
	_, err = r.Lib.File("sexy_S001_front")
	var invalid *gterr.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.NotContains(t, invalid.Message, "S字立ち", "the refusal does not name the pose")
	assert.True(t, r.Lib.Hides("sexy_S001_front"))
	assert.True(t, r.Lib.Hides("duo_D001_front.depth"))
	assert.False(t, r.Lib.Hides("std_P001_front"))
}

// The same library with a gate that was never given (Deps.AdultOpen nil) is closed for good.
func TestALibraryWithoutAGateIsClosed(t *testing.T) {
	r := posestest.New(t)
	r.Install(t)
	lib, err := poses.New(poses.Deps{Root: r.Root, Catalog: nil})
	require.NoError(t, err)
	assert.True(t, lib.Hides("sexy_S001_front"))
	assert.Nil(t, lib.Status().Adult)
	assert.ErrorIs(t, lib.InstallAdult(context.Background(), true, nil), poses.ErrAdultGateClosed)
}

// With the gate open the series are an offer of their own: the agreement is needed again, the main library first.
func TestTheSuggestiveSeriesNeedTheAgreementAndTheMainLibrary(t *testing.T) {
	r := posestest.New(t)
	r.Open.Store(true)

	st := r.Lib.Status().Adult
	require.NotNil(t, st, "an open gate makes the offer")
	assert.False(t, st.Installed)
	assert.Equal(t, 2, st.Poses, "S001 and D001")
	assert.Positive(t, st.DownloadBytes)

	require.ErrorIs(t, r.Lib.InstallAdult(context.Background(), true, nil), poses.ErrBaseNotInstalled)
	r.Install(t)
	before := r.Requests.Load()
	assert.ErrorIs(t, r.Lib.InstallAdult(context.Background(), false, nil), poses.ErrNotAccepted)
	assert.Equal(t, before, r.Requests.Load(), "no agreement: nothing is fetched")
	assert.Equal(t, []string{"P001", "P002", "U001"}, idsOf(t, r.Lib), "an offer that was not taken lists nothing")
}

// Installed and open, the series are listed (with the maps), served from their own directory and found by ID; closing the gate again takes them
// out at once, with no reinstall, and opening it brings them back.
func TestTheSuggestiveSeriesAreListedOnlyWhileTheGateIsOpen(t *testing.T) {
	r := posestest.New(t)
	r.InstallAdult(t)

	assert.Equal(t, []string{"P001", "P002", "U001", "S001", "D001"}, idsOf(t, r.Lib), "the catalog's order; the general poses and the suggestive ones")
	s1, ok := r.Lib.Pose("S001")
	require.True(t, ok)
	assert.True(t, s1.Adult)
	assert.Equal(t, "sexy_S001_front", s1.Image(poses.Front))
	assert.Equal(t, "sexy_S001_front.depth", s1.ImageOf(poses.Depth, poses.Front))
	assert.Equal(t, "sexy_S001_front.normal", s1.ImageOf(poses.Normal, poses.Front))
	d1, ok := r.Lib.Pose("D001")
	require.True(t, ok, "a duo pose rated general is still of the suggestive series")
	assert.True(t, d1.Adult)

	path, err := r.Lib.File("sexy_S001_front")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(r.AdultDir, "poses", "sexy_S001_front.webp"), path)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, posestest.Picture("sexy_S001_front"), got)
	mapPath, err := r.Lib.File("duo_D001_front.depth")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(r.AdultDir, "depth", "duo_D001_front.webp"), mapPath)
	assert.False(t, r.Lib.Hides("sexy_S001_front"))
	st := r.Lib.Status().Adult
	require.NotNil(t, st)
	assert.True(t, st.Installed)

	r.Open.Store(false)
	assert.Equal(t, []string{"P001", "P002", "U001"}, idsOf(t, r.Lib), "the gate closed: gone at once")
	_, err = r.Lib.File("sexy_S001_front")
	assert.Error(t, err)
	_, ok = r.Lib.Pose("D001")
	assert.False(t, ok)
	assert.Nil(t, r.Lib.Status().Adult)
	assert.True(t, r.Lib.Hides("sexy_S001_front"))
	assert.FileExists(t, path, "the files stay: the gate hides them, it does not delete them")

	r.Open.Store(true)
	assert.Equal(t, []string{"P001", "P002", "U001", "S001", "D001"}, idsOf(t, r.Lib))
}

// A pose of a suggestive series is not listed from the general set's files, whatever the catalog says: the general set never holds one.
func TestTheSuggestiveSeriesAreNotListedWithoutTheirOwnSet(t *testing.T) {
	r := posestest.New(t)
	r.Open.Store(true)
	r.Install(t)
	assert.NoDirExists(t, r.AdultDir)
	assert.Equal(t, []string{"P001", "P002", "U001"}, idsOf(t, r.Lib), "the catalog names S001 and D001, no picture of theirs is on the disk")
}

func TestRemoveAdultLeavesTheMainLibraryAndRemoveTakesBoth(t *testing.T) {
	r := posestest.New(t)
	r.InstallAdult(t)

	require.NoError(t, r.Lib.RemoveAdult())
	assert.NoDirExists(t, r.AdultDir)
	assert.Equal(t, []string{"P001", "P002", "U001"}, idsOf(t, r.Lib), "the main library stays")
	assert.False(t, r.Lib.Status().Adult.Installed)
	require.NoError(t, r.Lib.RemoveAdult(), "removing what is not there is no error")

	require.NoError(t, r.Lib.InstallAdult(context.Background(), true, nil))
	r.Open.Store(false) // removing is allowed with the gate closed
	require.NoError(t, r.Lib.Remove())
	assert.NoDirExists(t, r.AdultDir)
	assert.NoDirExists(t, r.Dir)
	assert.False(t, r.Lib.Status().Installed)
}

// A library that was installed before the depth and normal maps (Issue #1020) is "not installed" until they are in: the status says how much of the
// download is still to fetch, so that the user is told the files that are there are not fetched again.
func TestALibraryThatLacksTheMapsSaysHowMuchIsStillToDownload(t *testing.T) {
	r := posestest.New(t)
	full := r.Lib.Status()
	assert.Equal(t, full.DownloadBytes, full.RemainBytes, "nothing is in place: all of it")

	r.Install(t)
	done := r.Lib.Status()
	assert.True(t, done.Installed)
	assert.Zero(t, done.RemainBytes)

	require.NoError(t, os.RemoveAll(filepath.Join(r.Dir, "depth")))
	need := r.Lib.Status()
	assert.False(t, need.Installed, "the maps are missing")
	assert.Positive(t, need.RemainBytes)
	assert.Less(t, need.RemainBytes, need.DownloadBytes, "only the depth zip is still to fetch")
}
