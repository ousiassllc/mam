package samplingtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

// A two-stage video model (Wan 2.2) cannot run on one step (the low noise model would have nothing to do): the request is refused before anything
// runs, and two steps pass. A model that takes one step is not held to this.
func TestATwoStageModelRefusesOneStep(t *testing.T) {
	r := mmtest.New(t)
	f := comfyuitest.New(t)
	require.NoError(t, r.Models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	tm, ok := localcfg.TemplateByID("img2video-wan22-14b-lightx2v-uncensored")
	require.True(t, ok)
	require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: "box", Name: "wan", Model: tm.Model}))
	staged, _, err := r.Gentest.StageReference(comfyuitest.PNG)
	require.NoError(t, err)
	req := gentest.Request{
		Media: "video", Models: []string{"comfyui:box/wan"}, Prompts: []gentest.PromptInput{{Prompt: "waves"}},
		References: []gentest.RefInput{{Source: gentest.FromStaged, ID: staged, Role: "first_frame"}},
	}
	req.Sampling = sampling.Settings{Steps: ptr(1)}
	var invalid *gterr.InvalidError
	_, err = r.Gentest.Estimate(ctx(), req)
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Error(), "2〜")
	assert.Zero(t, f.Count("POST /prompt"))

	req.Sampling = sampling.Settings{Steps: ptr(2)}
	_, err = r.Gentest.Estimate(ctx(), req)
	assert.NoError(t, err)
}
