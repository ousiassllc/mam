package judge_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/facesim"
)

const passAnswer = `{"result":"pass","overall_confidence":0.93,"issues":[],"preserve":["character","acting"]}`

// rig is a Judge over fake frames, a fake face engine and a fake Reasoning LLM.
type rig struct {
	judge  *judge.Judge
	llm    *fakeLLM
	frames *fakeFrames
	engine *fakeEngine
}

// newRig makes a 5-second, 24 fps Take whose ten frames show a face of the given similarities.
func newRig(t *testing.T, answer string, withEngine bool, sims ...float64) *rig {
	t.Helper()
	r := &rig{llm: &fakeLLM{text: answer}, engine: &fakeEngine{}}
	r.frames = &fakeFrames{pictures: frames(t, sims...), duration: 5, fps: 24}
	var engine facesim.Engine
	if withEngine {
		engine = r.engine
	}
	r.judge = judge.New(r.llm, r.frames, engine, judge.DefaultConfig(), nil)
	return r
}

func steady() []float64 { return []float64{0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95} }

// drifting shows the face far from the reference (0.2) for frames 4-7, i.e. 2.0-4.0 s.
func drifting() []float64 { return []float64{0.95, 0.95, 0.95, 0.95, 0.2, 0.2, 0.2, 0.2, 0.95, 0.95} }

func faceInput(t *testing.T) judge.Input {
	t.Helper()
	return judge.Input{
		VideoPath: "take.mp4",
		Spec:      domain.ShotSpec{ShotObjective: "A woman runs", Camera: domain.SpecCamera{Framing: "medium"}},
		FaceSubjects: []judge.FaceSubject{
			{ID: "char-a", ReferenceImages: [][]byte{facePicture(t, 1.0), facePicture(t, 1.0)}},
		},
	}
}

func (r *rig) evaluate(t *testing.T, in judge.Input) domain.JudgeResult {
	t.Helper()
	res, err := r.judge.Evaluate(context.Background(), in)
	require.NoError(t, err)
	return res
}

func TestEvaluate_AGoodTakePassesWithItsMetricsAndPreserveList(t *testing.T) {
	r := newRig(t, passAnswer, true, steady()...)
	res := r.evaluate(t, faceInput(t))

	assert.Equal(t, domain.JudgePass, res.Result)
	assert.InDelta(t, 0.93, res.OverallConfidence, 1e-9)
	assert.Empty(t, res.Issues)
	assert.Equal(t, []string{"character", "acting"}, res.Preserve)
	assert.Equal(t, domain.FaceMeasured, res.Metrics.FaceIdentityStatus)
	require.Len(t, res.Metrics.FaceIdentity, 1)
	m := res.Metrics.FaceIdentity[0]
	assert.Equal(t, "char-a", m.CharacterID)
	assert.Equal(t, 10, m.SampledFrames)
	assert.True(t, m.Evaluated)
	assert.InDelta(t, 0.95, m.SimilarityMean, 0.02)
	assert.Nil(t, res.RepairStrategy)
	assert.Equal(t, llm.AgentVideoJudge, r.llm.agents[0], "the qualitative evaluation is the Video Judge's")
}

func TestEvaluate_FaceDriftBelowTheFailThresholdFailsTheTakeAndPrefersReferenceReinforcement(t *testing.T) {
	// The model likes the Take (pass, 0.99) but the numbers say the face drifted for 2 seconds.
	r := newRig(t, `{"result":"pass","overall_confidence":0.99,"issues":[],"repair_strategy":{"action":"prompt_repair","instructions":["keep_camera"]}}`, true, drifting()...)
	res := r.evaluate(t, faceInput(t))

	assert.Equal(t, domain.JudgeFail, res.Result)
	require.Len(t, res.Issues, 1)
	is := res.Issues[0]
	assert.Equal(t, domain.IssueTypeFaceIdentityDrift, is.Type)
	assert.Equal(t, domain.SeverityHigh, is.Severity)
	assert.Equal(t, "2.0-4.0s", is.TimeRange)
	assert.Equal(t, []int{48, 96}, is.Frames, "source frames at 24 fps")
	assert.False(t, is.LockViolation, "identity is not LOCKed here")
	require.NotNil(t, res.RepairStrategy)
	assert.Equal(t, domain.RepairReferenceReinforce, res.RepairStrategy.Action, "prompt_repair is replaced: a face drift is fixed by the reference")
	assert.Equal(t, []string{"keep_camera", domain.InstructionIncreaseFaceReference}, res.RepairStrategy.Instructions)
}

func TestEvaluate_ALockedIdentityMakesAFailThresholdBreachALockViolation(t *testing.T) {
	r := newRig(t, passAnswer, true, drifting()...)
	in := faceInput(t)
	in.Locked = map[domain.JudgeDimension]bool{domain.DimCharacterIdentity: true}
	res := r.evaluate(t, in)

	assert.Equal(t, domain.JudgeFail, res.Result, "whatever the model's score")
	require.Len(t, res.Issues, 1)
	assert.True(t, res.Issues[0].LockViolation)
}

func TestEvaluate_AWarningOnlyDoesNotFailTheTake(t *testing.T) {
	// 0.7 is below the calibrated warning threshold (0.85) and above the FAIL threshold (0.65).
	r := newRig(t, passAnswer, true, 0.95, 0.95, 0.95, 0.7, 0.7, 0.7, 0.95, 0.95, 0.95, 0.95)
	res := r.evaluate(t, faceInput(t))

	assert.Equal(t, domain.JudgePass, res.Result)
	require.Len(t, res.Issues, 1)
	assert.Equal(t, domain.SeverityMedium, res.Issues[0].Severity)
	assert.Nil(t, res.RepairStrategy)
}

func TestEvaluate_AWarningOnALockedIdentityStillPasses(t *testing.T) {
	// LOCK ties the FAIL threshold (§12.4); a warning is not a violation.
	r := newRig(t, passAnswer, true, 0.95, 0.95, 0.95, 0.7, 0.7, 0.7, 0.95, 0.95, 0.95, 0.95)
	in := faceInput(t)
	in.Locked = map[domain.JudgeDimension]bool{domain.DimCharacterIdentity: true}
	res := r.evaluate(t, in)

	assert.Equal(t, domain.JudgePass, res.Result)
	assert.False(t, res.Issues[0].LockViolation)
}

func TestEvaluate_FramesWithoutAFaceNeverFailTheTake(t *testing.T) {
	// Half of the frames show no face (ratio 0.5 is still within the limit) and the rest are fine.
	r := newRig(t, passAnswer, true, 0.95, -1, -1, 0.95, -1, 0.95, -1, 0.95, 0.95, -1)
	in := faceInput(t)
	in.Locked = map[domain.JudgeDimension]bool{domain.DimCharacterIdentity: true}
	res := r.evaluate(t, in)

	assert.Equal(t, domain.JudgePass, res.Result)
	assert.Empty(t, res.Issues)
	assert.InDelta(t, 0.5, res.Metrics.FaceIdentity[0].UndetectedRatio, 1e-9)
}

func TestEvaluate_WhenTheNumbersDoNotHoldTheTakeIsJudgedByTheModelAlone(t *testing.T) {
	// Only 3 of 10 frames show the face, and those are low: not evaluated, no Issue, no failure.
	r := newRig(t, `{"result":"fail","overall_confidence":0.8,"issues":[{"type":"acting_stiff","severity":"medium","dimension":"acting"}]}`,
		true, 0.2, 0.2, 0.2, -1, -1, -1, -1, -1, -1, -1)
	in := faceInput(t)
	in.Locked = map[domain.JudgeDimension]bool{domain.DimCharacterIdentity: true}
	res := r.evaluate(t, in)

	assert.False(t, res.Metrics.FaceIdentity[0].Evaluated)
	for _, is := range res.Issues {
		assert.NotEqual(t, domain.IssueTypeFaceIdentityDrift, is.Type)
	}
	assert.Equal(t, domain.JudgeFail, res.Result, "the model's own verdict decides")

	r = newRig(t, passAnswer, true, 0.2, 0.2, 0.2, -1, -1, -1, -1, -1, -1, -1)
	assert.Equal(t, domain.JudgePass, r.evaluate(t, in).Result, "and a pass stays a pass")
}

func TestEvaluate_WithoutTheFaceModelsTheTakeIsJudgedQualitativelyAndSaysSo(t *testing.T) {
	r := newRig(t, passAnswer, false, drifting()...)
	res := r.evaluate(t, faceInput(t))

	assert.Equal(t, domain.JudgePass, res.Result)
	assert.Equal(t, domain.FaceUnavailable, res.Metrics.FaceIdentityStatus)
	assert.Empty(t, res.Metrics.FaceIdentity)
	assert.Len(t, r.frames.specs, 1, "only the review frames were sampled")
}

func TestEvaluate_AFailingFaceEngineFallsBackToTheQualitativeEvaluation(t *testing.T) {
	r := newRig(t, passAnswer, true, steady()...)
	r.engine.err = errors.New("onnx runtime crashed")
	res := r.evaluate(t, faceInput(t))

	assert.Equal(t, domain.JudgePass, res.Result)
	assert.Equal(t, domain.FaceUnavailable, res.Metrics.FaceIdentityStatus)
}

func TestEvaluate_WithoutACharacterFaceTheMeasurementIsNotApplicable(t *testing.T) {
	r := newRig(t, passAnswer, true, steady()...)
	in := faceInput(t)
	in.FaceSubjects = nil
	res := r.evaluate(t, in)

	assert.Equal(t, domain.FaceNotApplicable, res.Metrics.FaceIdentityStatus)
	assert.Zero(t, r.engine.calls, "no picture was analysed")
}

func TestEvaluate_ALockedDimensionIssueOfMediumSeverityFailsTheTake(t *testing.T) {
	r := newRig(t, `{"result":"pass","overall_confidence":0.95,"issues":[
		{"type":"Product Logo-Deformation","severity":"medium","dimension":"logo_text_accuracy","time_range":"2.1-2.8s"},
		{"type":"camera_speed","severity":"medium","dimension":"camera_motion","time_range":"0.8-1.7s"}]}`, true, steady()...)
	in := faceInput(t)
	in.Locked = map[domain.JudgeDimension]bool{domain.DimLogoTextAccuracy: true}
	res := r.evaluate(t, in)

	assert.Equal(t, domain.JudgeFail, res.Result, "a LOCK violation fails whatever the model says")
	require.Len(t, res.Issues, 2)
	logo, camera := res.Issues[0], res.Issues[1]
	assert.Equal(t, "product_logo_deformation", logo.Type, "types are snake_case identifiers")
	assert.True(t, logo.LockViolation)
	assert.Equal(t, "2.1-2.8s", logo.TimeRange)
	assert.Equal(t, []int{50, 67}, logo.Frames, "2.1 s and 2.8 s at 24 fps")
	assert.False(t, camera.LockViolation, "camera motion is not LOCKed")
}

func TestEvaluate_AHighSeverityIssueFailsAPassingAnswer(t *testing.T) {
	r := newRig(t, `{"result":"pass","overall_confidence":0.9,"issues":[{"type":"hand_deformed","severity":"HIGH","dimension":"hands"}]}`, true, steady()...)
	res := r.evaluate(t, faceInput(t))

	assert.Equal(t, domain.JudgeFail, res.Result)
	assert.Equal(t, domain.SeverityHigh, res.Issues[0].Severity)
}

func TestEvaluate_TimeRangesAreCheckedAgainstTheClip(t *testing.T) {
	r := newRig(t, `{"result":"pass","overall_confidence":0.9,"issues":[
		{"type":"a","severity":"low","time_range":"4.5-9.0s"},
		{"type":"b","severity":"low","time_range":"7-8s"},
		{"type":"c","severity":"low","time_range":"soon"},
		{"type":"d","severity":"low","dimension":"not_a_dimension"}]}`, true, steady()...)
	res := r.evaluate(t, faceInput(t))

	require.Len(t, res.Issues, 4)
	assert.Equal(t, "4.5-5.0s", res.Issues[0].TimeRange, "clamped to the 5 s Take")
	assert.Equal(t, []int{108, 119}, res.Issues[0].Frames, "and the last frame of the Take at most")
	assert.Empty(t, res.Issues[1].TimeRange, "a range after the end of the Take is dropped")
	assert.Empty(t, res.Issues[1].Frames)
	assert.Empty(t, res.Issues[2].TimeRange, "an unreadable range is dropped, the Issue stays")
	assert.Empty(t, res.Issues[3].Dimension, "an unknown dimension is dropped")
}

func TestEvaluate_TheModelsOwnFaceDriftIsDroppedWhenTheNumbersMeasuredIt(t *testing.T) {
	r := newRig(t, `{"result":"fail","overall_confidence":0.8,"issues":[{"type":"face_identity_drift","severity":"high","time_range":"0.0-1.0s"}]}`, true, steady()...)
	res := r.evaluate(t, faceInput(t))

	assert.Empty(t, res.Issues, "the measurement says the face is steady")
	assert.Equal(t, domain.JudgeFail, res.Result, "the model's verdict still counts")
}

func TestEvaluate_TheModelsFaceDriftStaysWhenThereAreNoNumbers(t *testing.T) {
	r := newRig(t, `{"result":"fail","overall_confidence":0.8,"issues":[{"type":"face_identity_drift","severity":"high","time_range":"0.0-1.0s"}]}`, false, steady()...)
	res := r.evaluate(t, faceInput(t))

	require.Len(t, res.Issues, 1)
	require.NotNil(t, res.RepairStrategy)
	assert.Equal(t, domain.RepairReferenceReinforce, res.RepairStrategy.Action)
}

func TestEvaluate_UnusableAnswersAreInvalidJudgements(t *testing.T) {
	for name, answer := range map[string]string{
		"result missing":       `{"overall_confidence":0.9}`,
		"result unknown":       `{"result":"maybe","overall_confidence":0.9}`,
		"confidence missing":   `{"result":"pass"}`,
		"confidence too large": `{"result":"pass","overall_confidence":94}`,
		"issue without type":   `{"result":"pass","overall_confidence":0.9,"issues":[{"severity":"low"}]}`,
		"unknown severity":     `{"result":"pass","overall_confidence":0.9,"issues":[{"type":"x","severity":"critical"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, answer, true, steady()...)
			_, err := r.judge.Evaluate(context.Background(), faceInput(t))
			require.ErrorIs(t, err, judge.ErrInvalidJudgement)
			assert.ErrorIs(t, err, planning.ErrInvalidPlan, "reported to the user as an unusable AI answer")
		})
	}
}

func TestEvaluate_ANonJSONAnswerIsAnInvalidOutput(t *testing.T) {
	r := newRig(t, "I think it looks fine.", true, steady()...)
	_, err := r.judge.Evaluate(context.Background(), faceInput(t))
	assert.ErrorIs(t, err, llm.ErrInvalidOutput)
}

func TestEvaluate_ProviderAndFrameErrorsAreReturned(t *testing.T) {
	r := newRig(t, passAnswer, true, steady()...)
	r.llm.err = llm.ErrNoAPIKey
	_, err := r.judge.Evaluate(context.Background(), faceInput(t))
	assert.ErrorIs(t, err, llm.ErrNoAPIKey)

	r = newRig(t, passAnswer, true, steady()...)
	r.frames.err = judge.ErrNoFFmpeg
	_, err = r.judge.Evaluate(context.Background(), faceInput(t))
	assert.ErrorIs(t, err, judge.ErrNoFFmpeg)
	assert.Equal(t, "ffmpeg / ffprobe が見つかりません（インストールして PATH に追加してください）", judge.FailureReason(err))
}

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
