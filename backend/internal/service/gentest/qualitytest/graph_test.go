package qualitytest_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

const sdxl = "comfyui:box/sdxl"

// graphOf runs the request on the fake ComfyUI server and returns the graph it received.
func graphOf(t *testing.T, r *mmtest.Rig, f *comfyuitest.Fake, req gentest.Request) string {
	t.Helper()
	est, err := r.Gentest.Estimate(ctx(), req)
	require.NoError(t, err)
	ref, err := r.Gentest.Run(ctx(), req, gentest.Confirmation{EstimateID: est.ID, TotalUSD: est.TotalUSD})
	require.NoError(t, err)
	r.Start()
	require.Eventually(t, func() bool {
		st, err := r.Gentest.Batch(ctx(), ref.BatchID, true)
		return err == nil && st.Finished
	}, 30*time.Second, 20*time.Millisecond, "the batch did not end")
	raw, err := json.Marshal(f.Graph())
	require.NoError(t, err)
	return string(raw)
}

// 何も触らず生成したときは、今のテンプレートのままのグラフ: 標準の品質（と品質を決めない要求）は、ステップ数も CFG も上書きしない。
func TestTheGraphOfAnUntouchedRequestIsTheTemplatesOwnAndStandardIsTheSame(t *testing.T) {
	r := mmtest.New(t)
	f := comfyuitest.New(t)
	f.OutputNode, f.OutputFile, f.OutputBody = "9", "out.png", comfyuitest.PNG
	require.NoError(t, r.Models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	tpl, ok := localcfg.TemplateByID("txt2img-sdxl")
	require.True(t, ok)
	require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: "box", Name: "sdxl", Model: tpl.Model}))

	req := func(q sampling.Quality) gentest.Request {
		return gentest.Request{Media: "image", Models: []string{sdxl}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}, Seed: ptr(int64(7)), Quality: q}
	}
	plain := graphOf(t, r, f, req(""))
	assert.Equal(t, plain, graphOf(t, r, f, req(sampling.QualityStandard)))

	var g map[string]struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal([]byte(plain), &g))
	assert.Equal(t, "25", string(g["3"].Inputs["steps"]), "the steps the template holds")
	assert.Equal(t, "7", string(g["3"].Inputs["cfg"]))

	fast := graphOf(t, r, f, req(sampling.QualityFast))
	require.NoError(t, json.Unmarshal([]byte(fast), &g))
	assert.Equal(t, "15", string(g["3"].Inputs["steps"]), "the preset reaches the sampler node")
	assert.Equal(t, "7", string(g["3"].Inputs["cfg"]))
}
