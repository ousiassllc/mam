package service_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func (f genFixture) upload(t *testing.T, kind, name, tags string) domain.ReferenceItem {
	t.Helper()
	before, err := f.svc.Reference.Library(context.Background(), f.projectID, service.LibraryQuery{})
	require.NoError(t, err)
	view, err := f.svc.Reference.Upload(context.Background(), f.projectID, service.UploadReferenceInput{File: upload("a.png", pngBytes), Kind: kind, Name: name, Tags: tags})
	require.NoError(t, err)
	require.Len(t, view.Items, len(before.Items)+1)
	return view.Items[0]
}

func (f genFixture) buildPack(t *testing.T) {
	t.Helper()
	_, err := f.svc.Generation.BuildReferencePack(context.Background(), f.shotID)
	require.NoError(t, err)
	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)
}

func TestReferenceUpload_ValidatesEverythingBeforeStoringAnything(t *testing.T) {
	f := newGenFixture(t, false)
	long := make([]byte, 0, 200)
	for range 101 {
		long = append(long, 'a')
	}
	tags11 := "1,2,3,4,5,6,7,8,9,10,11"
	tests := []struct {
		name  string
		in    service.UploadReferenceInput
		field string
	}{
		{"bad kind", service.UploadReferenceInput{File: upload("a.png", pngBytes), Kind: "keyframe", Name: "x"}, "kind"},
		{"no kind", service.UploadReferenceInput{File: upload("a.png", pngBytes), Name: "x"}, "kind"},
		{"blank name", service.UploadReferenceInput{File: upload("a.png", pngBytes), Kind: "style", Name: "  "}, "name"},
		{"long name", service.UploadReferenceInput{File: upload("a.png", pngBytes), Kind: "style", Name: string(long)}, "name"},
		{"too many tags", service.UploadReferenceInput{File: upload("a.png", pngBytes), Kind: "style", Name: "x", Tags: tags11}, "tags"},
		{"long tag", service.UploadReferenceInput{File: upload("a.png", pngBytes), Kind: "style", Name: "x", Tags: string(long)}, "tags"},
		{"no file", service.UploadReferenceInput{Kind: "style", Name: "x"}, "file"},
		{"video", service.UploadReferenceInput{File: upload("a.mp4", mp4Bytes), Kind: "style", Name: "x"}, "file"},
		{"fake image", service.UploadReferenceInput{File: upload("a.png", []byte("MZ\x90\x00 no")), Kind: "style", Name: "x"}, "file"},
		{"empty file", service.UploadReferenceInput{File: upload("a.png", nil), Kind: "style", Name: "x"}, "file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.svc.Reference.Upload(context.Background(), f.projectID, tt.in)
			requireInvalid(t, err, tt.field)
		})
	}
	assert.Equal(t, 0, f.count(t, `SELECT COUNT(*) FROM reference_item`))
	assert.Equal(t, 0, f.count(t, `SELECT COUNT(*) FROM asset WHERE type = 'reference'`), "nothing is stored")
	assert.Equal(t, 0, countFiles(t, f.assetsDir), "no file is left on disk")

	_, err := f.svc.Reference.Upload(context.Background(), "nope", service.UploadReferenceInput{File: upload("a.png", pngBytes), Kind: "style", Name: "x"})
	assert.ErrorIs(t, err, service.ErrNotFound)
	assert.Equal(t, 0, countFiles(t, f.assetsDir))
}

func TestReferenceLibrary_RegistersSearchesAndRemoves(t *testing.T) {
	f := newGenFixture(t, false)
	ctx := context.Background()
	mio := f.upload(t, "character", "  Mio face ", "hero, 主役、Hero")
	assert.Equal(t, "Mio face", mio.Name)
	assert.Equal(t, []string{"hero", "主役"}, mio.Tags, "tags are split at commas and 、 and deduplicated ignoring case")
	assert.Equal(t, domain.SourceUploaded, mio.Source)
	f.upload(t, "product", "Serum", "")

	view, err := f.svc.Reference.Library(ctx, f.projectID, service.LibraryQuery{Q: "mio", Kind: "character", Tag: "主役"})
	require.NoError(t, err)
	require.Len(t, view.Items, 1)
	assert.Equal(t, "mio", view.Query.Q)
	view, err = f.svc.Reference.Library(ctx, f.projectID, service.LibraryQuery{Kind: "product", Tag: "hero"})
	require.NoError(t, err)
	assert.Empty(t, view.Items, "filters are ANDed")

	_, err = f.svc.Reference.Library(ctx, f.projectID, service.LibraryQuery{Kind: "keyframe"})
	requireInvalid(t, err, "kind")
	_, err = f.svc.Reference.Library(ctx, "nope", service.LibraryQuery{})
	assert.ErrorIs(t, err, service.ErrNotFound)

	require.NoError(t, f.svc.Reference.Remove(ctx, f.projectID, mio.ID))
	view, err = f.svc.Reference.Library(ctx, f.projectID, service.LibraryQuery{})
	require.NoError(t, err)
	assert.Len(t, view.Items, 1)
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM asset WHERE id = ?`, mio.AssetID), "the image stays")
	assert.ErrorIs(t, f.svc.Reference.Remove(ctx, f.projectID, mio.ID), service.ErrNotFound)
}

func TestBuildReferencePack_RegistersGeneratedImagesInTheLibraryExceptTheKeyframe(t *testing.T) {
	f := newGenFixture(t, true)
	f.buildPack(t)
	view, err := f.svc.Reference.Library(context.Background(), f.projectID, service.LibraryQuery{})
	require.NoError(t, err)
	assert.Len(t, view.Items, 5)
	for _, it := range view.Items {
		assert.Equal(t, domain.SourceGenerated, it.Source)
		assert.Equal(t, f.shotID, it.SourceShotID)
		assert.NotEqual(t, domain.RefKeyframe, it.Kind)
	}
	view, _ = f.svc.Reference.Library(context.Background(), f.projectID, service.LibraryQuery{Q: "serum"})
	require.Len(t, view.Items, 1)
	assert.Equal(t, domain.RefProduct, view.Items[0].Kind)
}

func TestReferencePin_PinnedImageSurvivesARebuildWithoutBeingGeneratedAgain(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()
	it := f.upload(t, "character", "Mio", "hero")

	inspector, err := f.svc.Reference.Pin(ctx, f.shotID, it.ID)
	require.NoError(t, err)
	require.Len(t, inspector.Pack, 1)
	assert.Equal(t, it.ID, inspector.Pack[0].LibraryItem)
	_, err = f.svc.Reference.Pin(ctx, f.shotID, "")
	var invalid *service.ValidationError
	require.ErrorAs(t, err, &invalid)
	_, err = f.svc.Reference.Pin(ctx, f.shotID, "missing")
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = f.svc.Reference.Pin(ctx, "missing", it.ID)
	assert.ErrorIs(t, err, service.ErrNotFound)

	f.buildPack(t)
	f.models.mu.Lock()
	generated := len(f.models.images)
	f.models.mu.Unlock()
	assert.Equal(t, 5, generated, "the Character Reference comes from the Library: 4 references and the Keyframe are made")

	byKind := map[domain.ReferenceKind]domain.Reference{}
	for _, r := range f.shot(t).References() {
		byKind[r.Kind] = r
	}
	assert.Len(t, byKind, 6)
	assert.Equal(t, it.AssetID, byKind[domain.RefCharacter].AssetID, "the pinned image is the Character Reference")
	assert.Equal(t, it.ID, byKind[domain.RefCharacter].LibraryItem)
	assert.Equal(t, 5, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry`), "no cost for the pinned image")
	assert.Equal(t, 5, f.count(t, `SELECT COUNT(*) FROM reference_item`), "the pinned image is not registered twice (1 upload + 4 generated)")

	// A second rebuild keeps it again.
	f.buildPack(t)
	for _, r := range f.shot(t).References() {
		if r.Kind == domain.RefCharacter {
			assert.Equal(t, it.AssetID, r.AssetID)
		}
	}
}

func TestReferencePin_ABusyShotIsRefused(t *testing.T) {
	f := newGenFixture(t, false)
	it := f.upload(t, "style", "Noir", "")
	f.execSQL(t, `UPDATE shot SET state = 'GENERATING' WHERE id = ?`, f.shotID)
	_, err := f.svc.Reference.Pin(context.Background(), f.shotID, it.ID)
	assert.ErrorIs(t, err, service.ErrBusy)
}

func (f genFixture) seedWorld(t *testing.T) {
	t.Helper()
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO character (id, project_id, name) VALUES ('c1', '` + f.projectID + `', 'Mio')`,
			`INSERT INTO product (id, project_id, name) VALUES ('p1', '` + f.projectID + `', 'Serum')`,
			`INSERT INTO location (id, project_id, name, time) VALUES ('l1', '` + f.projectID + `', 'Street', 'dusk')`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
}

func TestWorldAssignReference_ChecksKindSlotLockAndAppliesToTheNextReferencePack(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()
	f.seedWorld(t)
	face := f.upload(t, "character", "Mio face", "")
	street := f.upload(t, "location", "Street layout", "")

	view, err := f.svc.World.AssignReference(ctx, f.projectID, "", domain.TargetCharacter, "c1", "face", face.ID)
	require.NoError(t, err)
	assert.Equal(t, face.AssetID, view.World.Characters[0].FaceReferenceAssetID)
	_, err = f.svc.World.AssignReference(ctx, f.projectID, "", domain.TargetLocation, "l1", "layout", street.ID)
	require.NoError(t, err)

	var invalid *service.ValidationError
	_, err = f.svc.World.AssignReference(ctx, f.projectID, "", domain.TargetCharacter, "c1", "body", street.ID)
	require.ErrorAs(t, err, &invalid, "a Location image does not fit a Character slot")
	assert.Equal(t, "reference_id", invalid.Field)
	_, err = f.svc.World.AssignReference(ctx, f.projectID, "", domain.TargetCharacter, "c1", "layout", face.ID)
	require.ErrorAs(t, err, &invalid, "a Character has no layout slot")
	assert.Equal(t, "slot", invalid.Field)
	_, err = f.svc.World.AssignReference(ctx, f.projectID, "", domain.TargetCharacter, "c1", "face", "missing")
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = f.svc.World.AssignReference(ctx, f.projectID, "", domain.TargetCharacter, "nobody", "face", face.ID)
	assert.ErrorIs(t, err, service.ErrNotFound)

	f.buildPack(t)
	f.models.mu.Lock()
	generated := len(f.models.images)
	f.models.mu.Unlock()
	assert.Equal(t, 4, generated, "the Character and the Location come from the World State references: Product, Style, Lighting and the Keyframe are made")
	for _, r := range f.shot(t).References() {
		switch r.Kind {
		case domain.RefCharacter:
			assert.Equal(t, face.AssetID, r.AssetID)
		case domain.RefLocation:
			assert.Equal(t, street.AssetID, r.AssetID)
		}
	}

	// LOCK freezes the slot: no assigning, no clearing.
	_, err = f.svc.World.SetFieldLock(ctx, f.projectID, "", domain.TargetCharacter, "c1", "identity", domain.LockLocked)
	require.NoError(t, err)
	_, err = f.svc.World.AssignReference(ctx, f.projectID, "", domain.TargetCharacter, "c1", "face", "")
	assert.ErrorIs(t, err, service.ErrLocked)
}

func TestWorldSetFieldLock_ValidatesTargetParameterAndMode(t *testing.T) {
	f := newGenFixture(t, false)
	ctx := context.Background()
	f.seedWorld(t)

	view, err := f.svc.World.SetFieldLock(ctx, f.projectID, "", domain.TargetLocation, "l1", "lighting_state", domain.LockLocked)
	require.NoError(t, err)
	assert.True(t, view.Locks.Locked(domain.TargetLocation, "l1", "lighting_state"))
	view, err = f.svc.World.SetFieldLock(ctx, f.projectID, "", domain.TargetCamera, "", "lens", domain.LockLocked)
	require.NoError(t, err)
	assert.True(t, view.Locks.Locked(domain.TargetCamera, "", "lens"))
	view, err = f.svc.World.SetFieldLock(ctx, f.projectID, "", domain.TargetLocation, "l1", "lighting_state", domain.LockAuto)
	require.NoError(t, err)
	assert.False(t, view.Locks.Locked(domain.TargetLocation, "l1", "lighting_state"))

	bad := []struct {
		target domain.WorldTarget
		id     string
		field  string
		mode   domain.LockMode
		want   string
	}{
		{"planet", "l1", "time", domain.LockLocked, "target_type"},
		{domain.TargetLocation, "l1", "name", domain.LockLocked, "parameter"},
		{domain.TargetCharacter, "c1", "layout_reference", domain.LockLocked, "parameter"},
		{domain.TargetLocation, "l1", "time", domain.LockManual, "mode"},
		{domain.TargetLocation, "l1", "time", "", "mode"},
	}
	for _, b := range bad {
		_, err := f.svc.World.SetFieldLock(ctx, f.projectID, "", b.target, b.id, b.field, b.mode)
		var invalid *service.ValidationError
		require.ErrorAs(t, err, &invalid, b)
		assert.Equal(t, b.want, invalid.Field, b)
	}
	_, err = f.svc.World.SetFieldLock(ctx, f.projectID, "", domain.TargetLocation, "nope", "time", domain.LockLocked)
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = f.svc.World.SetFieldLock(ctx, "nope", "", domain.TargetCamera, "", "lens", domain.LockLocked)
	assert.ErrorIs(t, err, service.ErrNotFound)
}
