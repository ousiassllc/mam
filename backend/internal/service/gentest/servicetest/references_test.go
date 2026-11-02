package servicetest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

func f64(v float64) *float64 { return &v }

// stage stages n different uploads (a staged id is consumed by a run) and returns refs of the roles given.
func stage(t *testing.T, r *gentesttest.Rig, roles ...string) []gentest.RefInput {
	t.Helper()
	var out []gentest.RefInput
	for i, role := range roles {
		id, _, err := r.Svc.StageReference(append(append([]byte(nil), gentesttest.PNG...), byte(i)))
		require.NoError(t, err)
		out = append(out, gentest.RefInput{Source: gentest.FromStaged, ID: id, Role: role})
	}
	return out
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

func TestEstimateRefusesAnInvalidRoleStrengthOrLoRA(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""))
	ref := func(role string, s *float64) []gentest.RefInput {
		return []gentest.RefInput{{Source: gentest.FromStaged, ID: "x", Role: role, Strength: s}}
	}
	video := func(refs []gentest.RefInput) gentest.Request {
		return gentest.Request{Media: "video", Models: []string{"kling-3.0"}, Prompts: []gentest.PromptInput{p("x")}, References: refs}
	}
	cases := map[string]gentest.Request{}
	add := func(name string, mut func(*gentest.Request)) {
		q := imageReq("flux-2")
		mut(&q)
		cases[name] = q
	}
	add("unknown role", func(q *gentest.Request) { q.References = ref("pose", nil) })
	add("strength above 1", func(q *gentest.Request) { q.References = ref("face", f64(1.01)) })
	add("negative strength", func(q *gentest.Request) { q.References = ref("reference", f64(-0.1)) })
	add("lora without a name", func(q *gentest.Request) { q.LoRA = &gentest.LoRAInput{Name: "  "} })
	add("lora strength above 1", func(q *gentest.Request) { q.LoRA = &gentest.LoRAInput{Name: "a.safetensors", Strength: f64(2)} })
	cases["face on a video"] = video(ref("face", nil))
	cases["strength on a video"] = video(ref("reference", f64(0.5)))
	cases["lora on a video"] = func() gentest.Request {
		q := video(nil)
		q.LoRA = &gentest.LoRAInput{Name: "a.safetensors"}
		return q
	}()
	cases["avoid pose on a video"] = func() gentest.Request { q := video(nil); q.AvoidReferencePose = true; return q }()
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := r.Svc.Estimate(context.Background(), req)
			var invalid *gterr.InvalidError
			assert.ErrorAs(t, err, &invalid)
		})
	}
}

func TestEstimateAcceptsTheRolesAndStrengthsOfAnImageModel(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""))
	req := imageReq(string(gentesttest.EditModel))
	req.References = stage(t, r, "", "face", "style")
	req.References[1].Strength = f64(0)
	req.References[2].Strength = f64(1)
	req.LoRA = &gentest.LoRAInput{Name: " face.safetensors "}
	e, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "reference", e.Request.References[0].Role, "an empty role is the whole picture")
	require.NotNil(t, e.Request.LoRA)
	assert.Equal(t, "face.safetensors", e.Request.LoRA.Name)
	require.NotNil(t, e.Request.LoRA.Strength)
	assert.InDelta(t, localcfg.DefaultLoRAStrength, *e.Request.LoRA.Strength, 1e-9, "the LoRA strength defaults")
	assert.Empty(t, e.Warnings)
	assert.Nil(t, req.LoRA.Strength, "the caller's request is left as it was")
}

func TestEstimateWarnsWhatAModelDoesNotTakeOfTheReferences(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""))
	edit, i2i, plain := string(gentesttest.EditModel), string(gentesttest.Img2ImgModel), string(gentesttest.PlainModel)
	req := imageReq(edit, i2i, plain, "nano-banana")
	req.References = stage(t, r, "face", "style", "edit", "reference")
	req.LoRA = &gentest.LoRAInput{Name: "a.safetensors"}

	e, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)

	byModel := func(code string) []string {
		var ms []string
		for _, w := range warnings(e, code) {
			ms = append(ms, w.Model)
		}
		return ms
	}
	assert.ElementsMatch(t, []string{edit, i2i}, byModel(gentest.WarnTooManyReference), "4 references: the edit model takes 3, the img2img model 1")
	assert.ElementsMatch(t, []string{i2i, "nano-banana"}, byModel(gentest.WarnRoleUnsupported), "face / style / edit are for the edit model only")
	assert.Equal(t, []string{plain}, byModel(gentest.WarnInputsDropped))
	assert.ElementsMatch(t, []string{i2i, plain, "nano-banana"}, byModel(gentest.WarnLoRAUnsupported))
}

func TestEstimateWarnsTooManyReferencesOfAnEditModel(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""))
	req := imageReq(string(gentesttest.EditModel))
	req.References = stage(t, r, "", "", "", "")
	e, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, warnings(e, gentest.WarnTooManyReference), 1)
	assert.Contains(t, warnings(e, gentest.WarnTooManyReference)[0].Message, "3 枚")
}

func TestAvoidReferencePoseIsAddedToTheNegativeOfAModelThatUsesAReference(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""))
	edit, i2i, plain := string(gentesttest.EditModel), string(gentesttest.Img2ImgModel), string(gentesttest.PlainModel)
	req := gentest.Request{
		Media: "image", Models: []string{edit, i2i, plain}, AvoidReferencePose: true,
		Prompts: []gentest.PromptInput{{Prompt: "a fox"}, {Prompt: "a cat", Negative: "blurry"}},
	}
	req.References = stage(t, r, "", "")

	noNegative := req
	noNegative.Prompts = req.Prompts[:1]
	e, err := r.Svc.Estimate(context.Background(), noNegative)
	require.NoError(t, err)
	folded := warnings(e, gentest.WarnNegativeFolded)
	require.Len(t, folded, 1, "only the img2img model has no negative field and uses a reference; the plain one uses neither")
	assert.Equal(t, i2i, folded[0].Model)

	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State)

	avoid := localcfg.AvoidPoseNegative
	editCalls := r.Fake.CallsOf(gentesttest.EditModel)
	require.Len(t, editCalls, 2)
	negatives := map[string]string{editCalls[0].Image.Prompt: editCalls[0].Image.NegativePrompt, editCalls[1].Image.Prompt: editCalls[1].Image.NegativePrompt}
	assert.Equal(t, avoid, negatives["a fox"], "an empty negative becomes the pose negative")
	assert.Equal(t, "blurry, "+avoid, negatives["a cat"])

	i2iCalls := r.Fake.CallsOf(gentesttest.Img2ImgModel)
	require.Len(t, i2iCalls, 2)
	prompts := map[string]bool{}
	for _, c := range i2iCalls {
		assert.Empty(t, c.Image.NegativePrompt, "no field: it is folded into the prompt")
		prompts[c.Image.Prompt] = true
	}
	assert.Equal(t, map[string]bool{direct.Fold("a fox", avoid): true, direct.Fold("a cat", "blurry, "+avoid): true}, prompts)

	for _, c := range r.Fake.CallsOf(gentesttest.PlainModel) {
		assert.Empty(t, c.Image.NegativePrompt)
		assert.NotContains(t, c.Image.Prompt, avoid, "no reference used: no pose negative")
	}
	for _, res := range st.Results {
		assert.Equal(t, res.Model != string(gentesttest.PlainModel), res.AvoidReferencePose, res.Model)
	}
	for _, res := range st.Results {
		if res.Model == edit && res.Prompt == "a fox" {
			assert.Equal(t, "field", res.NegativeMode)
			assert.Empty(t, res.NegativePrompt, "what the user wrote stays as it was")
		}
	}
}

func TestRunSendsRoleStrengthAndLoRAAndKeepsThemInTheRecord(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""))
	req := imageReq(string(gentesttest.EditModel), string(gentesttest.Img2ImgModel), "nano-banana")
	req.References = stage(t, r, "face", "style", "")
	req.References[0].Strength = f64(0.3)
	req.References[1].Strength = f64(0)
	req.LoRA = &gentest.LoRAInput{Name: "face.safetensors", Strength: f64(0.55)}

	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)

	edit := r.Fake.CallsOf(gentesttest.EditModel)[0].Image
	require.Len(t, edit.Inputs, 3)
	assert.Equal(t, []string{"face", "style", "reference"}, []string{edit.Options.Refs[0].Role, edit.Options.Refs[1].Role, edit.Options.Refs[2].Role})
	assert.InDelta(t, 0.3, edit.Options.Refs[0].Strength, 1e-9)
	assert.Greater(t, edit.Options.Refs[1].Strength, 0.0, "a strength of 0 is not the default 0.7: it is sent as the least")
	assert.Less(t, edit.Options.Refs[1].Strength, 0.1)
	assert.Zero(t, edit.Options.Refs[2].Strength, "not given: the adapter applies the default")
	require.NotNil(t, edit.Options.LoRA)
	assert.Equal(t, localcfg.LoRA{Name: "face.safetensors", Strength: 0.55}, *edit.Options.LoRA)

	i2i := r.Fake.CallsOf(gentesttest.Img2ImgModel)[0].Image
	require.Len(t, i2i.Inputs, 1, "the img2img model takes one reference")
	assert.Equal(t, "reference", i2i.Options.Refs[0].Role, "a role the model does not take is the whole picture")
	assert.InDelta(t, 0.3, i2i.Options.Refs[0].Strength, 1e-9)
	assert.Nil(t, i2i.Options.LoRA, "no LoRA for a model that cannot take one")
	assert.Nil(t, r.Fake.CallsOf("nano-banana")[0].Image.Options.LoRA)

	for _, res := range st.Results {
		if res.Model == string(gentesttest.EditModel) {
			require.NotNil(t, res.LoRA)
			assert.Equal(t, "face.safetensors", res.LoRA.Name)
			assert.InDelta(t, 0.55, res.LoRA.Strength, 1e-9)
		} else {
			assert.Nil(t, res.LoRA, res.Model)
		}
	}
	d, err := r.Svc.Result(context.Background(), st.Results[0].ID)
	require.NoError(t, err)
	require.Len(t, d.Inputs, 3)
	assert.Equal(t, "face", d.Inputs[0].Role)
	require.NotNil(t, d.Inputs[0].Strength)
	assert.InDelta(t, 0.3, *d.Inputs[0].Strength, 1e-9)
	require.NotNil(t, d.Inputs[1].Strength)
	assert.Zero(t, *d.Inputs[1].Strength, "the stored strength is what the user chose")
	assert.Nil(t, d.Inputs[2].Strength)
}

func TestEstimateIDChangesWithTheNewOptions(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""))
	base := imageReq(string(gentesttest.EditModel))
	base.References = stage(t, r, "face")
	id := func(mut func(*gentest.Request)) string {
		q := base
		q.References = append([]gentest.RefInput(nil), base.References...)
		mut(&q)
		e, err := r.Svc.Estimate(context.Background(), q)
		require.NoError(t, err)
		return e.ID
	}
	plain := id(func(*gentest.Request) {})
	assert.Equal(t, plain, id(func(*gentest.Request) {}), "stable")
	assert.NotEqual(t, plain, id(func(q *gentest.Request) { q.AvoidReferencePose = true }))
	assert.NotEqual(t, plain, id(func(q *gentest.Request) { q.References[0].Strength = f64(0.2) }))
	assert.NotEqual(t, plain, id(func(q *gentest.Request) { q.LoRA = &gentest.LoRAInput{Name: "a"} }))
}

func TestModelInfoDescribesTheReferencesAndTheLoRAOfAnImageModel(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""))
	info := func(id string) modelcatalog.Info {
		m, err := r.Svc.Model(context.Background(), "image", id)
		require.NoError(t, err)
		return m
	}
	edit := info(string(gentesttest.EditModel))
	assert.True(t, edit.AcceptsReferences)
	assert.Equal(t, "edit", edit.ReferenceMode)
	assert.Equal(t, 3, edit.MaxReferenceImages)
	assert.Equal(t, []string{"reference", "face", "style", "edit"}, edit.ReferenceRoles)
	assert.True(t, edit.SupportsLoRA)

	i2i := info(string(gentesttest.Img2ImgModel))
	assert.Equal(t, "img2img", i2i.ReferenceMode)
	assert.Equal(t, 1, i2i.MaxReferenceImages)
	assert.Equal(t, []string{"reference"}, i2i.ReferenceRoles)
	assert.False(t, i2i.SupportsLoRA)

	plain := info(string(gentesttest.PlainModel))
	assert.False(t, plain.AcceptsReferences)
	assert.Empty(t, plain.ReferenceMode)
	assert.Zero(t, plain.MaxReferenceImages)
	assert.Empty(t, plain.ReferenceRoles)

	cloud := info("nano-banana")
	assert.True(t, cloud.AcceptsReferences)
	assert.Zero(t, cloud.MaxReferenceImages, "not known")
	assert.Equal(t, []string{"reference"}, cloud.ReferenceRoles)
	assert.False(t, cloud.SupportsLoRA)
}
