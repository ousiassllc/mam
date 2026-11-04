// Package availabilitytest pins that the model manager's catalog and the generation test's model list answer "can this model be used" the same way (#1025).
package availabilitytest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

const (
	modelID    = mmtest.OptionalModelID
	workflowID = "comfyui:" + modelmanager.BundledEndpoint + "/wf-opt"
)

func withOptionalModel() mmtest.Option { return mmtest.WithOptionalModel() }

// answers are the two answers to compare: the catalog's (the entry of list_model_catalog / get_catalog_model) and the generation test's (list_generation_models).
func answers(t *testing.T, r *mmtest.Rig) (entry mmview.Entry, listed modelcatalog.Info, direct modelfilter.Availability) {
	t.Helper()
	v, err := r.Svc.Model(modelID)
	require.NoError(t, err)
	entry = mmview.EntryOf(r.Svc, v)
	for _, m := range r.Gentest.Models(context.Background(), "image") {
		if m.ID == workflowID {
			listed = m
		}
	}
	require.Equal(t, workflowID, listed.ID, "the workflow is on the endpoint, so the generation test lists it")
	direct, ok := r.Gentest.Availability(workflowID)
	require.True(t, ok)
	return entry, listed, direct
}

func assertSame(t *testing.T, entry mmview.Entry, listed modelcatalog.Info, direct modelfilter.Availability) {
	t.Helper()
	assert.Equal(t, entry.Available, listed.Available, "catalog and generation test agree on available")
	assert.Equal(t, entry.Available, direct.Available)
	assert.Equal(t, string(entry.Reason), listed.Unavailable)
	assert.Equal(t, entry.Message, listed.UnavailableMessage)
	assert.Equal(t, entry.Availability, direct)
}

func installed(t *testing.T) *mmtest.Rig {
	t.Helper()
	r := mmtest.New(t, withOptionalModel())
	r.Start()
	r.InstallEngine()
	require.Equal(t, "succeeded", string(r.InstallModel(modelID).State))
	return r
}

// The required files are in place and only the optional LoRA is missing: installed on both sides, and the missing file is named.
func TestAnOptionalFileMissingLeavesTheModelInstalledOnBothSides(t *testing.T) {
	r := installed(t)
	require.NoError(t, os.Remove(r.ModelFile(mmtest.LoraD)))

	entry, listed, direct := answers(t, r)
	assert.Equal(t, modelmanager.ModelInstalled, entry.State)
	assert.True(t, entry.Available)
	assert.Equal(t, []string{mmtest.LoraD.Name}, entry.MissingOptional)
	assert.Contains(t, entry.PartialNote, mmtest.LoraD.Name)
	require.NotNil(t, listed.Catalog)
	assert.Equal(t, entry.MissingOptional, listed.Catalog.MissingOptional, "the generation test's list names the same missing file")
	assert.Equal(t, entry.PartialNote, listed.Catalog.PartialNote)
	assertSame(t, entry, listed, direct)

	for _, f := range entry.Files {
		assert.Equal(t, f.Name == mmtest.LoraD.Name, f.Optional, f.Name)
		assert.Equal(t, f.Name != mmtest.LoraD.Name, f.Present, f.Name)
	}
}

// With every file in place nothing is partial.
func TestAnInstalledModelWithEveryFileIsNotPartial(t *testing.T) {
	r := installed(t)
	entry, listed, direct := answers(t, r)
	assert.Equal(t, modelmanager.ModelInstalled, entry.State)
	assert.Empty(t, entry.MissingOptional)
	assert.Empty(t, entry.PartialNote)
	assertSame(t, entry, listed, direct)
}

// A required file is gone: incomplete in the catalog, and the generation test no longer offers the model either.
func TestARequiredFileMissingMakesTheModelUnavailableOnBothSides(t *testing.T) {
	r := installed(t)
	require.NoError(t, os.Remove(r.ModelFile(mmtest.ClipC)))

	entry, listed, direct := answers(t, r)
	assert.Equal(t, modelmanager.ModelIncomplete, entry.State)
	assert.False(t, entry.Available)
	assert.False(t, listed.Available)
	assert.Equal(t, string(modelfilter.ReasonNotInstalled), listed.Unavailable)
	assertSame(t, entry, listed, direct)
}

// The files are on disk and the workflow is on the endpoint, but no install is recorded (the report of #1025: the catalog said "not installed" while the
// generation test said "available"): both say not available, with the same words, which tell the user what to press.
func TestFilesAndWorkflowWithoutARecordAreUnavailableOnBothSides(t *testing.T) {
	r := mmtest.New(t, withOptionalModel())
	r.Start()
	r.InstallEngine()
	for _, f := range []mmtest.File{mmtest.ClipC, mmtest.VaeBC} {
		path := r.ModelFile(f)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, f.Body, 0o600))
	}
	tpl, ok := localcfg.TemplateByID("txt2img-sdxl")
	require.True(t, ok)
	require.NoError(t, r.Models.CreateModel(context.Background(), localmodels.ModelInput{Endpoint: modelmanager.BundledEndpoint, Name: "wf-opt", Model: tpl.Model}))

	entry, listed, direct := answers(t, r)
	assert.Equal(t, modelmanager.ModelNotInstalled, entry.State)
	assert.True(t, entry.FilesInPlace)
	assert.False(t, entry.Available)
	assert.False(t, listed.Available, "the workflow alone does not make the model available")
	assert.Contains(t, entry.Message, "導入の記録がありません")
	assertSame(t, entry, listed, direct)
}
