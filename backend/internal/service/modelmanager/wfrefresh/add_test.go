package wfrefresh_test

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/wfrefresh"
)

// fakeStore is the saved models (by name) and the install record.
type fakeStore struct {
	models  map[string]localcfg.Model
	record  map[string]store.InstalledModel
	created []string
	err     error
}

func (f *fakeStore) CreateModel(_ context.Context, in localmodels.ModelInput) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.models[in.Name]; ok {
		return localmodels.ErrExists
	}
	f.models[in.Name] = in.Model
	f.created = append(f.created, in.Name)
	return nil
}

func (f *fakeStore) InstalledModels() map[string]store.InstalledModel { return f.record }

func (f *fakeStore) AddWorkflows(id string, names []string) error {
	inst := f.record[id]
	inst.Workflows = append(inst.Workflows, names...)
	f.record[id] = inst
	return nil
}

// installedBefore is the state of a model installed before the catalog got the workflows in skip: its files as the catalog lists them, the
// workflows of the catalog but those, each saved as the template has it.
func installedBefore(t *testing.T, cat *catalog.Catalog, id string, skip ...string) *fakeStore {
	t.Helper()
	m, ok := cat.Model(id)
	require.True(t, ok, id)
	f := &fakeStore{models: map[string]localcfg.Model{}, record: map[string]store.InstalledModel{}}
	inst := store.InstalledModel{LicenseID: m.License.ID}
	for _, file := range m.Files {
		inst.Files = append(inst.Files, store.InstalledFile{Path: file.Path(), SHA256: file.SHA256, Size: file.Size})
	}
	for _, w := range m.Workflows {
		if slices.Contains(skip, w.Name) {
			continue
		}
		model, err := w.Model(localcfg.TemplateByID)
		require.NoError(t, err)
		f.models[w.Name] = model
		inst.Workflows = append(inst.Workflows, w.Name)
	}
	f.record[id] = inst
	return f
}

// #990: a model installed before #974 gets sdxl-base-inpaint and flux1-dev-inpaint at the start, without being installed again.
func TestAddMissingShowsTheInpaintWorkflowsOfAnInstalledModel(t *testing.T) {
	cat := catalog.Default()
	for id, name := range map[string]string{"sdxl-base": "sdxl-base-inpaint", "flux1-dev": "flux1-dev-inpaint"} {
		f := installedBefore(t, cat, id, name)
		require.NotContains(t, f.models, name)
		var w warnings

		wfrefresh.AddMissing(context.Background(), f, f, cat, endpoint, w.warn)

		assert.Contains(t, f.models, name, id)
		assert.Equal(t, []string{name}, f.created, id)
		assert.Contains(t, f.record[id].Workflows, name, "the record has it, so that a delete and the age gate cover it: %s", id)
		assert.Zero(t, w.n, id)

		wfrefresh.AddMissing(context.Background(), f, f, cat, endpoint, w.warn) // a second start changes nothing
		assert.Equal(t, []string{name}, f.created, id)
		assert.Equal(t, 1, countOf(f.record[id].Workflows, name), id)
	}
}

func countOf(xs []string, x string) int {
	n := 0
	for _, v := range xs {
		if v == x {
			n++
		}
	}
	return n
}

// A copy of that name that exists is the user's: it is left as it is and its name is recorded.
func TestAddMissingNeverOverwritesACopyThatExists(t *testing.T) {
	cat := catalog.Default()
	f := installedBefore(t, cat, "sdxl-base", "sdxl-base-inpaint")
	mine := editGraph(t, template(t, "inpaint-sdxl"), "3", "steps", 7)
	mine.CostPerImage = 0.5
	f.models["sdxl-base-inpaint"] = mine

	wfrefresh.AddMissing(context.Background(), f, f, cat, endpoint, (&warnings{}).warn)

	assert.Equal(t, mine, f.models["sdxl-base-inpaint"])
	assert.Empty(t, f.created)
	assert.Contains(t, f.record["sdxl-base"].Workflows, "sdxl-base-inpaint")
}

// A model that is not installed, one that lacks a file the catalog lists now, and a workflow the user deleted (its name is recorded) get nothing.
func TestAddMissingLeavesWhatIsNotInstalledOrNotComplete(t *testing.T) {
	cat := catalog.Default()

	none := installedBefore(t, cat, "sdxl-base", "sdxl-base-inpaint")
	delete(none.record, "sdxl-base")
	wfrefresh.AddMissing(context.Background(), none, none, cat, endpoint, (&warnings{}).warn)
	assert.Empty(t, none.created, "not installed")

	stale := installedBefore(t, cat, "sdxl-base", "sdxl-base-inpaint")
	inst := stale.record["sdxl-base"]
	inst.Files = append([]store.InstalledFile(nil), inst.Files...)
	inst.Files[0].SHA256 = "0000"
	stale.record["sdxl-base"] = inst
	wfrefresh.AddMissing(context.Background(), stale, stale, cat, endpoint, (&warnings{}).warn)
	assert.Empty(t, stale.created, "the installed file is not the one the catalog pins")

	deleted := installedBefore(t, cat, "sdxl-base")
	delete(deleted.models, "sdxl-base-inpaint")
	wfrefresh.AddMissing(context.Background(), deleted, deleted, cat, endpoint, (&warnings{}).warn)
	assert.Empty(t, deleted.created, "the record has the name: the user deleted it")
}

// A pinned model file is not a failure and is not written.
func TestAddMissingDoesNotWarnOnAPinnedFile(t *testing.T) {
	cat := catalog.Default()
	f := installedBefore(t, cat, "sdxl-base", "sdxl-base-inpaint")
	f.err = localmodels.ErrPinned
	var w warnings

	wfrefresh.AddMissing(context.Background(), f, f, cat, endpoint, w.warn)

	assert.Zero(t, w.n)
	assert.NotContains(t, f.record["sdxl-base"].Workflows, "sdxl-base-inpaint")
}
