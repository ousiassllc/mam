// Package samplingtest runs the detailed settings of the generation test (Issue #973: steps, cfg, sampler, scheduler and seed) from
// the request to the graph the fake ComfyUI server received, and checks what a result keeps of them.
package samplingtest_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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

var ctx = context.Background

const (
	sdxl  = "comfyui:box/sdxl"
	klein = "comfyui:box/klein"
)

// box registers the fake ComfyUI with an SDXL model and FLUX.2 klein (a Flux2Scheduler and a CFGGuider: no scheduler to pick).
func box(t *testing.T, r *mmtest.Rig) *comfyuitest.Fake {
	t.Helper()
	f := comfyuitest.New(t)
	f.OutputNode, f.OutputFile, f.OutputBody = "9", "out.png", comfyuitest.PNG
	require.NoError(t, r.Models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	for name, tpl := range map[string]string{"sdxl": "txt2img-sdxl", "klein": "txt2img-flux2-klein"} {
		tm, ok := localcfg.TemplateByID(tpl)
		require.True(t, ok)
		require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: "box", Name: name, Model: tm.Model}))
	}
	return f
}

func request(models ...string) gentest.Request {
	return gentest.Request{Media: "image", Models: models, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}}
}

func run(t *testing.T, r *mmtest.Rig, req gentest.Request) gentest.BatchStatus {
	t.Helper()
	est, err := r.Gentest.Estimate(ctx(), req)
	require.NoError(t, err)
	ref, err := r.Gentest.Run(ctx(), req, gentest.Confirmation{EstimateID: est.ID, TotalUSD: est.TotalUSD})
	require.NoError(t, err)
	r.Start()
	var st gentest.BatchStatus
	require.Eventually(t, func() bool {
		st, err = r.Gentest.Batch(ctx(), ref.BatchID, true)
		return err == nil && st.Finished
	}, 30*time.Second, 20*time.Millisecond, "the batch did not end")
	return st
}

func input(t *testing.T, f *comfyuitest.Fake, node, name string) string {
	t.Helper()
	return string(f.Graph()[node].Inputs[name])
}

func ptr[T any](v T) *T { return &v }

// With nothing named the model runs its own values, and the result says which they were: that is what makes a result repeatable.
func TestAResultKeepsTheValuesItRanWith(t *testing.T) {
	r := mmtest.New(t)
	f := box(t, r)
	st := run(t, r, request(sdxl))
	require.Equal(t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	got := st.Results[0].Sampling
	require.NotNil(t, got)
	assert.Equal(t, sampling.Settings{Steps: ptr(25), CFG: ptr(7.0), Sampler: "dpmpp_2m", Scheduler: "karras"}, *got)
	assert.Equal(t, "25", input(t, f, "3", "steps"))
	assert.Equal(t, "7", input(t, f, "3", "cfg"))
	assert.Equal(t, `"dpmpp_2m"`, input(t, f, "3", "sampler_name"))
}

// What the user sets goes to the models that take it and into the history; a model that has no scheduler to pick keeps none.
func TestTheValuesReachEveryModelThatTakesThemAndTheHistory(t *testing.T) {
	r := mmtest.New(t)
	box(t, r)
	req := request(sdxl, klein)
	req.Sampling = sampling.Settings{Steps: ptr(12), CFG: ptr(3.5), Sampler: "heun", Scheduler: "normal"}
	st := run(t, r, req)
	require.Equal(t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	by := map[string]sampling.Settings{}
	for _, res := range st.Results {
		require.NotNil(t, res.Sampling, res.Model)
		by[res.Model] = *res.Sampling
	}
	assert.Equal(t, sampling.Settings{Steps: ptr(12), CFG: ptr(3.5), Sampler: "heun", Scheduler: "normal"}, by[sdxl])
	assert.Equal(t, sampling.Settings{Steps: ptr(12), CFG: ptr(3.5), Sampler: "heun"}, by[klein])

	var warned []string
	est, err := r.Gentest.Estimate(ctx(), req)
	require.NoError(t, err)
	for _, w := range est.Warnings {
		if w.Code == gentest.WarnSamplingIgnored {
			warned = append(warned, w.Model)
			assert.Contains(t, w.Message, "スケジューラー")
		}
	}
	assert.Equal(t, []string{klein}, warned, "only the model without a scheduler warns, and says which value it leaves out")
}

// A fixed seed gives every model the same seed, and the n-th of a count the seed plus n: any one of them can be made again.
func TestAFixedSeedIsUsedByEveryModelAndCountsUp(t *testing.T) {
	r := mmtest.New(t)
	f := box(t, r)
	req := request(sdxl)
	req.Seed, req.Count = ptr(int64(777)), 3
	st := run(t, r, req)
	require.Equal(t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	var seeds []int64
	for _, res := range st.Results {
		require.True(t, res.SeedSent)
		seeds = append(seeds, *res.Seed)
	}
	assert.Equal(t, []int64{777, 778, 779}, seeds)
	assert.Equal(t, "779", input(t, f, "3", "seed"), "the last run is the third")
}

func TestWithoutASeedEveryResultGetsItsOwn(t *testing.T) {
	r := mmtest.New(t)
	box(t, r)
	req := request(sdxl)
	req.Count = 2
	st := run(t, r, req)
	require.Equal(t, gentest.StateSucceeded, st.State)
	assert.NotEqual(t, *st.Results[0].Seed, *st.Results[1].Seed)
}

// Values that are out of range are refused before anything is queued or sent to the server.
func TestValuesOutOfRangeAreRefusedBeforeAnythingRuns(t *testing.T) {
	r := mmtest.New(t)
	f := box(t, r)
	over := int64(gentest.SeedMax)
	cases := map[string]func(*gentest.Request){
		"steps 0":              func(q *gentest.Request) { q.Sampling.Steps = ptr(0) },
		"steps 151":            func(q *gentest.Request) { q.Sampling.Steps = ptr(151) },
		"cfg 31":               func(q *gentest.Request) { q.Sampling.CFG = ptr(31.0) },
		"cfg -1":               func(q *gentest.Request) { q.Sampling.CFG = ptr(-1.0) },
		"unknown sampler":      func(q *gentest.Request) { q.Sampling.Sampler = "my_sampler" },
		"unknown scheduler":    func(q *gentest.Request) { q.Sampling.Scheduler = "zigzag" },
		"negative seed":        func(q *gentest.Request) { q.Seed = ptr(int64(-1)) },
		"seed over 32 bits":    func(q *gentest.Request) { q.Seed = ptr(over + 1) },
		"seed + count over it": func(q *gentest.Request) { q.Seed, q.Count = ptr(over), 2 },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			req := request(sdxl)
			edit(&req)
			var invalid *gterr.InvalidError
			_, err := r.Gentest.Estimate(ctx(), req)
			assert.ErrorAs(t, err, &invalid)
		})
	}
	assert.Zero(t, f.Count("POST /prompt"))

	req := request(sdxl)
	req.Seed, req.Count = ptr(over), 1
	_, err := r.Gentest.Estimate(ctx(), req)
	assert.NoError(t, err, "the largest seed is allowed with one result")
}

// The speed mode wins over the steps and the cfg of a request: the mode's values are sent, and the estimate says so.
func TestASpeedModeWinsOverTheStepsAndTheCFG(t *testing.T) {
	r := mmtest.New(t)
	f := comfyuitest.New(t)
	f.OutputFile, f.OutputNode, f.OutputBody = "clip.mp4", "59", []byte("mp4")
	require.NoError(t, r.Models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	tm, _ := localcfg.TemplateByID("img2video-wan22-14b-lightx2v")
	require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: "box", Name: "fast", Model: tm.Model}))
	staged, _, err := r.Gentest.StageReference(comfyuitest.PNG)
	require.NoError(t, err)
	req := gentest.Request{
		Media: "video", Models: []string{"comfyui:box/fast"}, Prompts: []gentest.PromptInput{{Prompt: "waves"}},
		References: []gentest.RefInput{{Source: gentest.FromStaged, ID: staged, Role: "first_frame"}},
		Sampling:   sampling.Settings{Steps: ptr(30), CFG: ptr(7.0), Sampler: "heun"},
	}

	est, err := r.Gentest.Estimate(ctx(), req)
	require.NoError(t, err)
	var msg string
	for _, w := range est.Warnings {
		if w.Code == gentest.WarnSamplingIgnored {
			msg = w.Message
		}
	}
	assert.Contains(t, msg, "ステップ数（速さのモードが決めます）")
	assert.Contains(t, msg, "CFG（速さのモードが決めます）")
	assert.NotContains(t, msg, "サンプラー")

	st := run(t, r, req)
	require.Equal(t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	var steps, cfg float64
	require.NoError(t, json.Unmarshal([]byte(input(t, f, "3", "steps")), &steps))
	require.NoError(t, json.Unmarshal([]byte(input(t, f, "3", "cfg")), &cfg))
	assert.Equal(t, [2]float64{4, 1}, [2]float64{steps, cfg}, "the mode's values, not the request's")
	assert.Equal(t, `"heun"`, input(t, f, "3", "sampler_name"))
	got := st.Results[0].Sampling
	require.NotNil(t, got)
	assert.Nil(t, got.Steps, "the result does not claim steps it did not choose")
	assert.Nil(t, got.CFG)
	assert.Equal(t, "heun", got.Sampler)
}

// A model that takes none of them (a distilled LTX-2.5 runs on fixed sigmas) warns, keeps nothing and still makes the picture; a fixed seed
// it cannot take is said too.
func TestAModelThatTakesNoneWarnsAndRunsAsItIs(t *testing.T) {
	r := mmtest.New(t)
	f := comfyuitest.New(t)
	require.NoError(t, r.Models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	tm, _ := localcfg.TemplateByID("txt2video-ltx-2-5")
	w := *tm.Model.Workflow
	for w.Has(localcfg.ParamSeed) { // a workflow with no seed to put
		for i, a := range w.Assignments {
			if a.Param == localcfg.ParamSeed {
				w.Assignments = append(w.Assignments[:i:i], w.Assignments[i+1:]...)
				break
			}
		}
	}
	tm.Model.Workflow = &w
	require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: "box", Name: "ltx", Model: tm.Model}))

	req := gentest.Request{
		Media: "video", Models: []string{"comfyui:box/ltx"}, Prompts: []gentest.PromptInput{{Prompt: "waves"}},
		Sampling: sampling.Settings{Steps: ptr(30)}, Seed: ptr(int64(5)),
	}
	est, err := r.Gentest.Estimate(ctx(), req)
	require.NoError(t, err)
	codes := map[string]bool{}
	for _, wn := range est.Warnings {
		codes[wn.Code] = true
	}
	assert.True(t, codes[gentest.WarnSamplingIgnored], "the steps have no place in this model")
	assert.True(t, codes[gentest.WarnSeedIgnored], "a model without a seed cannot take a fixed one")
}
