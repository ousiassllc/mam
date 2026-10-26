package gentest_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func importForm(project string, extra url.Values) url.Values {
	f := url.Values{"project_id": {project}, "as": {"reference"}, "kind": {"character"}, "name": {"Mio"}, "tags": {"hero, red"}}
	for k, v := range extra {
		f[k] = v
	}
	return f
}

func TestImportAsReferenceAndAssignedToASlot(t *testing.T) {
	s := newScreen(t)
	id := s.results(s.run(batchForm("flux-2")))[0].ID
	project := s.rig.NewProject()
	page := s.page(paths.GenerationTestPart("results", id))
	assert.Contains(t, page, `data-testid="gt-import"`)
	assert.Contains(t, page, `value="`+project+`"`, "the Project is offered")

	rec := s.do(http.MethodPost, paths.GenerationTestPart("results", id, "import"), importForm(project, url.Values{
		"assign_target": {"character"}, "assign_id": {"c1"}, "assign_slot": {"face"},
	}))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "スロットに割り当てました")
	assert.Equal(t, []string{project + "|character|c1|face|" + referenceOf(t, s, project)}, *s.rig.Assigned)
	assert.Contains(t, s.page(paths.GenerationTestPart("results", id)), "参照画像（Mio）として取り込みました", "the detail says where it went")
}

func referenceOf(t *testing.T, s *screen, project string) string {
	t.Helper()
	images, err := s.rig.Svc.ProjectImages(t.Context(), project)
	require.NoError(t, err)
	require.Len(t, images, 1)
	return images[0].ReferenceItemID
}

func TestImportAsAnAssetNeedsNoLibraryFields(t *testing.T) {
	s := newScreen(t)
	id := s.results(s.run(batchForm("flux-2")))[0].ID
	project := s.rig.NewProject()
	rec := s.do(http.MethodPost, paths.GenerationTestPart("results", id, "import"), url.Values{"project_id": {project}, "as": {"asset"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="gt-import-done"`)
	assert.Empty(t, *s.rig.Assigned)
}

func TestImportRefusalsKeepTheFormAndSayWhy(t *testing.T) {
	s := newScreen(t)
	id := s.results(s.run(batchForm("flux-2")))[0].ID
	project := s.rig.NewProject()
	for name, tc := range map[string]struct {
		form   url.Values
		status int
	}{
		"no name":          {importForm(project, url.Values{"name": {""}}), http.StatusUnprocessableEntity},
		"unknown project":  {importForm("nope", nil), http.StatusNotFound},
		"slot of the kind": {importForm(project, url.Values{"kind": {"product"}, "assign_target": {"character"}, "assign_id": {"c1"}, "assign_slot": {"face"}}), http.StatusUnprocessableEntity},
		"bad as":           {url.Values{"project_id": {project}, "as": {"nowhere"}}, http.StatusUnprocessableEntity},
	} {
		rec := s.do(http.MethodPost, paths.GenerationTestPart("results", id, "import"), tc.form)
		assert.Equal(t, tc.status, rec.Code, name+": "+rec.Body.String())
		assert.Contains(t, rec.Body.String(), `data-testid="gt-import-error"`, name)
		assert.Contains(t, rec.Body.String(), `data-testid="gt-import-submit"`, name+": the form stays")
	}
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodPost, paths.GenerationTestPart("results", "nope", "import"), importForm(project, nil)).Code)
}

func TestImportToALockedSlotIs409AndTheImageStaysImported(t *testing.T) {
	s := newScreen(t, gentesttest.WithAssignError(service.ErrLocked))
	id := s.results(s.run(batchForm("flux-2")))[0].ID
	project := s.rig.NewProject()
	rec := s.do(http.MethodPost, paths.GenerationTestPart("results", id, "import"), importForm(project, url.Values{
		"assign_target": {"character"}, "assign_id": {"c1"}, "assign_slot": {"face"},
	}))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "取り込みました")
	assert.Contains(t, rec.Body.String(), "LOCK")
	assert.NotEmpty(t, referenceOf(t, s, project), "the Library image exists")
}
