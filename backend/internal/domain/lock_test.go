package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func baseSpec() domain.ShotSpec {
	return domain.ShotSpec{
		ShotObjective: "She runs",
		Character:     &domain.SpecActor{ID: "Character_A", Wardrobe: "black_coat"},
		Acting:        []domain.ActingBeat{{Time: "0.0-4.0", Action: "run"}},
		Camera:        domain.SpecCamera{Framing: "medium", Movement: "static"},
		Lighting:      domain.SpecLighting{Mood: "cold"},
	}
}

func TestParameterLocks_AParameterWithoutARowIsAuto(t *testing.T) {
	locks := domain.ParameterLocks{domain.ParamCamera: {Mode: domain.LockLocked}, domain.ParamActing: {Mode: domain.LockManual}}

	assert.Equal(t, domain.LockAuto, locks.Mode(domain.ParamLighting))
	assert.True(t, locks.Locked(domain.ParamCamera))
	assert.False(t, locks.Locked(domain.ParamActing))
	assert.Equal(t, []domain.SpecPart{domain.PartActing}, locks.PartsWith(domain.LockManual))
	assert.Equal(t, []domain.SpecPart{domain.PartCamera}, locks.LockedParts())
}

func TestLockedChanges_ReportsOnlyLockedPartsThatChange(t *testing.T) {
	from := baseSpec()
	to := baseSpec()
	to.Camera.Movement = "handheld"
	to.Acting = []domain.ActingBeat{{Time: "0.0-4.0", Action: "walk"}}
	locks := domain.ParameterLocks{domain.ParamCamera: {Mode: domain.LockLocked}, domain.ParamLighting: {Mode: domain.LockLocked}}

	assert.Equal(t, []domain.LockParameter{domain.ParamCamera}, domain.LockedChanges(locks, from, to), "lighting is LOCKed but unchanged; acting changed but is not LOCKed")
	assert.Empty(t, domain.LockedChanges(domain.ParameterLocks{domain.ParamCamera: {Mode: domain.LockManual}}, from, to), "only LOCK forbids a change")
	assert.Empty(t, domain.LockedChanges(locks, from, from))

	prompt := domain.ParameterLocks{domain.ParamPrompt: {Mode: domain.LockLocked}}
	assert.Equal(t, []domain.LockParameter{domain.ParamPrompt}, domain.LockedChanges(prompt, from, to), "Prompt covers the whole spec")

	empty := baseSpec()
	empty.Acting = nil
	nilActing := baseSpec()
	nilActing.Acting = []domain.ActingBeat{}
	assert.Empty(t, domain.LockedChanges(domain.ParameterLocks{domain.ParamActing: {Mode: domain.LockLocked}}, empty, nilActing), "no list and an empty list are the same")
}

func TestPreserveParts_CopiesTheGivenPartsFromTheCurrentSpec(t *testing.T) {
	current := baseSpec()
	planned := baseSpec()
	planned.Character = &domain.SpecActor{ID: "Character_A", Wardrobe: "red_dress"}
	planned.Camera.Framing = "close-up"
	planned.Acting = []domain.ActingBeat{{Time: "0.0-4.0", Action: "sprint"}}

	got := domain.PreserveParts(planned, current, []domain.SpecPart{domain.PartCharacter, domain.PartActing})

	assert.Equal(t, "black_coat", got.Character.Wardrobe)
	assert.Equal(t, "run", got.Acting[0].Action)
	assert.Equal(t, "close-up", got.Camera.Framing, "parts that are not preserved follow the plan")
	assert.Contains(t, got.Constraints, "preserve_face_identity")
	assert.Empty(t, planned.Constraints, "the plan is not modified")
}

func TestLockSet_StoryParameterFreezesEveryStoryField(t *testing.T) {
	story := domain.LockSet{{Target: domain.TargetStory, Field: string(domain.ParamStory)}: {}}
	assert.True(t, story.Locked(domain.TargetStory, "", "previous_event"))
	assert.True(t, story.Locked(domain.TargetStory, "", domain.StoryFieldScene))
	assert.False(t, story.Locked(domain.TargetCamera, "", "movement"))
	assert.False(t, story.Locked(domain.TargetCharacter, "c1", "emotion"))
}

func TestLockParameter_CoversTheTwelveParametersOfTheSpec(t *testing.T) {
	all := append(domain.ShotLockParameters(), domain.ProjectLockParameters()...)
	assert.Len(t, all, 12)
	seen := map[string]bool{}
	for _, p := range all {
		assert.NotEqual(t, string(p), p.Label(), "%s has a label", p)
		assert.False(t, seen[p.Label()])
		seen[p.Label()] = true
		assert.NotEqual(t, p.ValidForShot(), p.ValidForProject(), "%s belongs to exactly one of Shot and Project", p)
	}
	raw, _ := json.Marshal(domain.LockModes())
	assert.JSONEq(t, `["auto","manual","lock"]`, string(raw))
}
