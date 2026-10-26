package servicetest_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/projectlink"
)

func TestImportAsReferenceCopiesTheFileRegistersTheLibraryEntryAndRecordsTheTarget(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	project := r.NewProject()
	st := runImages(t, r, "flux-2") // not a favorite: any succeeded result can be imported
	res := st.Results[0]

	out, err := r.Svc.Import(ctx, projectlink.ImportRequest{ResultID: res.ID, ProjectID: project, As: projectlink.AsReference, Kind: "character", Name: " Mio face ", Tags: "hero, close-up"})
	require.NoError(t, err)
	assert.NotEmpty(t, out.ReferenceItemID)
	assert.False(t, out.Assigned)

	// The file is a copy in the project's assets directory; the history still has its own.
	copied := filepath.Join(r.AssetsDir, project, out.AssetID+".png")
	data, err := os.ReadFile(copied)
	require.NoError(t, err)
	assert.Equal(t, gentesttest.PNG, data)
	f, err := r.Svc.ResultFile(ctx, res.ID)
	require.NoError(t, err)
	assert.FileExists(t, f.Path)

	var typ, source, name, tags string
	var shot *string
	require.NoError(t, r.DB.Reader().QueryRow(`SELECT a.type, ri.source, ri.name, ri.tags, ri.source_shot_id FROM reference_item ri JOIN asset a ON a.id = ri.asset_id WHERE ri.id = ?`, out.ReferenceItemID).
		Scan(&typ, &source, &name, &tags, &shot))
	assert.Equal(t, "reference", typ)
	assert.Equal(t, "generated", source)
	assert.Equal(t, "Mio face", name)
	assert.JSONEq(t, `["hero","close-up"]`, tags)
	assert.Nil(t, shot, "source_shot_id stays NULL")

	d, err := r.Svc.Result(ctx, res.ID)
	require.NoError(t, err)
	require.Len(t, d.Imports, 1, "where it went is recorded")
	assert.Equal(t, project, d.Imports[0].ProjectID)
	assert.Equal(t, out.AssetID, d.Imports[0].AssetID)
	assert.Equal(t, out.ReferenceItemID, d.Imports[0].ReferenceItemID)
	assert.Equal(t, "reference", d.Imports[0].Target.As)
	assert.Zero(t, countRows(t, r, "take")+countRows(t, r, "cost_ledger_entry"))
	assert.Empty(t, *r.Assigned)
}

func TestImportAsAssetKeepsTheMediaType(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	project := r.NewProject()
	img := runImages(t, r, "flux-2").Results[0]
	vref, err := r.Confirmed(gentest.Request{Media: "video", Models: []string{"kling-3.0"}, Prompts: []gentest.PromptInput{p("x")}})
	require.NoError(t, err)
	vid := r.Wait(vref.BatchID).Results[0]

	for res, want := range map[string]string{img.ID: "image", vid.ID: "video"} {
		out, err := r.Svc.Import(ctx, projectlink.ImportRequest{ResultID: res, ProjectID: project, As: projectlink.AsAsset})
		require.NoError(t, err)
		var typ, mime, file string
		require.NoError(t, r.DB.Reader().QueryRow(`SELECT type, mime_type, file_path FROM asset WHERE id = ?`, out.AssetID).Scan(&typ, &mime, &file))
		assert.Equal(t, want, typ)
		assert.FileExists(t, filepath.Join(r.AssetsDir, filepath.FromSlash(file)))
		assert.Empty(t, out.ReferenceItemID)
	}
	assert.Zero(t, countRows(t, r, "reference_item"))
}

func TestImportAssignsTheNewReferenceToASlotWhenAsked(t *testing.T) {
	r := gentesttest.New(t)
	project := r.NewProject()
	res := runImages(t, r, "flux-2").Results[0]

	out, err := r.Svc.Import(context.Background(), projectlink.ImportRequest{
		ResultID: res.ID, ProjectID: project, As: projectlink.AsReference, Kind: "character", Name: "Mio",
		Assign: &projectlink.AssignTarget{Target: "character", ID: "char-1", Slot: "face"},
	})
	require.NoError(t, err)
	assert.True(t, out.Assigned)
	assert.Equal(t, []string{project + "|character|char-1|face|" + out.ReferenceItemID}, *r.Assigned)
}

func TestImportRefusesWhatDoesNotFitBeforeWritingAnything(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	project := r.NewProject()
	img := runImages(t, r, "flux-2").Results[0]
	vref, _ := r.Confirmed(gentest.Request{Media: "video", Models: []string{"kling-3.0"}, Prompts: []gentest.PromptInput{p("x")}})
	vid := r.Wait(vref.BatchID).Results[0]
	base := projectlink.ImportRequest{ResultID: img.ID, ProjectID: project, As: projectlink.AsReference, Kind: "character", Name: "n"}
	with := func(f func(*projectlink.ImportRequest)) projectlink.ImportRequest { c := base; f(&c); return c }
	assign := func(target, slot string) *projectlink.AssignTarget {
		return &projectlink.AssignTarget{Target: target, ID: "x", Slot: slot}
	}

	for name, req := range map[string]projectlink.ImportRequest{
		"no way":       with(func(c *projectlink.ImportRequest) { c.As = "" }),
		"bad kind":     with(func(c *projectlink.ImportRequest) { c.Kind = "keyframe" }),
		"no name":      with(func(c *projectlink.ImportRequest) { c.Name = " " }),
		"long name":    with(func(c *projectlink.ImportRequest) { c.Name = string(make([]rune, 101)) + "x" }),
		"long tag":     with(func(c *projectlink.ImportRequest) { c.Tags = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" }),
		"video as ref": with(func(c *projectlink.ImportRequest) { c.ResultID = vid.ID }),
		"wrong slot":   with(func(c *projectlink.ImportRequest) { c.Assign = assign("product", "geometry") }),
		"no such slot": with(func(c *projectlink.ImportRequest) { c.Assign = assign("character", "hat") }),
		"asset slot":   with(func(c *projectlink.ImportRequest) { c.As, c.Assign = projectlink.AsAsset, assign("character", "face") }),
	} {
		_, err := r.Svc.Import(ctx, req)
		var invalid *gterr.InvalidError
		assert.True(t, errors.As(err, &invalid), "%s: %v", name, err)
	}
	_, err := r.Svc.Import(ctx, with(func(c *projectlink.ImportRequest) { c.ProjectID = "nope" }))
	assert.ErrorIs(t, err, gterr.ErrNotFound)
	_, err = r.Svc.Import(ctx, with(func(c *projectlink.ImportRequest) { c.ResultID = "nope" }))
	assert.ErrorIs(t, err, gterr.ErrNotFound)

	assert.Zero(t, countRows(t, r, "asset"), "a refusal writes no asset")
	entries, _ := os.ReadDir(r.AssetsDir)
	assert.Empty(t, entries, "and copies no file")
	assert.Zero(t, countRows(t, r, "gentest_import"))
}

func TestImportRefusesAResultThatIsNotAFile(t *testing.T) {
	r := gentesttest.New(t)
	project := r.NewProject()
	r.Fake.FailWith(domain.ModelFlux2, errors.New("boom"))
	res := runImages(t, r, "flux-2").Results[0]
	require.Equal(t, "failed", res.Status)

	_, err := r.Svc.Import(context.Background(), projectlink.ImportRequest{ResultID: res.ID, ProjectID: project, As: projectlink.AsAsset})
	var c *gterr.ConflictError
	assert.ErrorAs(t, err, &c)
}

func TestAssignRefusalLeavesTheImportInPlaceAndSaysSo(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithAssignError(gterr.ErrNotFound))
	project := r.NewProject()
	res := runImages(t, r, "flux-2").Results[0]

	out, err := r.Svc.Import(context.Background(), projectlink.ImportRequest{
		ResultID: res.ID, ProjectID: project, As: projectlink.AsReference, Kind: "character", Name: "Mio",
		Assign: &projectlink.AssignTarget{Target: "character", ID: "x", Slot: "face"},
	})
	var partial *projectlink.PartialImportError
	require.ErrorAs(t, err, &partial)
	assert.ErrorIs(t, err, gterr.ErrNotFound, "the refusal of the second step is reachable")
	assert.False(t, out.Assigned)
	assert.Equal(t, out.ReferenceItemID, partial.Outcome.ReferenceItemID)
	assert.Equal(t, 1, countRows(t, r, "reference_item"), "the Library image exists")
	assert.Equal(t, 1, countRows(t, r, "gentest_import"))
}
