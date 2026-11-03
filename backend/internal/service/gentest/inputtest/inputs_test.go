// Package inputtest_test tests the check of the input images against the chosen models (#1009), over the local models of gentesttest.WithInputModels.
package inputtest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// The check of the input images (#1009): a request a model cannot use its images in is stopped, one that loses only some is warned about and records what it lost.

func p(s string) gentest.PromptInput { return gentest.PromptInput{Prompt: s} }

func imageReq(models ...string) gentest.Request {
	return gentest.Request{Media: "image", Models: models, Prompts: []gentest.PromptInput{p("a fox")}}
}

func codes(e gentest.Estimate) []string {
	var out []string
	for _, w := range e.Warnings {
		out = append(out, w.Code)
	}
	return out
}

func videoReq(models []string, refs ...gentest.RefInput) gentest.Request {
	return gentest.Request{Media: "video", Models: models, Prompts: []gentest.PromptInput{p("a fox runs")}, References: refs}
}

func staged(t *testing.T, r *gentesttest.Rig, role string) gentest.RefInput {
	t.Helper()
	id, _, err := r.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	return gentest.RefInput{Source: gentest.FromStaged, ID: id, Role: role}
}

// failure is the common error the request was stopped with.
func failure(t *testing.T, err error) *apperr.Error {
	t.Helper()
	e, ok := apperr.As(err)
	require.True(t, ok, "not a common error: %v", err)
	return e
}

func TestAnI2VModelWithoutAFirstFrameIsStoppedBeforeItIsSent(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	_, err := r.Svc.Estimate(context.Background(), videoReq([]string{string(gentesttest.I2VModel)}))
	e := failure(t, err)
	assert.Equal(t, apperr.CodeInputMissing, e.Code)
	assert.Equal(t, "first_frame", e.Args["param"])
	v := e.View(apperr.LangJa)
	assert.Contains(t, v.Cause, "最初のフレームの画像が必要です")
	assert.Contains(t, v.Cause, "画像を添えて", "no image: the fix is to attach one")
	assert.Contains(t, v.Remedy, "最初のフレーム")

	_, err = r.Svc.Run(context.Background(), videoReq([]string{string(gentesttest.I2VModel)}), gentest.Confirmation{})
	assert.Equal(t, apperr.CodeInputMissing, failure(t, err).Code, "Run stops too, before it looks at the confirmation")
	assert.Empty(t, r.Fake.Calls(), "nothing was sent")
}

func TestAReferenceImageForAFirstFrameModelSaysToChangeTheRole(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	_, err := r.Svc.Estimate(context.Background(), videoReq([]string{string(gentesttest.I2VModel)}, staged(t, r, "reference")))
	e := failure(t, err)
	assert.Equal(t, apperr.CodeInputMissing, e.Code)
	v := e.View(apperr.LangJa)
	assert.Contains(t, v.Cause, "最初のフレームの画像が必要です。添えた画像が「参照」になっています。「最初のフレーム」に変えてください。")
	assert.Equal(t, apperr.ClassValidation, v.Class)
	assert.Empty(t, r.Fake.Calls())

	// the role changed: it goes through
	_, err = r.Svc.Estimate(context.Background(), videoReq([]string{string(gentesttest.I2VModel)}, staged(t, r, "first_frame")))
	require.NoError(t, err)
}

func TestEveryImageIsUnusedByATextToVideoModelIsStopped(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	_, err := r.Svc.Estimate(context.Background(), videoReq([]string{string(gentesttest.T2VModel)}, staged(t, r, "first_frame")))
	e := failure(t, err)
	assert.Equal(t, apperr.CodeInputUnused, e.Code)
	v := e.View(apperr.LangJa)
	assert.Contains(t, v.Cause, "文章だけから作るモデルなので、添えた画像は使われません")
	assert.Contains(t, v.Remedy, "I2V")

	// the same without an image runs
	_, err = r.Svc.Estimate(context.Background(), videoReq([]string{string(gentesttest.T2VModel)}))
	require.NoError(t, err)
}

func TestAnImageModelWithoutReferencesStopsWhenNoModelUsesTheImage(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithNoReferenceModel())
	req := imageReq(string(gentesttest.NoReferenceModel))
	req.References = []gentest.RefInput{staged(t, r, "")}
	_, err := r.Svc.Estimate(context.Background(), req)
	assert.Equal(t, apperr.CodeInputUnused, failure(t, err).Code)

	req.Models = []string{string(gentesttest.NoReferenceModel), "nano-banana"}
	e, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err, "one model uses the image: the other only warns")
	assert.Contains(t, codes(e), gentest.WarnInputsDropped)
}

func TestAControlNetModelWithoutItsReferenceIsStopped(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	_, err := r.Svc.Estimate(context.Background(), imageReq(string(gentesttest.ControlModel)))
	e := failure(t, err)
	assert.Equal(t, apperr.CodeInputMissing, e.Code)
	assert.Equal(t, "reference_image", e.Args["param"])
}

// Some of the images are unused by a model: the run goes on with a warning, and the result says what the model did not get.
func TestAPartlyUnusedInputIsAWarningAndIsRecorded(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	first, last := staged(t, r, "first_frame"), staged(t, r, "last_frame")
	req := videoReq([]string{string(gentesttest.I2VModel), string(gentesttest.FLFModel)}, first, last)

	e, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)
	var warned []string
	for _, w := range e.Warnings {
		if w.Code == gentest.WarnInputsDropped {
			warned = append(warned, w.Model)
			assert.Contains(t, w.Message, "最後のフレーム")
		}
	}
	assert.Equal(t, []string{string(gentesttest.I2VModel)}, warned, "the first-frame-only model drops the last frame; the model with both drops nothing")

	ref, err := r.Svc.Run(context.Background(), req, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Len(t, st.Results, 2)
	byModel := map[string]record.ResultView{}
	for _, res := range st.Results {
		byModel[res.Model] = res
	}
	i2v, flf := byModel[string(gentesttest.I2VModel)], byModel[string(gentesttest.FLFModel)]
	assert.Empty(t, flf.UnusedInputs)
	assert.Len(t, flf.UsedInputs, 2)
	require.Len(t, i2v.UnusedInputs, 1)
	assert.Len(t, i2v.UsedInputs, 1)
	assert.Equal(t, "last_frame", i2v.UnusedInputs[0].Role)
	assert.Contains(t, i2v.UnusedInputs[0].Reason, "最後のフレーム")

	d, err := r.Svc.Result(context.Background(), i2v.ID)
	require.NoError(t, err)
	var ids []string
	for _, in := range d.Inputs {
		ids = append(ids, in.ID)
	}
	assert.Contains(t, ids, i2v.UnusedInputs[0].InputID, "the unused input is one of the inputs the detail lists")
	assert.NotContains(t, i2v.UsedInputs, i2v.UnusedInputs[0].InputID)
}

// The screen's notice and the estimate are the same check: the notice of a form that is not finished says what the estimate would.
func TestCheckInputsIsTheSameCheckAsTheEstimate(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	cases := []struct {
		name string
		req  gentest.Request
		code apperr.Code
	}{
		{"i2v without a frame", videoReq([]string{string(gentesttest.I2VModel)}), apperr.CodeInputMissing},
		{"t2v with a frame", videoReq([]string{string(gentesttest.T2VModel)}, staged(t, r, "first_frame")), apperr.CodeInputUnused},
		{"i2v with a frame", videoReq([]string{string(gentesttest.I2VModel)}, staged(t, r, "first_frame")), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			check := r.Svc.CheckInputs(c.req)
			_, err := r.Svc.Estimate(context.Background(), c.req)
			if c.code == "" {
				assert.Nil(t, check.Block)
				assert.NoError(t, err)
				return
			}
			require.NotNil(t, check.Block)
			assert.Equal(t, c.code, check.Block.Code)
			assert.Equal(t, check.Block.View(apperr.LangJa), failure(t, err).View(apperr.LangJa), "the same words")
		})
	}
	// an unfinished form: no prompt yet, the notice still works
	none := gentest.Request{Media: "video", Models: []string{string(gentesttest.T2VModel)}, References: []gentest.RefInput{{Source: gentest.FromStaged, ID: "#1", Role: "first_frame"}}}
	assert.Equal(t, apperr.CodeInputUnused, r.Svc.CheckInputs(none).Block.Code)
	assert.Nil(t, r.Svc.CheckInputs(gentest.Request{Media: "video"}).Block, "no model: nothing to check")
}

// A picture added to the form starts as the first frame while a chosen model needs one, and only for the first picture.
func TestDefaultInputRoleIsTheFirstFrameForAnI2VModel(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	i2v, t2v := string(gentesttest.I2VModel), string(gentesttest.T2VModel)
	assert.Equal(t, "first_frame", r.Svc.DefaultInputRole("video", []string{i2v}, nil))
	assert.Equal(t, "first_frame", r.Svc.DefaultInputRole("video", []string{t2v, i2v}, nil))
	assert.Equal(t, "reference", r.Svc.DefaultInputRole("video", []string{t2v}, nil))
	assert.Equal(t, "reference", r.Svc.DefaultInputRole("video", []string{i2v}, []gentest.RefInput{{Role: "first_frame"}}), "one first frame is enough")
	assert.Equal(t, "reference", r.Svc.DefaultInputRole("video", nil, nil))
	assert.Equal(t, "reference", r.Svc.DefaultInputRole("image", []string{string(gentesttest.ControlModel)}, nil), "an image model's pictures are whole pictures by default")
}
