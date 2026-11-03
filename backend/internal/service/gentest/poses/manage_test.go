package poses_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses/posestest"
)

// The progress of an install can be read while it runs (the screen polls it) and is idle before and after.
func TestProgressIsReadableWhileTheInstallRunsAndIdleOtherwise(t *testing.T) {
	r := posestest.New(t)
	assert.Equal(t, poses.Progress{}, r.Lib.Progress())

	var seen []poses.Progress
	require.NoError(t, r.Lib.Install(context.Background(), true, func(done, total int64) {
		p := r.Lib.Progress()
		seen = append(seen, p)
		assert.True(t, p.Running)
		assert.Equal(t, done, p.Done)
		assert.Equal(t, total, p.Total)
	}))
	require.NotEmpty(t, seen)
	assert.Positive(t, seen[len(seen)-1].Total)
	assert.False(t, r.Lib.Progress().Running, "an install that ended is not running")
}

// Removing deletes the files and the record: the library is not installed, and an install puts it back. A library that is not installed is
// removed without an error, and an install that is running is not removed under its own feet.
func TestRemoveTakesTheLibraryOutAndAnInstallPutsItBack(t *testing.T) {
	r := posestest.New(t)
	require.NoError(t, r.Lib.Remove(), "nothing to remove is not an error")

	require.NoError(t, r.Lib.Install(context.Background(), true, nil))
	file, err := r.Lib.File("std_P001_front")
	require.NoError(t, err)
	require.FileExists(t, file)

	require.NoError(t, r.Lib.Remove())
	assert.False(t, r.Lib.Status().Installed)
	_, err = r.Lib.Poses()
	assert.ErrorIs(t, err, poses.ErrNotInstalled)
	_, err = r.Lib.File("std_P001_front")
	assert.ErrorIs(t, err, poses.ErrNotInstalled)
	assert.NoFileExists(t, file)
	_, statErr := os.Stat(r.Dir)
	assert.True(t, errors.Is(statErr, os.ErrNotExist), "the directory of the pictures is gone")

	require.NoError(t, r.Lib.Install(context.Background(), true, nil))
	assert.True(t, r.Lib.Status().Installed)
	all, err := r.Lib.Poses()
	require.NoError(t, err)
	assert.Equal(t, []string{"P001", "P002", "U001"}, ids(all))
}

func TestRemoveRefusesWhileAnInstallRuns(t *testing.T) {
	r := posestest.New(t)
	var refused error
	require.NoError(t, r.Lib.Install(context.Background(), true, func(_, _ int64) {
		if refused == nil {
			refused = r.Lib.Remove()
		}
	}))
	var conflict *gterr.ConflictError
	require.ErrorAs(t, refused, &conflict)
	assert.True(t, r.Lib.Status().Installed, "the install finished undisturbed")
}
