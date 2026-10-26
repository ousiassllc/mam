package workflowstest_test

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

func TestCreateSavesVersionOneAndPublishesTheModel(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	graph, spec := template(t, "txt2img-sdxl")

	w, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Hero still", Kind: localcfg.MediaImage, API: graph, Spec: &spec})
	require.NoError(t, err)
	assert.Equal(t, 1, w.Version)
	assert.True(t, w.Ready, "%v", w.Problems)
	assert.Equal(t, modelreg.WorkflowID(w.ID, 1), w.ModelID)
	assert.Equal(t, workflows.BundledEndpoint, w.Endpoint)

	id := domain.ModelName(w.ModelID)
	assert.True(t, slices.Contains(domain.ImageModels(), id), "a choice for the image purposes")
	assert.Equal(t, "Hero still (v1)", modelreg.Label(w.ModelID))
	assert.Equal(t, "endpoint:"+workflows.BundledEndpoint, string(modeladapter.VendorOf(id)), "one breaker and limit per endpoint")
	assert.InDelta(t, 0, r.adapter.Config().ImageCost(id), 1e-9, "a local run has no vendor bill")

	_, err = r.svc.Create(ctx, workflows.CreateInput{Name: "hero STILL", Kind: localcfg.MediaImage, API: graph})
	assert.ErrorIs(t, err, workflows.ErrExists, "names are unique ignoring case")
	var invalid *workflows.InvalidError
	_, err = r.svc.Create(ctx, workflows.CreateInput{Name: "x", Kind: localcfg.MediaImage, API: []byte(`{"nodes":[],"links":[]}`)})
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "Save (API Format)", "the editor format is refused with the hint")
	_, err = r.svc.Create(ctx, workflows.CreateInput{Name: "y", Kind: "audio", API: graph})
	require.ErrorAs(t, err, &invalid)
}

func TestVideoWorkflowIsPinnableAndOlderVersionsStayPinnable(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	graph, spec := template(t, "img2video-wan22-5b")
	w, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Clip", Kind: localcfg.MediaVideo, API: graph, Spec: &spec})
	require.NoError(t, err)
	v1 := domain.ModelName(w.ModelID)
	assert.True(t, slices.Contains(domain.VideoModels(), v1))
	assert.Empty(t, r.adapter.Unavailable(v1), "callable at once")
	caps, ok := modeladapter.Capabilities(v1)
	require.True(t, ok, "capabilities by name only, as Prepare needs them")
	assert.True(t, caps.FirstFrame)

	spec.SizeMultiple += 16
	w2, err := r.svc.Update(ctx, w.ID, workflows.UpdateInput{Spec: &spec})
	require.NoError(t, err)
	v2 := domain.ModelName(w2.ModelID)
	assert.Equal(t, 2, w2.Version)
	assert.False(t, slices.Contains(domain.VideoModels(), v1), "only the current version is offered (nothing is pinned to v1)")
	assert.True(t, slices.Contains(domain.VideoModels(), v2))
	assert.Empty(t, r.adapter.Unavailable(v1), "the older version keeps its backend for the Takes and Shots that name it")
	assert.Empty(t, r.adapter.Unavailable(v2))
	_, err = r.svc.PinID(ctx, w.ID, 1)
	var refused *workflows.InvalidError
	assert.ErrorAs(t, err, &refused, "an older version cannot be pinned any more")
	id, err := r.svc.PinID(ctx, w.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, string(v2), id)
	_, err = r.svc.PinID(ctx, w.ID, 9)
	assert.ErrorIs(t, err, workflows.ErrNotFound)

}

func TestEditingBumpsTheVersionOnlyWhenGraphOrAssignmentChanges(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	graph, spec := template(t, "txt2img-sdxl")
	w, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Flow", Kind: localcfg.MediaImage, API: graph, Spec: &spec})
	require.NoError(t, err)

	renamed := "Flow renamed"
	w, err = r.svc.Update(ctx, w.ID, workflows.UpdateInput{Name: &renamed})
	require.NoError(t, err)
	assert.Equal(t, 1, w.Version, "a rename makes no version")
	same := spec
	w, err = r.svc.Update(ctx, w.ID, workflows.UpdateInput{Spec: &same})
	require.NoError(t, err)
	assert.Equal(t, 1, w.Version, "the same assignment makes no version")

	spec.SizeMultiple = 64
	w, err = r.svc.Update(ctx, w.ID, workflows.UpdateInput{Spec: &spec})
	require.NoError(t, err)
	assert.Equal(t, 2, w.Version)

	edited := append([]byte(nil), graph...)
	edited = []byte(string(edited[:len(edited)-1]) + `,"99":{"class_type":"PreviewImage","inputs":{"images":["8",0]}}}`)
	w, err = r.svc.Update(ctx, w.ID, workflows.UpdateInput{API: edited})
	require.NoError(t, err)
	assert.Equal(t, 3, w.Version, "a new graph makes a version")

	versions, err := r.svc.Versions(ctx, w.ID)
	require.NoError(t, err)
	require.Len(t, versions, 3)
	assert.Equal(t, []int{3, 2, 1}, []int{versions[0].Version, versions[1].Version, versions[2].Version}, "newest first")
	assert.True(t, versions[0].Current)
	assert.Equal(t, 0, versions[2].Spec.SizeMultiple, "version 1 keeps what it ran with")
	assert.JSONEq(t, string(graph), string(versions[2].APIJSON))
}

func TestADraftIsEditedInPlaceUntilItCanRun(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	unknown := []byte(`{"4":{"class_type":"SomeCustomNode","inputs":{"x":1}}}`)
	w, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Draft", Kind: localcfg.MediaImage, API: unknown})
	require.NoError(t, err, "a graph the guess does not fit is saved as a draft")
	var refused *workflows.InvalidError
	_, err = r.svc.Create(ctx, workflows.CreateInput{Name: "Bad", Kind: localcfg.MediaImage, API: unknown, Spec: &localcfg.WorkflowSpec{}})
	require.ErrorAs(t, err, &refused, "an explicit assignment must be complete")
	assert.False(t, w.Ready)
	assert.NotEmpty(t, w.Problems)
	assert.False(t, slices.Contains(domain.ImageModels(), domain.ModelName(w.ModelID)))

	graph, spec := template(t, "txt2img-sdxl")
	w, err = r.svc.Update(ctx, w.ID, workflows.UpdateInput{API: graph, Spec: &spec})
	require.NoError(t, err)
	assert.Equal(t, 1, w.Version, "no Take can name a draft, so no version is spent")
	assert.True(t, w.Ready)
	assert.True(t, slices.Contains(domain.ImageModels(), domain.ModelName(w.ModelID)))

	var invalid *workflows.InvalidError
	bad := spec
	bad.OutputNode = "404"
	_, err = r.svc.Update(ctx, w.ID, workflows.UpdateInput{Spec: &bad})
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "output_node")
	got, err := r.svc.Get(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, spec.OutputNode, got.Spec.OutputNode, "a refused edit changed nothing")
}

func TestRoutesFollowTheLatestVersionAndAUsedWorkflowCannotBeDeleted(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	graph, spec := template(t, "txt2img-sdxl")
	w, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Stills", Kind: localcfg.MediaImage, API: graph, Spec: &spec})
	require.NoError(t, err)

	view, err := r.models.View()
	require.NoError(t, err)
	assert.True(t, slices.ContainsFunc(view.ImageModels, func(o modelsettings.ImageOption) bool { return string(o.Model) == w.ModelID }), "listed in the settings choices")
	in := modelsettings.Input{Images: view.Images, Policies: view.Policies}
	in.Images[modelsettings.PurposeNewReference] = modelsettings.ImageRoute{
		Primary: modelsettings.ImageTarget{Model: domain.ModelName(w.ModelID)}, Fallback: modelsettings.ImageTarget{Model: domain.ModelNanoBanana},
	}
	require.NoError(t, r.models.Save(in))
	assert.Equal(t, domain.ModelName(w.ModelID), r.adapter.Config().Images[modeladapter.PurposeNewReference].Primary.Model)

	spec.SizeMultiple = 32
	w2, err := r.svc.Update(ctx, w.ID, workflows.UpdateInput{Spec: &spec})
	require.NoError(t, err)
	require.Equal(t, 2, w2.Version)
	assert.Equal(t, domain.ModelName(w2.ModelID), r.adapter.Config().Images[modeladapter.PurposeNewReference].Primary.Model, "a route means the workflow: it follows the new version")
	list, err := r.svc.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"images.new_reference.primary"}, list[0].UsedBy)

	var invalid *workflows.InvalidError
	require.ErrorAs(t, r.svc.Delete(ctx, w.ID), &invalid)
	assert.Contains(t, invalid.Message, "images.new_reference.primary")
	require.NoError(t, r.models.Reset())
	require.NoError(t, r.svc.Delete(ctx, w.ID))
	assert.False(t, slices.Contains(domain.ImageModels(), domain.ModelName(w2.ModelID)))
	assert.ErrorIs(t, r.svc.Delete(ctx, w.ID), workflows.ErrNotFound)
}
