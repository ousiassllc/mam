package judge_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestEvaluate_TheModelSeesReferencesThenFramesAndTheLocks(t *testing.T) {
	r := newRig(t, passAnswer, true, steady()...)
	in := faceInput(t)
	prev := domain.ShotSpec{ShotObjective: "She stands up"}
	in.Previous = &prev
	in.References = []judge.RefImage{
		{Kind: domain.RefCharacter, Name: "Mira", MimeType: "image/png", Data: []byte("ref1")},
		{Kind: domain.RefProduct, Name: "Serum", MimeType: "image/png", Data: []byte("ref2")},
	}
	in.Locked = map[domain.JudgeDimension]bool{domain.DimCharacterIdentity: true, domain.DimFace: true}
	r.evaluate(t, in)

	require.Len(t, r.llm.requests, 1)
	req := r.llm.requests[0]
	assert.True(t, req.JSON)
	assert.Contains(t, req.User, "A woman runs")
	assert.Contains(t, req.User, "She stands up", "the previous Shot's spec")
	assert.Contains(t, req.User, "character_identity, face", "the LOCKed dimensions")
	assert.Contains(t, req.User, "5.0 seconds long at 24.0 fps")
	assert.Contains(t, req.System, "lip_sync")
	assert.Contains(t, req.System, "reference_reinforcement")
	require.Len(t, req.Images, 2+10, "two references, then the review frames")
	assert.Equal(t, "Reference image (character): Mira", req.Images[0].Caption)
	assert.Equal(t, []byte("ref1"), req.Images[0].Data)
	assert.Contains(t, req.Images[2].Caption, "Video frame 1 of the Take at t=0.00s")
	assert.Equal(t, "image/jpeg", req.Images[2].MimeType)

	// The review is limited to what the setting allows: 12 frames, 4 references, 768 px wide.
	assert.Equal(t, judge.SampleSpec{FPS: 4, MaxFrames: 12, MaxWidth: 768}, r.frames.specs[0])
	assert.Equal(t, judge.SampleSpec{FPS: 4, MaxFrames: 48, MaxWidth: 1280}, r.frames.specs[1])
}

func TestEvaluate_NeverSendsMoreThanTheConfiguredFramesAndReferences(t *testing.T) {
	cfg := judge.DefaultConfig()
	cfg.Frames.ReviewFrames, cfg.Frames.MaxReferenceImages = 3, 1
	r := newRig(t, passAnswer, true, steady()...)
	r.judge = judge.New(r.llm, r.frames, r.engine, cfg, nil)
	in := faceInput(t)
	in.References = []judge.RefImage{{Kind: domain.RefCharacter, MimeType: "image/png", Data: []byte("a")}, {Kind: domain.RefProduct, MimeType: "image/png", Data: []byte("b")}}
	r.evaluate(t, in)

	assert.Len(t, r.llm.requests[0].Images, 1+3)
}

func TestDecide_LeavesAPassingTakeWithoutARepairStrategy(t *testing.T) {
	res := domain.JudgeResult{Issues: []domain.JudgeIssue{{Type: "x", Severity: domain.SeverityLow}}}
	judge.Decide(&res, domain.JudgePass)
	assert.Equal(t, domain.JudgePass, res.Result)
	assert.Nil(t, res.RepairStrategy)

	res = domain.JudgeResult{RepairStrategy: &domain.RepairStrategy{}}
	judge.Decide(&res, domain.JudgeFail)
	assert.Equal(t, domain.JudgeFail, res.Result)
	assert.Nil(t, res.RepairStrategy, "an action-less strategy is no strategy")
}

func TestEvaluate_ScoresOfTheTemplatesAxesAreKeptAndTheRestIsDropped(t *testing.T) {
	r := newRig(t, `{"result":"pass","overall_confidence":0.9,"issues":[],"scores":{"acting":82.5,"story":120,"camera":-3,"emotion":"high","product_accuracy":90}}`, false)
	in := faceInput(t)
	in.Template = domain.TemplateDrama
	res := r.evaluate(t, in)

	assert.Equal(t, map[domain.EvaluationAxis]float64{domain.AxisActing: 82.5}, res.Scores,
		"out of range, non-numeric and foreign axes are dropped")
	assert.Contains(t, r.llm.requests[0].User, "acting", "the axes of the template are asked for")

	in.Template = ""
	assert.Empty(t, r.evaluate(t, in).Scores, "no template: no scores")
}
