package screentest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Issue #998: the row of a model in the grid of a batch made from a face reference says how well the face held over its results (the mean of the SFace numbers and how many
// reach the line), so that the effect of a setting is read as a number; a batch without a face reference says nothing.
func TestTheGridSaysHowWellTheFaceHeldForEachModel(t *testing.T) {
	score := func(context.Context, []byte, []byte) (facesim.Comparison, error) {
		return facesim.Comparison{Cosine: 0.52}, nil
	}
	s := newScreen(t, gentesttest.WithReferenceModels(""), gentesttest.WithFaceScore(score))
	id, _, err := s.rig.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	ref, err := s.rig.Confirmed(gentest.Request{
		Media: "image", Models: []string{string(gentesttest.EditModel)}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}},
		References: []gentest.RefInput{{Source: gentest.FromStaged, ID: id, Role: "face"}},
	})
	require.NoError(t, err)
	s.rig.Wait(ref.BatchID)

	body := s.get(paths.GenerationTestPart("batches", ref.BatchID)).Body.String()
	assert.Contains(t, body, `data-testid="gt-row-face"`)
	assert.Contains(t, body, "平均 0.520")
	assert.Contains(t, body, "1/1 件が目安 0.363 以上")

	plain := s.run(batchForm("flux-2"))
	assert.NotContains(t, s.get(paths.GenerationTestPart("batches", plain)).Body.String(), `data-testid="gt-row-face"`, "no reference of the role face: nothing to say")
}
