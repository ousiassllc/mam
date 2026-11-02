package refs_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/refs"
)

func f64(v float64) *float64 { return &v }

func TestCheckRole(t *testing.T) {
	for _, tc := range []struct {
		name, media, role string
		strength          *float64
		want              string
		ok                bool
	}{
		{"default role", "image", "", nil, "reference", true},
		{"face", "image", "face", f64(0.5), "face", true},
		{"edge strengths", "image", "style", f64(1), "style", true},
		{"zero strength", "image", "edit", f64(0), "edit", true},
		{"unknown", "image", "pose", nil, "pose", false},
		{"frame on image", "image", "first_frame", nil, "first_frame", false},
		{"face on video", "video", "face", nil, "face", false},
		{"video frame", "video", "last_frame", nil, "last_frame", true},
		{"strength on video", "video", "reference", f64(0.5), "reference", false},
		{"too strong", "image", "face", f64(1.1), "face", false},
		{"negative", "image", "face", f64(-0.01), "face", false},
		{"NaN", "image", "face", f64(math.NaN()), "face", false},
	} {
		got, err := refs.CheckRole(tc.media, tc.role, tc.strength)
		assert.Equal(t, tc.want, got, tc.name)
		assert.Equal(t, tc.ok, err == nil, tc.name)
	}
}

func TestCheckLoRA(t *testing.T) {
	l, err := refs.CheckLoRA("image", " a.safetensors ", nil)
	assert.NoError(t, err)
	assert.Equal(t, refs.LoRA{Name: "a.safetensors", Strength: localcfg.DefaultLoRAStrength}, l)
	l, err = refs.CheckLoRA("image", "a", f64(0))
	assert.NoError(t, err)
	assert.Zero(t, l.Strength, "0 is a strength")
	for _, bad := range []struct {
		media, name string
		s           *float64
	}{{"image", "", nil}, {"image", "a", f64(1.5)}, {"video", "a", nil}} {
		_, err := refs.CheckLoRA(bad.media, bad.name, bad.s)
		assert.Error(t, err, "%+v", bad)
	}
}

func TestInputOfMapsTheRoleAndKeepsAZeroStrengthApart(t *testing.T) {
	role, s := refs.InputOf([]string{"reference"}, "face", nil)
	assert.Equal(t, "reference", role)
	assert.Zero(t, s)
	role, s = refs.InputOf(refs.ImageRoles, "face", f64(0))
	assert.Equal(t, "face", role)
	assert.Equal(t, refs.MinStrength, s, "0 would be read as the default")
}

func TestWithAvoidPose(t *testing.T) {
	assert.Equal(t, localcfg.AvoidPoseNegative, refs.WithAvoidPose(""))
	assert.Equal(t, "blurry, "+localcfg.AvoidPoseNegative, refs.WithAvoidPose("blurry"))
}

func TestDecide(t *testing.T) {
	edit := refs.Model{Label: "E", AcceptsReferences: true, MaxReferences: 2, Roles: refs.ImageRoles, SupportsLoRA: true}
	d := refs.Decide(edit, []string{"face", "style", "edit"}, true, true)
	assert.Equal(t, []int{0, 1}, d.Used)
	assert.True(t, d.LoRA)
	assert.True(t, d.AvoidPose)
	assert.Len(t, d.Warnings, 1)
	assert.Equal(t, refs.WarnTooMany, d.Warnings[0].Code)

	d = refs.Decide(refs.Model{Label: "I", AcceptsReferences: true, MaxReferences: 1, Roles: []string{"reference"}}, []string{"face", "style"}, true, false)
	assert.Equal(t, []int{0}, d.Used)
	assert.False(t, d.LoRA)
	codes := []string{}
	for _, w := range d.Warnings {
		codes = append(codes, w.Code)
	}
	assert.ElementsMatch(t, []string{refs.WarnTooMany, refs.WarnRoleUnsupported, refs.WarnLoRAUnsupported}, codes)

	d = refs.Decide(refs.Model{Label: "P"}, []string{"reference"}, false, true)
	assert.Empty(t, d.Used)
	assert.False(t, d.AvoidPose, "no reference used: nothing to avoid")
	assert.Equal(t, refs.WarnInputsDropped, d.Warnings[0].Code)

	d = refs.Decide(edit, nil, false, true)
	assert.False(t, d.AvoidPose, "no reference at all")
	assert.Empty(t, d.Warnings)
}
