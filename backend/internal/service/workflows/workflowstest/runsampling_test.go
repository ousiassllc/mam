package workflowstest_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

func ptr[T any](v T) *T { return &v }

func stills(t *testing.T, r *rig, drop ...localcfg.Param) workflows.Workflow {
	t.Helper()
	graph, spec := template(t, "txt2img-sdxl")
	var kept []localcfg.Assignment
	for _, a := range spec.Assignments {
		keep := true
		for _, d := range drop {
			keep = keep && a.Param != d
		}
		if keep {
			kept = append(kept, a)
		}
	}
	spec.Assignments = kept
	w, err := r.svc.Create(context.Background(), workflows.CreateInput{Name: "Stills", Kind: localcfg.MediaImage, API: graph, Spec: &spec})
	require.NoError(t, err)
	return w
}

// #973: the seed and the sampling values of a run reach the graph, and the record keeps what the run used (the values the input did not
// name are the workflow's own), so that the run can be made again.
func TestRunTakesTheSeedAndTheSamplingValuesAndKeepsWhatItUsed(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	w := stills(t, r)

	rec, err := r.svc.StartRun(ctx, workflows.RunInput{
		WorkflowID: w.ID, Prompt: "a fox", Seed: ptr(int64(31337)), Sampling: sampling.Settings{Steps: ptr(12), Sampler: "euler"},
	})
	require.NoError(t, err)
	assert.EqualValues(t, 31337, rec.Seed)
	require.NotNil(t, rec.Sampling)
	assert.Equal(t, sampling.Settings{Steps: ptr(12), CFG: ptr(7.0), Sampler: "euler", Scheduler: "karras"}, *rec.Sampling)
	require.Equal(t, workflows.RunSucceeded, wait(t, r, rec.ID).Status)

	g := r.comfy.Graph()
	assert.JSONEq(t, "31337", string(g["3"].Inputs["seed"]))
	assert.JSONEq(t, "12", string(g["3"].Inputs["steps"]))
	assert.JSONEq(t, `"euler"`, string(g["3"].Inputs["sampler_name"]))
	assert.JSONEq(t, "7", string(g["3"].Inputs["cfg"]), "not named: the workflow's value stays")
	assert.JSONEq(t, `"karras"`, string(g["3"].Inputs["scheduler"]))

	kept, err := r.svc.Run(ctx, rec.ID)
	require.NoError(t, err)
	assert.Equal(t, rec.Seed, kept.Seed, "the record on disk has them too")
	assert.Equal(t, 12, *kept.Sampling.Steps)
}

func TestRunWithoutASeedPicksOneAndRecordsIt(t *testing.T) {
	r := newRig(t)
	w := stills(t, r)
	a, err := r.svc.StartRun(context.Background(), workflows.RunInput{WorkflowID: w.ID, Prompt: "a fox"})
	require.NoError(t, err)
	require.Equal(t, workflows.RunSucceeded, wait(t, r, a.ID).Status)
	assert.JSONEq(t, jsonNumber(a.Seed), string(r.comfy.Graph()["3"].Inputs["seed"]), "the seed of the graph is the one of the record")
	b, err := r.svc.StartRun(context.Background(), workflows.RunInput{WorkflowID: w.ID, Prompt: "a fox"})
	require.NoError(t, err)
	assert.NotEqual(t, a.Seed, b.Seed)
	assert.JSONEq(t, "25", string(r.comfy.Graph()["3"].Inputs["steps"]), "nothing named: the graph as it stands")
	require.NotNil(t, b.Sampling)
	assert.Equal(t, 25, *b.Sampling.Steps)
}

func TestRunRefusesValuesOutOfRange(t *testing.T) {
	r := newRig(t)
	w := stills(t, r)
	var invalid *workflows.InvalidError
	for name, in := range map[string]workflows.RunInput{
		"steps":     {Sampling: sampling.Settings{Steps: ptr(0)}},
		"cfg":       {Sampling: sampling.Settings{CFG: ptr(99.0)}},
		"sampler":   {Sampling: sampling.Settings{Sampler: "nope"}},
		"seed":      {Seed: ptr(int64(-1))},
		"seed high": {Seed: ptr(int64(workflows.SeedMax) + 1)},
	} {
		in.WorkflowID, in.Prompt = w.ID, "x"
		_, err := r.svc.StartRun(context.Background(), in)
		assert.ErrorAs(t, err, &invalid, name)
	}
	assert.Zero(t, r.comfy.Count("POST /prompt"))
}

// A workflow that has no place for a value says so (a warning of the run) and runs as it is.
func TestRunWarnsOfTheValuesTheWorkflowHasNoPlaceFor(t *testing.T) {
	r := newRig(t)
	w := stills(t, r, localcfg.ParamSteps, localcfg.ParamScheduler)
	rec, err := r.svc.StartRun(context.Background(), workflows.RunInput{
		WorkflowID: w.ID, Prompt: "x", Sampling: sampling.Settings{Steps: ptr(12), CFG: ptr(5.0), Scheduler: "normal"},
	})
	require.NoError(t, err)
	var warned []string
	for _, wn := range rec.Warnings {
		if wn.Kind == workflows.WarnSampling {
			warned = append(warned, wn.Message)
		}
	}
	require.Len(t, warned, 2)
	assert.Contains(t, warned[0], "ステップ数")
	assert.Contains(t, warned[1], "スケジューラー")
	require.Equal(t, workflows.RunSucceeded, wait(t, r, rec.ID).Status)
	g := r.comfy.Graph()
	assert.JSONEq(t, "25", string(g["3"].Inputs["steps"]))
	assert.JSONEq(t, `"karras"`, string(g["3"].Inputs["scheduler"]))
	assert.JSONEq(t, "5", string(g["3"].Inputs["cfg"]), "the one it has a place for is used")
	assert.Nil(t, rec.Sampling.Steps, "what the run used has no steps the workflow did not take")
}

func jsonNumber(n int64) string { return strconv.FormatInt(n, 10) }
