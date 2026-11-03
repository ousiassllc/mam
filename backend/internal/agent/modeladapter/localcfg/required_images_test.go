package localcfg_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// The templates say which input images they cannot run without (#1009): an image-to-video template needs its first frame, a text-to-video one nothing.
func TestRequiredImagesOfTheTemplates(t *testing.T) {
	var i2v, t2v int
	for _, tpl := range localcfg.Templates() {
		w := tpl.Model.Workflow
		if tpl.Model.Media != localcfg.MediaVideo || w == nil {
			continue
		}
		got := w.RequiredImages()
		if w.Has(localcfg.ParamFirstFrame) {
			i2v++
			assert.Contains(t, got, localcfg.RequiredFirstFrame, tpl.ID)
		} else {
			t2v++
			assert.NotContains(t, got, localcfg.RequiredFirstFrame, tpl.ID)
		}
		if strings.Contains(tpl.ID, "t2v") && !strings.Contains(tpl.ID, "i2v") {
			assert.Empty(t, got, "a text-to-video template needs no picture: "+tpl.ID)
		}
	}
	assert.Positive(t, i2v)
	assert.Positive(t, t2v)
}

func TestRequiredImagesOfAWorkflow(t *testing.T) {
	w := localcfg.Workflow{Assignments: []localcfg.Assignment{
		{Param: localcfg.ParamFirstFrame}, {Param: localcfg.ParamLastFrame}, {Param: localcfg.ParamReferenceImage, Optional: true},
	}}
	assert.Equal(t, []string{"first_frame", "last_frame"}, w.RequiredImages())
	w.Assignments = append(w.Assignments, localcfg.Assignment{Param: localcfg.ParamReferenceImage})
	assert.Equal(t, []string{"first_frame", "last_frame", "reference"}, w.RequiredImages())
	w.NoReference = []localcfg.Override{{}}
	assert.Equal(t, []string{"first_frame", "last_frame"}, w.RequiredImages(), "a workflow that says what a run without a reference changes may run without one")
	assert.Empty(t, localcfg.Workflow{}.RequiredImages())
}
