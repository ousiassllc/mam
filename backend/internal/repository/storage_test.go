package repository_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func (f genRepoFixture) setTake(t *testing.T, takeID string, status domain.TakeStatus) {
	t.Helper()
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE take SET status = ? WHERE id = ?`, status, takeID); err != nil {
			return err
		}
		if status == domain.TakeSelected {
			_, err := tx.ExecContext(ctx, `UPDATE shot SET selected_take_id = ? WHERE id = (SELECT pv.shot_id FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE t.id = ?)`, takeID, takeID)
			return err
		}
		return nil
	}))
}

func TestStorageRepository_MarksRejectedAndBeatenTakesUnneededAndListsThemFirst(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	shots, _ := f.repo.ListProjectShots(ctx, f.projectID)
	other := shots[1].Shot.ID

	selected := newTake(t, f, f.shotID, "sel.mp4", 100, 1, nil)
	beaten := newTake(t, f, f.shotID, "beaten.mp4", 200, 1, nil)
	rejected := newTake(t, f, other, "rej.mp4", 300, 1, nil)
	pending := newTake(t, f, other, "cand.mp4", 400, 1, nil)
	f.setTake(t, selected, domain.TakeSelected)
	f.setTake(t, rejected, domain.TakeRejected)

	sum, err := repository.NewStorageRepository(f.db).Usage(ctx, f.projectID)
	require.NoError(t, err)
	assert.EqualValues(t, 1000, sum.AssetBytes)
	require.Len(t, sum.Takes, 4)
	unneeded := map[string]bool{}
	for _, tk := range sum.Takes {
		unneeded[tk.TakeID] = tk.Unneeded
	}
	assert.Equal(t, map[string]bool{selected: false, beaten: true, rejected: true, pending: false}, unneeded,
		"a Take is unneeded when rejected or when another Take of its Shot is Selected")
	assert.True(t, sum.Takes[0].Unneeded && sum.Takes[1].Unneeded, "the unneeded ones come first")
	n, size := sum.UnneededTakes()
	assert.Equal(t, 2, n)
	assert.EqualValues(t, 500, size)
	assert.EqualValues(t, 500, sum.Takes[0].SizeBytes+sum.Takes[1].SizeBytes, "sizes come from the asset rows")
}

func TestStorageRepository_ArchiveTakeFreesTheFileButKeepsTheTakeAndRefusesTheSelectedOne(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	repo := repository.NewStorageRepository(f.db)
	selected := newTake(t, f, f.shotID, "sel.mp4", 100, 1, nil)
	other := newTake(t, f, f.shotID, "other.mp4", 200, 1, nil)
	f.setTake(t, selected, domain.TakeSelected)

	removed, err := repo.ArchiveTake(ctx, f.projectID, other)
	require.NoError(t, err)
	require.Len(t, removed, 1)
	assert.Equal(t, "other.mp4", removed[0].FilePath, "the caller deletes the file")

	var status string
	var path *string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT status, file_path FROM take WHERE id = ?`, other).Scan(&status, &path))
	assert.Equal(t, "rejected", status)
	assert.Nil(t, path, "the Take row stays for traceability, without a file")
	var assets, ledger int
	require.NoError(t, f.db.Reader().QueryRow(`SELECT (SELECT COUNT(*) FROM asset WHERE file_path = 'other.mp4'), (SELECT COUNT(*) FROM cost_ledger_entry WHERE take_id = ?)`, other).Scan(&assets, &ledger))
	assert.Zero(t, assets)
	assert.Equal(t, 1, ledger, "what the Take cost is not forgotten")

	_, err = repo.ArchiveTake(ctx, f.projectID, selected)
	assert.ErrorIs(t, err, repository.ErrConflict)
	_, err = repo.ArchiveTake(ctx, f.projectID, other)
	assert.ErrorIs(t, err, repository.ErrNotFound, "an archived Take has no file to give up")
	_, err = repo.ArchiveTake(ctx, "another-project", selected)
	assert.ErrorIs(t, err, repository.ErrNotFound, "a Take of another Project is invisible")
}

func TestStorageRepository_ArchiveUnneededKeepsSelectedAndUndecidedTakes(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	shots, _ := f.repo.ListProjectShots(ctx, f.projectID)
	other := shots[1].Shot.ID
	repo := repository.NewStorageRepository(f.db)

	selected := newTake(t, f, f.shotID, "sel.mp4", 100, 1, nil)
	newTake(t, f, f.shotID, "beaten.mp4", 200, 1, nil)
	newTake(t, f, other, "cand.mp4", 400, 1, nil)
	f.setTake(t, selected, domain.TakeSelected)

	removed, err := repo.ArchiveUnneeded(ctx, f.projectID)
	require.NoError(t, err)
	require.Len(t, removed, 1)
	assert.Equal(t, "beaten.mp4", removed[0].FilePath)

	sum, err := repo.Usage(ctx, f.projectID)
	require.NoError(t, err)
	require.Len(t, sum.Takes, 2, "the Selected Take and the Take of the undecided Shot keep their files")
	removed, err = repo.ArchiveUnneeded(ctx, f.projectID)
	require.NoError(t, err)
	assert.Empty(t, removed, "nothing left to archive")

	_, err = repo.Usage(ctx, "missing")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}
