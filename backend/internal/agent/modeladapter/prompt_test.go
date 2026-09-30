package modeladapter_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

var cjk = regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}]`)

func fullJob() modeladapter.VideoJob {
	img := func(s string) modeladapter.Input { return modeladapter.Input{Data: []byte(s), MimeType: "image/png"} }
	refs := make([]modeladapter.Input, 12)
	for i := range refs {
		refs[i] = img("ref")
	}
	first, last := img("first"), img("last")
	return modeladapter.VideoJob{
		Spec: domain.ShotSpec{
			ShotObjective: "Character notices something behind her and starts running",
			Character:     &domain.SpecActor{ID: "Character_A", Identity: "locked", Wardrobe: "black_coat_v2", Hair: "inherit_previous"},
			Product:       &domain.SpecProduct{ID: "bottle", Label: "MAM", Orientation: "label facing camera"},
			Acting: []domain.ActingBeat{
				{Time: "0.0-0.7", Action: "subtle reaction"},
				{Time: "0.7-4.0", Action: "begin running"},
			},
			Camera:      domain.SpecCamera{Lens: "35mm", Framing: "medium", Movement: "slow_push_in"},
			Lighting:    domain.SpecLighting{Inherit: "previous_shot", Mood: "cold_blue"},
			Continuity:  domain.SpecContinuity{StartFromPreviousFinalState: true},
			Constraints: []string{"preserve_face_identity", "do_not_add_characters"},
		},
		DurationSeconds: 4, AspectRatio: "9:16", FirstFrame: &first, LastFrame: &last, References: refs,
	}
}

func TestPrepareRendersTheSpecAsAnEnglishPromptForKling(t *testing.T) {
	req, err := modeladapter.Prepare(klingTarget, fullJob())
	require.NoError(t, err)

	assert.False(t, cjk.MatchString(req.Prompt), "model prompts are English")
	assert.Contains(t, req.Prompt, "starts running")
	assert.Contains(t, req.Prompt, "0.0-0.7s: subtle reaction")
	assert.Contains(t, req.Prompt, "35mm lens")
	assert.Contains(t, req.Prompt, "slow push in")
	assert.Contains(t, req.Prompt, "keep the character's face identical")
	assert.NotContains(t, req.Prompt, "inherit", "identifiers are not leaked into the prompt")
	assert.Contains(t, req.NegativePrompt, "distorted face")
	assert.Contains(t, req.NegativePrompt, "add characters", "do_not_* constraints also go to the negative prompt")
	assert.Equal(t, 5.0, req.DurationSeconds)
	assert.NotNil(t, req.FirstFrame)
	assert.NotNil(t, req.LastFrame)
	assert.Empty(t, req.References, "Kling has no free reference images")
	assert.Equal(t, "9:16", req.AspectRatio)
}

func TestPrepareForSeedanceKeepsReferencesWithinTheModelLimitAndFoldsTheNegativePrompt(t *testing.T) {
	job := fullJob()
	job.FirstFrame, job.LastFrame = nil, nil
	req, err := modeladapter.Prepare(seedanceTarget, job)
	require.NoError(t, err)

	assert.Len(t, req.References, 9, "Seedance takes 12 reference files: 9 are used for images")
	assert.Empty(t, req.NegativePrompt)
	assert.Contains(t, req.Prompt, "Avoid: blurry")
	assert.Equal(t, 4.0, req.DurationSeconds)
	assert.Equal(t, "720p", req.Resolution)
}

func TestPrepareKeepsFramesAndReferencesForSeedance(t *testing.T) {
	req, err := modeladapter.Prepare(seedanceTarget, fullJob())
	require.NoError(t, err)
	assert.NotNil(t, req.FirstFrame)
	assert.NotNil(t, req.LastFrame)
	assert.Len(t, req.References, 9, "Seedance takes the first frame and up to 9 other reference images")

	kling, err := modeladapter.Prepare(klingTarget, fullJob())
	require.NoError(t, err)
	assert.NotNil(t, kling.FirstFrame)
	assert.Empty(t, kling.References, "Kling takes frames only")
}

func TestSnapDurationChoosesTheShortestSupportedLengthThatFits(t *testing.T) {
	caps, ok := modeladapter.Capabilities(domain.ModelKling3)
	require.True(t, ok)
	assert.Equal(t, 5.0, caps.SnapDuration(2))
	assert.Equal(t, 5.0, caps.SnapDuration(5))
	assert.Equal(t, 10.0, caps.SnapDuration(5.1))
	assert.Equal(t, 10.0, caps.SnapDuration(30), "longer Shots use the longest clip")
}

func TestPrepareRejectsAModelWithoutAdapter(t *testing.T) {
	_, err := modeladapter.Prepare(modeladapter.VideoTarget{Model: domain.ModelName("veo-3"), Resolution: "1080p"}, baseJob())
	require.Error(t, err)
}

func TestPrepareForRunwayUsesTheFirstFrameAndKeepsTheAvoidListWithinTheLimit(t *testing.T) {
	job := fullJob()
	job.Spec.ShotObjective = strings.Repeat("A very long objective. ", 80)
	req, err := modeladapter.Prepare(runwayTarget, job)
	require.NoError(t, err)

	assert.NotNil(t, req.FirstFrame)
	assert.Nil(t, req.LastFrame, "Gen-4.5 takes a first frame only")
	assert.Empty(t, req.References)
	assert.Empty(t, req.NegativePrompt)
	assert.LessOrEqual(t, len([]rune(req.Prompt)), 1000, "promptText is limited to 1000 characters")
	assert.Contains(t, req.Prompt, "distorted face", "the avoid list survives the cut")
	assert.Equal(t, 4.0, req.DurationSeconds, "any whole second from 2 to 10 is available")
}
