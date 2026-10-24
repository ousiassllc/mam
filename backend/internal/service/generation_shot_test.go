package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

var japanese = regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}]`)

func TestGenerateShot_ReturnsAcceptedGeneratingShotAndMakesTakesWithCanonicalIdentifiers(t *testing.T) {
	f := newGenFixture(t, true)
	f.models.block = make(chan struct{})

	frag, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")
	require.NoError(t, err)
	assert.Equal(t, domain.ShotGenerating, frag.State, "the answer shows the Shot generating; the Job is held, so it cannot have ended")
	close(f.models.block)

	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)

	// F-06: the Canonical Shot Spec is in shot.shot_spec and, as a snapshot, in prompt_version.
	shot := f.shot(t).Shot
	spec, ok := shot.CanonicalSpec()
	require.True(t, ok)
	assert.Equal(t, "Character_A", spec.Character.ID)
	assert.Equal(t, "4.0s", spec.Duration)
	var snapshot, stored string
	require.NoError(t, f.query(t, `SELECT shot_spec_snapshot FROM prompt_version WHERE shot_id = ?`, f.shotID).Scan(&snapshot))
	require.NoError(t, f.query(t, `SELECT shot_spec FROM shot WHERE id = ?`, f.shotID).Scan(&stored))
	assert.JSONEq(t, stored, snapshot)

	// F-07 / F-12: the balanced policy makes 3 Takes with Seedance 2.0, stored with canonical identifiers.
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take`))
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take WHERE model_name = 'seedance-2.0' AND status = 'candidate'`))
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = 'seedance-2.0' AND shot_id = ? AND estimated_cost > 0`, f.shotID))
	takes := f.shot(t).Takes
	require.Len(t, takes, 3)
	for _, tk := range takes {
		require.NotEmpty(t, tk.FilePath)
		_, err := os.Stat(filepath.Join(f.assetsDir, filepath.FromSlash(tk.FilePath)))
		assert.NoError(t, err, "the video file of a Take exists")
	}

	// The prompt sent to the model is English, and the record keeps what each model would get.
	for _, req := range f.models.videoRequests() {
		assert.NotRegexp(t, japanese, req.Prompt)
		assert.Contains(t, req.Prompt, "starts running")
	}
	var record string
	require.NoError(t, f.query(t, `SELECT model_adapter_output FROM prompt_version WHERE shot_id = ?`, f.shotID).Scan(&record))
	assert.Contains(t, record, `"seedance-2.0"`)
	assert.Contains(t, record, `"kling-3.0"`, "the alternative model's request is recorded too")

	// The chat shows a progress card and a card per Take.
	kinds := map[domain.MessageKind]int{}
	for _, m := range f.thread(t, f.projectID) {
		kinds[m.Kind]++
	}
	assert.Equal(t, 1, kinds[domain.KindProgress])
	assert.Equal(t, 3, kinds[domain.KindTake])
}

func TestGenerateShot_KeepsTheSpecUnlessThereIsANewInstruction(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()

	_, err := f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	require.NoError(t, err)
	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)
	assert.Equal(t, 1, f.llm.calls(llm.AgentPromptPlanner))

	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return f.count(t, `SELECT COUNT(*) FROM take`) == 6 }, 30*time.Second, 10*time.Millisecond)
	f.waitShotState(t, domain.ShotPlanning)
	assert.Equal(t, 1, f.llm.calls(llm.AgentPromptPlanner), "a retake reuses the Canonical Shot Spec")
	assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM prompt_version`), "every generation is a prompt version")

	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "もう少しカメラを寄せて")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return f.count(t, `SELECT COUNT(*) FROM take`) == 9 }, 30*time.Second, 10*time.Millisecond)
	assert.Equal(t, 2, f.llm.calls(llm.AgentPromptPlanner), "an instruction makes the Prompt Planner revise the spec")
	assert.Contains(t, f.llm.prompts(llm.AgentPromptPlanner)[1], "もう少しカメラを寄せて")
}

func TestGenerateShot_RejectsASecondRunWhileTheShotIsBeingGenerated(t *testing.T) {
	f := newGenFixture(t, true)
	f.models.block = make(chan struct{})
	ctx := context.Background()

	_, err := f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	require.NoError(t, err)
	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	assert.ErrorIs(t, err, service.ErrBusy)
	_, err = f.svc.Generation.BuildReferencePack(ctx, f.shotID)
	assert.ErrorIs(t, err, service.ErrBusy, "a Reference Pack is not built while the Shot is generating either")

	close(f.models.block)
	f.waitJobsDone(t)
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take`), "only one generation ran")
}

func TestGenerateShot_BudgetCeilingLimitsTakesOrRefuses(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()
	// Balanced: Seedance 2.0 at 720p costs 0.08 USD/s; the Shot's clip is 4 s, so one Take costs 0.32.
	tiny, oneTake := 0.01, 0.35

	f.setBudget(t, nil, &tiny)
	_, err := f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	assert.ErrorIs(t, err, service.ErrBudgetExceeded, "not even one Take fits")
	assert.Equal(t, domain.ShotPlanning, f.shot(t).State, "nothing was started")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM job`))

	f.setBudget(t, nil, &oneTake)
	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	require.NoError(t, err)
	f.waitJobsDone(t)
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM take`), "the number of Takes is reduced to what the budget allows")
	var record string
	require.NoError(t, f.query(t, `SELECT model_adapter_output FROM prompt_version WHERE shot_id = ?`, f.shotID).Scan(&record))
	assert.Contains(t, record, "budget allows 1 of 3 Takes")

	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	assert.ErrorIs(t, err, service.ErrBudgetExceeded, "the first generation used up the Shot's budget")
}

func TestGenerateShot_FailureIsReportedInTheChatAndTheShotIsReleased(t *testing.T) {
	f := newGenFixture(t, true)
	f.models.err = &modeladapter.ProviderError{Vendor: "seedance", Status: http.StatusForbidden, Message: "policy"}

	_, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")
	require.NoError(t, err)
	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)

	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take`))
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry`), "a failed call is not charged")
	last := lastMessage(t, f.thread(t, f.projectID))
	assert.Equal(t, domain.KindText, last.Kind)
	assert.Contains(t, last.Content, "生成に失敗しました")
	assert.Contains(t, last.Content, "拒否")

	f.models.err = nil
	_, err = f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")
	assert.NoError(t, err, "the user can start again")
}

func TestBuildReferencePack_MakesEveryReferenceAndTheKeyframeThenGenerationUsesThem(t *testing.T) {
	f := newGenFixture(t, true)
	f.models.block = make(chan struct{})
	ctx := context.Background()

	frag, err := f.svc.Generation.BuildReferencePack(ctx, f.shotID)
	require.NoError(t, err)
	assert.Equal(t, domain.ShotGenerating, frag.State, "the Job is held: the answer cannot be read after it ended")
	close(f.models.block)
	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)

	refs := f.shot(t).References()
	var kinds []domain.ReferenceKind
	for _, r := range refs {
		kinds = append(kinds, r.Kind)
		assert.NotEmpty(t, r.AssetID)
	}
	assert.Equal(t, domain.ReferenceKinds(), kinds, "Character / Product / Location / Style / Lighting References and the Keyframe")
	assert.Equal(t, 6, f.count(t, `SELECT COUNT(*) FROM asset WHERE type = 'reference'`))

	// model-adapters.md §3: new Character / Product / Location -> Imagen 4; Style / Lighting / Keyframe -> Nano Banana.
	byKind := map[domain.ReferenceKind]domain.ModelName{}
	for _, r := range refs {
		byKind[r.Kind] = r.Model
	}
	assert.Equal(t, domain.ModelImagen4, byKind[domain.RefCharacter])
	assert.Equal(t, domain.ModelImagen4, byKind[domain.RefProduct])
	assert.Equal(t, domain.ModelImagen4, byKind[domain.RefLocation])
	assert.Equal(t, domain.ModelNanoBanana, byKind[domain.RefStyle])
	assert.Equal(t, domain.ModelNanoBanana, byKind[domain.RefLighting])
	assert.Equal(t, domain.ModelNanoBanana, byKind[domain.RefKeyframe])
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = 'imagen-4'`))
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = 'nano-banana'`))

	keyframes := 0
	for _, m := range f.thread(t, f.projectID) {
		if m.Kind == domain.KindKeyframe {
			keyframes++
		}
	}
	assert.Equal(t, 6, keyframes, "every image is shown in the chat")

	// The Keyframe starts the clip and the other references go along.
	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	require.NoError(t, err)
	f.waitShotState(t, domain.ShotPlanning)
	require.Eventually(t, func() bool { return len(f.models.videoRequests()) == 3 }, 30*time.Second, 10*time.Millisecond)
	for _, req := range f.models.videoRequests() {
		require.NotNil(t, req.FirstFrame, "the Keyframe is the first frame")
		assert.Equal(t, pngBytes, req.FirstFrame.Data)
		assert.Empty(t, req.References, "frames and reference images are exclusive")
	}
	var stored string
	require.NoError(t, f.query(t, `SELECT shot_spec FROM shot WHERE id = ?`, f.shotID).Scan(&stored))
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(stored), &doc))
	assert.Contains(t, doc, "references", "the Canonical Shot Spec is written next to the Reference Pack")
	assert.Contains(t, doc, "shot_objective")
}

func TestBuildReferencePack_FailureKeepsTheOldPackAndLeavesNoFiles(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()

	_, err := f.svc.Generation.BuildReferencePack(ctx, f.shotID)
	require.NoError(t, err)
	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)
	before := f.shot(t).References()
	files := countFiles(t, f.assetsDir)

	f.models.err = &modeladapter.ProviderError{Vendor: "google", Status: http.StatusForbidden}
	_, err = f.svc.Generation.BuildReferencePack(ctx, f.shotID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return f.shot(t).State == domain.ShotPlanning && f.count(t, `SELECT COUNT(*) FROM job WHERE status = 'failed'`) == 1
	}, 30*time.Second, 10*time.Millisecond)

	assert.Equal(t, before, f.shot(t).References())
	assert.Equal(t, files, countFiles(t, f.assetsDir))
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	require.NoError(t, filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	}))
	return n
}
