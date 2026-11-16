package configtest

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// The audio value (Issue #1153) is the sound of a speech-to-video workflow: a video workflow takes it, an image workflow does not, and it is what makes a model
// "audio input" and a sound required.
func TestAudioAssignmentBelongsToAVideoWorkflow(t *testing.T) {
	s2v, ok := localcfg.TemplateByID("audio2video-wan22-s2v-14b")
	require.True(t, ok)
	assert.Empty(t, localcfg.ValidateModel("box/s2v", s2v.Model, comfy()), "the speech-to-video template is a valid video workflow")
	assert.True(t, slices.ContainsFunc(s2v.Model.Workflow.Assignments, func(a localcfg.Assignment) bool { return a.Param == localcfg.ParamAudio }))

	image, ok := localcfg.TemplateByID("txt2img-sdxl")
	require.True(t, ok)
	w := *image.Model.Workflow
	w.Assignments = append(slices.Clone(w.Assignments), localcfg.Assignment{Param: localcfg.ParamAudio, Node: w.Assignments[0].Node, Input: w.Assignments[0].Input})
	m := image.Model
	m.Workflow = &w
	errs := localcfg.ValidateModel("box/img", m, comfy())
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), `"audio" is not a value of a image workflow`)
}

func TestAudioAssignmentMakesTheModelTakeAndRequireASound(t *testing.T) {
	s2v, _ := localcfg.TemplateByID("audio2video-wan22-s2v-14b")
	caps, ok := s2v.Model.VideoCaps()
	require.True(t, ok)
	assert.True(t, caps.AudioInput)
	assert.Equal(t, []string{localcfg.RequiredReference, localcfg.RequiredAudio}, s2v.Model.Workflow.RequiredImages())

	// without the assignment it is a model that takes no sound
	w := *s2v.Model.Workflow
	w.Assignments = slices.DeleteFunc(slices.Clone(w.Assignments), func(a localcfg.Assignment) bool { return a.Param == localcfg.ParamAudio })
	m := s2v.Model
	m.Workflow = &w
	caps, _ = m.VideoCaps()
	assert.False(t, caps.AudioInput)
	assert.NotContains(t, w.RequiredImages(), localcfg.RequiredAudio)

	// the sound is named once however many assignments there are, in the order of the assignments
	two := localcfg.Workflow{Assignments: []localcfg.Assignment{{Param: localcfg.ParamAudio}, {Param: localcfg.ParamFirstFrame}, {Param: localcfg.ParamAudio}}}
	assert.Equal(t, []string{"audio", "first_frame"}, two.RequiredImages())
}
