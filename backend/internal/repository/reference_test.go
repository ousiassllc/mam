package repository_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

type libraryFixture struct {
	worldFixture
	refs  *repository.ReferenceRepository
	locID string
}

func newLibraryFixture(t *testing.T) libraryFixture {
	t.Helper()
	f := libraryFixture{worldFixture: newWorldFixture(t)}
	f.refs = repository.NewReferenceRepository(f.db)
	f.locID = domain.NewID()
	require.NoError(t, exec(t, f.db, `INSERT INTO location (id, project_id, name, time, weather) VALUES (?, ?, 'Street', 'sunset', 'clear')`, f.locID, f.projectID))
	return f
}

// upload registers an uploaded image and returns its Library ID.
func (f libraryFixture) upload(t *testing.T, kind domain.ReferenceKind, name string, tags ...string) domain.ReferenceItem {
	t.Helper()
	id := domain.NewID()
	item := domain.ReferenceItem{ID: domain.NewID(), ProjectID: f.projectID, AssetID: id, Kind: kind, Name: name, Tags: tags, Source: domain.SourceUploaded}
	require.NoError(t, f.refs.Add(context.Background(), item, domain.Asset{ID: id, ProjectID: f.projectID, Type: domain.AssetReference, FilePath: "p/" + id + ".png", MimeType: "image/png"}))
	return item
}

func names(items []domain.ReferenceItem) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}

func TestReferenceRepository_ListFiltersByQueryKindAndTagNewestFirst(t *testing.T) {
	ctx := context.Background()
	f := newLibraryFixture(t)
	f.upload(t, domain.RefCharacter, "Mio face", "hero", "close-up")
	f.upload(t, domain.RefProduct, "Serum bottle", "Hero")
	f.upload(t, domain.RefLocation, "100%_street")

	all, err := f.refs.List(ctx, f.projectID, repository.ReferenceFilter{})
	require.NoError(t, err)
	assert.Equal(t, []string{"100%_street", "Serum bottle", "Mio face"}, names(all), "newest first")

	got, err := f.refs.List(ctx, f.projectID, repository.ReferenceFilter{Query: "MIO"})
	require.NoError(t, err)
	assert.Equal(t, []string{"Mio face"}, names(got), "a name part, ignoring case")
	got, _ = f.refs.List(ctx, f.projectID, repository.ReferenceFilter{Query: "close"})
	assert.Equal(t, []string{"Mio face"}, names(got), "a tag part")
	got, _ = f.refs.List(ctx, f.projectID, repository.ReferenceFilter{Kind: domain.RefProduct})
	assert.Equal(t, []string{"Serum bottle"}, names(got))
	got, _ = f.refs.List(ctx, f.projectID, repository.ReferenceFilter{Tag: "hero"})
	assert.ElementsMatch(t, []string{"Serum bottle", "Mio face"}, names(got), "a whole tag, ignoring case")
	got, _ = f.refs.List(ctx, f.projectID, repository.ReferenceFilter{Tag: "her"})
	assert.Empty(t, got, "a tag is matched whole")
	got, _ = f.refs.List(ctx, f.projectID, repository.ReferenceFilter{Query: "hero", Kind: domain.RefProduct})
	assert.Equal(t, []string{"Serum bottle"}, names(got), "filters are ANDed")
	got, _ = f.refs.List(ctx, f.projectID, repository.ReferenceFilter{Query: "%"})
	assert.Equal(t, []string{"100%_street"}, names(got), "LIKE wildcards in the query are literal")
	got, _ = f.refs.List(ctx, "other-project", repository.ReferenceFilter{})
	assert.Empty(t, got, "another Project's Library")
}

func TestReferenceRepository_AddAndDeleteKeepTheImage(t *testing.T) {
	ctx := context.Background()
	f := newLibraryFixture(t)
	it := f.upload(t, domain.RefStyle, "Noir")
	got, err := f.refs.Get(ctx, f.projectID, it.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"Noir"}, []string{got.Name})
	assert.Equal(t, domain.SourceUploaded, got.Source)
	assert.Empty(t, got.Tags)

	assert.ErrorIs(t, f.refs.Add(ctx, domain.ReferenceItem{ID: "x", ProjectID: "nope", AssetID: "y", Kind: domain.RefStyle, Name: "n", Source: domain.SourceUploaded},
		domain.Asset{ID: "y", ProjectID: "nope", Type: domain.AssetReference, FilePath: "p/y.png", MimeType: "image/png"}), repository.ErrNotFound)
	assert.Equal(t, 0, f.count(t, `SELECT COUNT(*) FROM asset WHERE id = 'y'`), "nothing is stored for an unknown Project")

	assert.ErrorIs(t, f.refs.Delete(ctx, "other-project", it.ID), repository.ErrNotFound)
	require.NoError(t, f.refs.Delete(ctx, f.projectID, it.ID))
	assert.ErrorIs(t, f.refs.Delete(ctx, f.projectID, it.ID), repository.ErrNotFound)
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM asset WHERE id = ?`, it.AssetID), "the image stays")
}

func TestGenerationRepository_SaveReferencePackRegistersNewImagesInTheLibraryButNotTheKeyframeOrReusedImages(t *testing.T) {
	ctx := context.Background()
	f := newLibraryFixture(t)
	pinned := f.upload(t, domain.RefCharacter, "Mio")
	p := repository.NewReferencePack{ProjectID: f.projectID, ShotID: f.shotID}
	p.References = append(p.References, domain.Reference{Kind: domain.RefCharacter, Name: "Mio", AssetID: pinned.AssetID, LibraryItem: pinned.ID})
	for i, r := range []domain.Reference{{Kind: domain.RefProduct, Name: "Serum"}, {Kind: domain.RefStyle}, {Kind: domain.RefKeyframe}} {
		id := fmt.Sprintf("gen-%d", i)
		r.AssetID = id
		p.References = append(p.References, r)
		p.Assets = append(p.Assets, domain.Asset{ID: id, ProjectID: f.projectID, Type: domain.AssetReference, FilePath: "p/" + id + ".png", MimeType: "image/png"})
	}
	require.NoError(t, f.repo.SaveReferencePack(ctx, p))

	items, err := f.refs.List(ctx, f.projectID, repository.ReferenceFilter{})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Mio", "Serum", "Style"}, names(items), "the Keyframe is not registered, the pinned image is not registered twice")
	for _, it := range items {
		if it.Source == domain.SourceGenerated {
			assert.Equal(t, f.shotID, it.SourceShotID)
		}
	}
}

func TestReferenceRepository_PinReplacesSameKindAndNameAndRefusesBusyShots(t *testing.T) {
	ctx := context.Background()
	f := newLibraryFixture(t)
	old := domain.Reference{Kind: domain.RefCharacter, Name: "mio", AssetID: "old-asset", Prompt: "p"}
	keep := domain.Reference{Kind: domain.RefStyle, AssetID: "style-asset"}
	require.NoError(t, f.repo.SaveReferencePack(ctx, repository.NewReferencePack{ProjectID: f.projectID, ShotID: f.shotID, References: []domain.Reference{old, keep},
		Assets: []domain.Asset{
			{ID: "old-asset", ProjectID: f.projectID, Type: domain.AssetReference, FilePath: "p/o.png", MimeType: "image/png"},
			{ID: "style-asset", ProjectID: f.projectID, Type: domain.AssetReference, FilePath: "p/s.png", MimeType: "image/png"},
		}}))
	it := f.upload(t, domain.RefCharacter, "Mio")

	require.NoError(t, f.refs.Pin(ctx, f.shotID, it.ID))
	refs := shotReferences(t, f, f.shotID)
	require.Len(t, refs, 2)
	byKind := map[domain.ReferenceKind]domain.Reference{}
	for _, r := range refs {
		byKind[r.Kind] = r
	}
	assert.Equal(t, it.AssetID, byKind[domain.RefCharacter].AssetID, "the same kind and name (ignoring case) is replaced")
	assert.Equal(t, it.ID, byKind[domain.RefCharacter].LibraryItem)
	assert.Equal(t, "style-asset", byKind[domain.RefStyle].AssetID, "other references stay")
	require.NoError(t, f.refs.Pin(ctx, f.shotID, it.ID), "pinning twice does not duplicate")
	assert.Len(t, shotReferences(t, f, f.shotID), 2)

	assert.ErrorIs(t, f.refs.Pin(ctx, f.shotID, "missing"), repository.ErrNotFound)
	assert.ErrorIs(t, f.refs.Pin(ctx, "missing", it.ID), repository.ErrNotFound)

	other := createProject(t, repository.NewProjectRepository(f.db), domain.StatePlanning)
	foreign := domain.ReferenceItem{ID: domain.NewID(), ProjectID: other, AssetID: "foreign", Kind: domain.RefCharacter, Name: "Mio", Source: domain.SourceUploaded}
	require.NoError(t, f.refs.Add(ctx, foreign, domain.Asset{ID: "foreign", ProjectID: other, Type: domain.AssetReference, FilePath: "p/f.png", MimeType: "image/png"}))
	assert.ErrorIs(t, f.refs.Pin(ctx, f.shotID, foreign.ID), repository.ErrNotFound, "another Project's image cannot be pinned")

	ok, err := f.repo.StartShot(ctx, f.shotID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.ErrorIs(t, f.refs.Pin(ctx, f.shotID, it.ID), repository.ErrShotBusy)
}

// A Repair reads the Reference Pack when it starts and saves a prompt_version with whatever the spec
// holds when it ends, so a pin during REPAIRING would be in the prompt_version but not in the Take (#345).
func TestReferenceRepository_PinIsRefusedWhileTheShotIsRepairing(t *testing.T) {
	ctx := context.Background()
	f := newLibraryFixture(t)
	it := f.upload(t, domain.RefCharacter, "Mio")
	ok, err := f.repo.StartRepair(ctx, f.shotID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.ErrorIs(t, f.refs.Pin(ctx, f.shotID, it.ID), repository.ErrShotBusy)
	assert.Empty(t, shotReferences(t, f, f.shotID), "nothing is written")
}

func TestReferenceRepository_PinReplacesTheStyleAndLightingOfThePackWhateverTheirNames(t *testing.T) {
	ctx := context.Background()
	f := newLibraryFixture(t)
	generated := []domain.Reference{{Kind: domain.RefStyle, AssetID: "style-asset"}, {Kind: domain.RefLighting, AssetID: "light-asset"}, {Kind: domain.RefProduct, Name: "Serum", AssetID: "prod-asset"}}
	require.NoError(t, f.repo.SaveReferencePack(ctx, repository.NewReferencePack{ProjectID: f.projectID, ShotID: f.shotID, References: generated,
		Assets: []domain.Asset{
			{ID: "style-asset", ProjectID: f.projectID, Type: domain.AssetReference, FilePath: "p/s.png", MimeType: "image/png"},
			{ID: "light-asset", ProjectID: f.projectID, Type: domain.AssetReference, FilePath: "p/l.png", MimeType: "image/png"},
			{ID: "prod-asset", ProjectID: f.projectID, Type: domain.AssetReference, FilePath: "p/p.png", MimeType: "image/png"},
		}}))
	style := f.upload(t, domain.RefStyle, "Noir")
	light := f.upload(t, domain.RefLighting, "Golden hour")
	serum := f.upload(t, domain.RefProduct, "Other product")

	for _, it := range []domain.ReferenceItem{style, light, serum} {
		require.NoError(t, f.refs.Pin(ctx, f.shotID, it.ID))
	}

	byKind := map[domain.ReferenceKind][]domain.Reference{}
	for _, r := range shotReferences(t, f, f.shotID) {
		byKind[r.Kind] = append(byKind[r.Kind], r)
	}
	require.Len(t, byKind[domain.RefStyle], 1, "a Pack has one Style image: the pinned one replaces the generated one")
	assert.Equal(t, style.ID, byKind[domain.RefStyle][0].LibraryItem)
	require.Len(t, byKind[domain.RefLighting], 1)
	assert.Equal(t, light.ID, byKind[domain.RefLighting][0].LibraryItem)
	assert.Len(t, byKind[domain.RefProduct], 2, "Products are told apart by name")
}

func shotReferences(t *testing.T, f libraryFixture, shotID string) []domain.Reference {
	t.Helper()
	var spec string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT shot_spec FROM shot WHERE id = ?`, shotID).Scan(&spec))
	return domain.Shot{ShotSpec: []byte(spec)}.References()
}

func TestWorldRepository_SetFieldLockBlocksExtractorAndUserEditsAndAutoReleases(t *testing.T) {
	ctx := context.Background()
	f := newLibraryFixture(t)

	require.NoError(t, f.world.SetFieldLock(ctx, f.projectID, "", domain.TargetLocation, f.locID, "lighting_state", domain.LockLocked))
	require.NoError(t, f.world.SetFieldLock(ctx, f.projectID, "", domain.TargetCamera, "", "lens", domain.LockLocked))
	locks, err := f.world.Locks(ctx, f.projectID)
	require.NoError(t, err)
	assert.True(t, locks.Locked(domain.TargetLocation, f.locID, "lighting_state"))
	assert.True(t, locks.Locked(domain.TargetCamera, "", "lens"))
	assert.False(t, locks.Locked(domain.TargetLocation, f.locID, "weather"))

	assert.ErrorIs(t, f.world.UpdateEntity(ctx, f.projectID, "", domain.TargetLocation, f.locID, map[string]string{"lighting_state": "dark"}), repository.ErrLocked)
	require.NoError(t, f.world.UpdateEntity(ctx, f.projectID, "", domain.TargetLocation, f.locID, map[string]string{"weather": "rain"}))

	applied, _, err := f.world.ApplyStateDiff(ctx, f.projectID, "", f.shotID, f.takeID, domain.StateDiff{Changes: []domain.StateChange{
		{Target: domain.TargetLocation, ID: f.locID, Field: "lighting_state", To: raw(`"dark"`)},
		{Target: domain.TargetLocation, ID: f.locID, Field: "time", To: raw(`"night"`)},
		{Target: domain.TargetCamera, Field: "lens", To: raw(`"85mm"`)},
	}})
	require.NoError(t, err)
	require.Len(t, applied.Changes, 1, "only the unlocked Location field is applied")
	assert.Equal(t, "time", applied.Changes[0].Field)
	assert.JSONEq(t, `"sunset"`, string(applied.Changes[0].From))
	w, err := f.world.World(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, "night", w.Locations[0].Time, "Location State carries over to the next Shot")
	assert.Empty(t, w.Locations[0].LightingState)
	assert.Empty(t, w.Camera.Lens)

	// Switching back to AUTO releases the field, and a repeated LOCK is one row.
	require.NoError(t, f.world.SetFieldLock(ctx, f.projectID, "", domain.TargetLocation, f.locID, "lighting_state", domain.LockLocked))
	assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM lock WHERE project_id = ? AND mode = 'lock'`, f.projectID))
	require.NoError(t, f.world.SetFieldLock(ctx, f.projectID, "", domain.TargetLocation, f.locID, "lighting_state", domain.LockAuto))
	require.NoError(t, f.world.UpdateEntity(ctx, f.projectID, "", domain.TargetLocation, f.locID, map[string]string{"lighting_state": "dark"}))

	assert.ErrorIs(t, f.world.SetFieldLock(ctx, f.projectID, "", domain.TargetLocation, "missing", "time", domain.LockLocked), repository.ErrNotFound)
	assert.ErrorIs(t, f.world.SetFieldLock(ctx, "nope", "", domain.TargetCamera, "", "lens", domain.LockLocked), repository.ErrNotFound)
	assert.ErrorIs(t, f.world.SetFieldLock(ctx, f.projectID, "", domain.TargetCharacter, f.locID, "wardrobe", domain.LockLocked), repository.ErrNotFound, "an ID of another kind")
}

func TestWorldRepository_AssignReferenceHonoursLockAndAppendsAVersion(t *testing.T) {
	ctx := context.Background()
	f := newLibraryFixture(t)
	face, _ := domain.FindReferenceSlot(domain.TargetCharacter, "face")
	x := f.upload(t, domain.RefCharacter, "Mio").AssetID
	y := f.upload(t, domain.RefCharacter, "Mio 2").AssetID

	require.NoError(t, f.world.AssignReference(ctx, f.projectID, "", face, f.charID, x))
	w, err := f.world.World(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, x, w.Characters[0].FaceReferenceAssetID)
	assert.Equal(t, 1, w.Version)

	require.NoError(t, f.world.SetFieldLock(ctx, f.projectID, "", domain.TargetCharacter, f.charID, "identity", domain.LockLocked))
	assert.ErrorIs(t, f.world.AssignReference(ctx, f.projectID, "", face, f.charID, y), repository.ErrLocked)
	assert.ErrorIs(t, f.world.AssignReference(ctx, f.projectID, "", face, f.charID, ""), repository.ErrLocked, "clearing a LOCKed slot is refused too")
	w, _ = f.world.World(ctx, f.projectID, "")
	assert.Equal(t, x, w.Characters[0].FaceReferenceAssetID)

	require.NoError(t, f.world.SetFieldLock(ctx, f.projectID, "", domain.TargetCharacter, f.charID, "identity", domain.LockAuto))
	require.NoError(t, f.world.AssignReference(ctx, f.projectID, "", face, f.charID, ""))
	w, _ = f.world.World(ctx, f.projectID, "")
	assert.Empty(t, w.Characters[0].FaceReferenceAssetID, "an empty asset clears the slot")

	layout, _ := domain.FindReferenceSlot(domain.TargetLocation, "layout")
	assert.ErrorIs(t, f.world.AssignReference(ctx, f.projectID, "", layout, "missing", x), repository.ErrNotFound)
}

// A World State version carries the reference image slots (face_reference ... layout_reference), so the
// restore puts them back too, honours the LOCK of the attribute the slot belongs to, and a version whose
// image is gone leaves the slot as it is (#359).
func TestVersionRepository_RestoreWorldStateBringsTheReferenceSlotsBack(t *testing.T) {
	ctx := context.Background()
	f := newLibraryFixture(t)
	versions := repository.NewVersionRepository(f.db)
	face, _ := domain.FindReferenceSlot(domain.TargetCharacter, "face")
	layout, _ := domain.FindReferenceSlot(domain.TargetLocation, "layout")
	x := f.upload(t, domain.RefCharacter, "Mio").AssetID
	y := f.upload(t, domain.RefCharacter, "Mio 2").AssetID
	l1 := f.upload(t, domain.RefLocation, "Street").AssetID

	require.NoError(t, f.world.AssignReference(ctx, f.projectID, "", face, f.charID, x))
	require.NoError(t, f.world.AssignReference(ctx, f.projectID, "", layout, f.locID, l1)) // version 2: face x, layout l1
	require.NoError(t, f.world.AssignReference(ctx, f.projectID, "", face, f.charID, y))
	require.NoError(t, f.world.AssignReference(ctx, f.projectID, "", layout, f.locID, ""))
	slots := func() (string, string) {
		w, err := f.world.World(ctx, f.projectID, "")
		require.NoError(t, err)
		return w.Characters[0].FaceReferenceAssetID, w.Locations[0].LayoutReferenceAssetID
	}
	gotFace, gotLayout := slots()
	require.Equal(t, y, gotFace)
	require.Empty(t, gotLayout)

	// A LOCKed identity refuses the restore and nothing is written.
	require.NoError(t, f.world.SetFieldLock(ctx, f.projectID, "", domain.TargetCharacter, f.charID, "identity", domain.LockLocked))
	_, err := versions.RestoreWorldStateVersion(ctx, f.projectID, "", 2)
	assert.ErrorIs(t, err, repository.ErrLocked)
	gotFace, gotLayout = slots()
	assert.Equal(t, y, gotFace)
	assert.Empty(t, gotLayout)
	require.NoError(t, f.world.SetFieldLock(ctx, f.projectID, "", domain.TargetCharacter, f.charID, "identity", domain.LockAuto))

	n, err := versions.RestoreWorldStateVersion(ctx, f.projectID, "", 2)
	require.NoError(t, err)
	gotFace, gotLayout = slots()
	assert.Equal(t, x, gotFace)
	assert.Equal(t, l1, gotLayout)
	w, _ := f.world.World(ctx, f.projectID, "")
	assert.Equal(t, n, w.Version, "the restore is a new version that carries the slots")
	assert.Equal(t, x, w.Characters[0].FaceReferenceAssetID)

	// An image that no longer exists is not pointed at again.
	require.NoError(t, f.world.AssignReference(ctx, f.projectID, "", face, f.charID, y))
	require.NoError(t, exec(t, f.db, `DELETE FROM asset WHERE id = ?`, x))
	_, err = versions.RestoreWorldStateVersion(ctx, f.projectID, "", 2)
	require.NoError(t, err)
	gotFace, _ = slots()
	assert.Equal(t, y, gotFace)
}
