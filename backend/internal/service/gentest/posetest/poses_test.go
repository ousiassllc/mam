// Package posetest_test tests the pose pictures as input images of a run (#986) over a real Service with the editing model of gentesttest.
// It is apart from servicetest because that directory is full (linterly).
package posetest_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses/posestest"
)

func imageReq(models ...string) gentest.Request {
	return gentest.Request{Media: "image", Models: models, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}}
}

func f64(v float64) *float64 { return &v }

// stage stages an upload and returns a ref of the role given.
func stage(t *testing.T, r *gentesttest.Rig, role string) []gentest.RefInput {
	t.Helper()
	id, _, err := r.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	return []gentest.RefInput{{Source: gentest.FromStaged, ID: id, Role: role}}
}

func warnings(e gentest.Estimate, code string) []gentest.Warning {
	var out []gentest.Warning
	for _, w := range e.Warnings {
		if w.Code == code {
			out = append(out, w)
		}
	}
	return out
}

func poseRef(id string) gentest.RefInput {
	return gentest.RefInput{Source: gentest.FromPoseLibrary, ID: id, Role: gentest.RolePose}
}

// A pose picture of the library goes to an editing model next to the character's picture, with the role "pose": the run copies the
// picture into the batch and the record says where it came from (#986).
func TestAPosePictureIsGivenToAnEditModelWithTheRolePose(t *testing.T) {
	lib := posestest.New(t)
	lib.Install(t)
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""), gentesttest.WithPoseLibrary(lib.Lib))
	req := imageReq(string(gentesttest.EditModel))
	req.References = append(stage(t, r, "reference"), poseRef("std_P001_front"))
	req.References[1].Strength = f64(1)

	e, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, e.Warnings, "the editing model takes the role pose")

	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)

	call := r.Fake.CallsOf(gentesttest.EditModel)[0].Image
	require.Len(t, call.Inputs, 2)
	assert.Equal(t, "reference", call.Options.Refs[0].Role)
	assert.Equal(t, "pose", call.Options.Refs[1].Role)
	assert.InDelta(t, 1, call.Options.Refs[1].Strength, 1e-9)
	assert.Equal(t, posestest.Picture("std_P001_front"), call.Inputs[1].Data, "the bytes are the library's picture")
	assert.Equal(t, "image/webp", call.Inputs[1].MimeType)

	d, err := r.Svc.Result(context.Background(), st.Results[0].ID)
	require.NoError(t, err)
	require.Len(t, d.Inputs, 2)
	assert.Equal(t, "pose", d.Inputs[1].Role)
	assert.Equal(t, gentest.FromPoseLibrary, d.Inputs[1].Origin.Kind)
	assert.Equal(t, "std_P001_front", d.Inputs[1].Origin.ID)
}

// A model that does not take the role uses the chart as a whole picture, and the estimate says so; the pose option "do not copy" then
// concerns it as much as any other reference.
func TestAModelWithoutTheRolePoseUsesThePoseChartAsAWholePicture(t *testing.T) {
	lib := posestest.New(t)
	lib.Install(t)
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""), gentesttest.WithPoseLibrary(lib.Lib))
	req := imageReq(string(gentesttest.Img2ImgModel))
	req.References = []gentest.RefInput{poseRef("std_P002_side")}

	e, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, warnings(e, gentest.WarnRoleUnsupported), 1)
	assert.Contains(t, warnings(e, gentest.WarnRoleUnsupported)[0].Message, "ポーズ")

	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State)
	assert.Equal(t, "reference", r.Fake.CallsOf(gentesttest.Img2ImgModel)[0].Image.Options.Refs[0].Role)
}

// The refusals say what to do: the library that is not installed, a picture that is not one of the library's, and no library at all.
func TestAPosePictureOfALibraryThatCannotBeUsedIsRefusedInWords(t *testing.T) {
	uninstalled := posestest.New(t)
	installed := posestest.New(t)
	installed.Install(t)
	for name, tc := range map[string]struct {
		opt  gentesttest.Option
		id   string
		want string
	}{
		"not installed":         {gentesttest.WithPoseLibrary(uninstalled.Lib), "std_P001_front", "導入されていません"},
		"a made-up file":        {gentesttest.WithPoseLibrary(installed.Lib), "../../etc/passwd", "ID が正しくありません"},
		"a suggestive pose":     {gentesttest.WithPoseLibrary(installed.Lib), "std_P003_front", "ポーズ集にありません"},
		"an angle without file": {gentesttest.WithPoseLibrary(installed.Lib), "upper_U001_below", "ポーズ集にありません"},
		"no library":            {gentesttest.WithPoseLibrary(nil), "std_P001_front", "使えません"},
	} {
		t.Run(name, func(t *testing.T) {
			r := gentesttest.New(t, gentesttest.WithReferenceModels(""), tc.opt)
			req := imageReq(string(gentesttest.EditModel))
			req.References = []gentest.RefInput{poseRef(tc.id)}
			_, err := r.Svc.Estimate(context.Background(), req)
			var invalid *gterr.InvalidError
			require.ErrorAs(t, err, &invalid)
			assert.Contains(t, invalid.Message, tc.want)
		})
	}
}

// The batch keeps its own copy: deleting the library afterwards does not take the inputs out of the history.
func TestTheHistoryKeepsItsOwnCopyOfAPosePicture(t *testing.T) {
	lib := posestest.New(t)
	lib.Install(t)
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""), gentesttest.WithPoseLibrary(lib.Lib))
	req := imageReq(string(gentesttest.EditModel))
	req.References = []gentest.RefInput{poseRef("upper_U001_front")}
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State)
	require.NoError(t, os.RemoveAll(lib.Dir))

	d, err := r.Svc.Result(context.Background(), st.Results[0].ID)
	require.NoError(t, err)
	require.Len(t, d.Inputs, 1)
	f, err := r.Svc.InputFile(context.Background(), d.Inputs[0].ID)
	require.NoError(t, err)
	got, err := os.ReadFile(f.Path)
	require.NoError(t, err)
	assert.Equal(t, posestest.Picture("upper_U001_front"), got)
}
