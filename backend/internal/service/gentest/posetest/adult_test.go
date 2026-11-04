package posetest_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses/posestest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/runner"
)

// run runs a batch with the references and waits for it; the result's detail comes back.
func run(t *testing.T, r *gentesttest.Rig, refs ...gentest.RefInput) record.ResultDetail {
	t.Helper()
	req := imageReq(string(gentesttest.EditModel))
	req.References = refs
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)
	d, err := r.Svc.Result(context.Background(), st.Results[0].ID)
	require.NoError(t, err)
	return d
}

// An input image that was taken from a pose of a suggestive series (sexy, duo) is used, listed in the history, served and taken again only while the age
// gate is open (Issue #1020): with the gate closed the history does not list it, does not serve its copy, and neither the pose nor the copy can be given
// to a new run (the copy of a copy too); the copy stays with its batch, so it comes back when the gate opens.
func TestAnAdultPoseInputIsOutOfSightInTheHistoryWhileTheAgeGateIsClosed(t *testing.T) {
	lib := posestest.New(t)
	lib.InstallAdult(t) // opens the gate, installs both sets
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""), gentesttest.WithPoseLibrary(lib.Lib))
	ctx := context.Background()

	first := run(t, r, poseRef("sexy_S001_front"), poseRef("duo_D001_front.depth"))
	require.Len(t, first.Inputs, 2, "listed while the gate is open")
	assert.Equal(t, "sexy_S001_front", first.Inputs[0].Origin.ID)
	assert.Equal(t, posestest.Picture("sexy_S001_front"), r.Fake.CallsOf(gentesttest.EditModel)[0].Image.Inputs[0].Data)
	copyID := first.Inputs[0].ID
	// the copy of the copy: "この設定でもう一度" puts an earlier batch's input back
	second := run(t, r, gentest.RefInput{Source: gentest.FromBatchInput, ID: copyID, Role: gentest.RolePose})
	require.Len(t, second.Inputs, 1)
	assert.Equal(t, gentest.FromBatchInput, second.Inputs[0].Origin.Kind)
	// and an ordinary pose next to an adult one
	mixed := run(t, r, poseRef("std_P001_front"), poseRef("sexy_S001_diag"))
	require.Len(t, mixed.Inputs, 2)

	lib.Open.Store(false)

	got, err := r.Svc.Result(ctx, first.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Inputs, "no name, no ID, no picture of the pose in the detail of the result")
	got, err = r.Svc.Result(ctx, second.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Inputs, "a copy of a copy is as hidden as the first copy")
	got, err = r.Svc.Result(ctx, mixed.ID)
	require.NoError(t, err)
	require.Len(t, got.Inputs, 1, "an ordinary pose next to it stays")
	assert.Equal(t, "std_P001_front", got.Inputs[0].Origin.ID)

	for _, in := range []string{copyID, second.Inputs[0].ID, mixed.Inputs[1].ID} {
		_, err := r.Svc.InputFile(ctx, in)
		assert.ErrorIs(t, err, gterr.ErrNotFound, "the copy is not served")
	}
	_, err = r.Svc.InputFile(ctx, mixed.Inputs[0].ID)
	assert.NoError(t, err, "an ordinary pose's copy is")

	for name, ref := range map[string]gentest.RefInput{
		"the pose":     poseRef("sexy_S001_front"),
		"its map":      poseRef("duo_D001_front.depth"),
		"the copy":     {Source: gentest.FromBatchInput, ID: copyID, Role: gentest.RolePose},
		"the copy too": {Source: gentest.FromBatchInput, ID: second.Inputs[0].ID, Role: gentest.RolePose},
	} {
		req := imageReq(string(gentesttest.EditModel))
		req.References = []gentest.RefInput{ref}
		_, err := r.Svc.Estimate(ctx, req)
		var invalid *gterr.InvalidError
		require.ErrorAs(t, err, &invalid, name)
		assert.NotContains(t, invalid.Message, "S字立ち", name)
		assert.NotContains(t, invalid.Message, "向き合い", name)
	}

	lib.Open.Store(true)
	got, err = r.Svc.Result(ctx, first.ID)
	require.NoError(t, err)
	assert.Len(t, got.Inputs, 2, "the gate opened again: they are back")
	_, err = r.Svc.InputFile(ctx, copyID)
	assert.NoError(t, err)
}

// A batch that was queued with an adult pose while the gate was open is not run once the gate is closed: nothing is called, every result fails with a
// message that names neither the pose nor the picture.
func TestABatchQueuedWithAnAdultPoseIsNotRunOnceTheAgeGateIsClosed(t *testing.T) {
	lib := posestest.New(t)
	lib.InstallAdult(t)
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""), gentesttest.WithPoseLibrary(lib.Lib))
	r.Queue.Stop() // queued, not run: the job is run by hand below

	req := imageReq(string(gentesttest.EditModel))
	req.References = []gentest.RefInput{poseRef("sexy_S001_front")}
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	lib.Open.Store(false)

	payload, err := json.Marshal(record.Payload{BatchID: ref.BatchID})
	require.NoError(t, err)
	err = r.Svc.RunJob(context.Background(), domain.Job{Payload: payload})
	assert.ErrorIs(t, err, runner.ErrInputHidden)
	assert.NotContains(t, err.Error(), "S字立ち")
	assert.Empty(t, r.Fake.Calls(), "the model is not called")
	st, err := r.Svc.Batch(context.Background(), ref.BatchID, true)
	require.NoError(t, err)
	require.NotEmpty(t, st.Results)
	for _, res := range st.Results {
		assert.Equal(t, gentest.StateFailed, res.Status, "the results are settled, not left queued")
		assert.NotContains(t, res.Error, "sexy_")
	}
}
