package service_test

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func TestSubscribeShot_SaysIdleWhenNothingRunsAndStreamsProgressUntilDone(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()

	ch, cancel, err := f.svc.Generation.SubscribeShot(ctx, f.shotID)
	require.NoError(t, err)
	assert.Equal(t, generation.StageIdle, (<-ch).Stage, "a client that connects to an idle Shot is told so at once")
	cancel()

	f.models.block = make(chan struct{})
	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	require.NoError(t, err)
	ch, cancel, err = f.svc.Generation.SubscribeShot(ctx, f.shotID)
	require.NoError(t, err)
	defer cancel()
	close(f.models.block)

	var stages []generation.Stage
	deadline := time.After(30 * time.Second)
	for last := generation.Stage(""); last != generation.StageDone; {
		select {
		case p := <-ch:
			assert.Equal(t, f.shotID, p.ShotID)
			stages = append(stages, p.Stage)
			last = p.Stage
		case <-deadline:
			t.Fatalf("no done message; got %v", stages)
		}
	}
	assert.Contains(t, stages, generation.StageGenerating)
}

func TestSubscribeShot_RepairingShotIsToldItIsRepairing(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()
	f.execSQL(t, `UPDATE shot SET state = 'REPAIRING' WHERE id = ?`, f.shotID)

	ch, cancel, err := f.svc.Generation.SubscribeShot(ctx, f.shotID)
	require.NoError(t, err)
	defer cancel()

	select {
	case p := <-ch:
		assert.Equal(t, generation.StageRepairing, p.Stage, "a Shot being repaired starts with its stage, not idle")
		assert.Equal(t, generation.ActionRepair, p.Action)
	case <-time.After(5 * time.Second):
		t.Fatal("no first message")
	}
}

func TestSubscribeShot_UnknownShotIsNotFound(t *testing.T) {
	f := newGenFixture(t, true)
	_, _, err := f.svc.Generation.SubscribeShot(context.Background(), "nope")
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = f.svc.Generation.GenerateShot(context.Background(), "nope", "")
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestGenerateShot_IsUnavailableWithoutAModelAdapter(t *testing.T) {
	f := newGenFixture(t, false)
	_, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")
	assert.ErrorIs(t, err, service.ErrUnavailable)
	_, err = f.svc.Generation.BuildReferencePack(context.Background(), f.shotID)
	assert.ErrorIs(t, err, service.ErrUnavailable)
}

func TestGenerateShot_RejectsAnOverlongInstruction(t *testing.T) {
	f := newGenFixture(t, true)
	long := make([]rune, 1001)
	for i := range long {
		long[i] = 'あ'
	}
	_, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, string(long))
	var v *service.ValidationError
	require.ErrorAs(t, err, &v)
	assert.Equal(t, "additional_instruction", v.Field)
}

func TestRecover_ReleasesShotsThatWereBeingGeneratedWhenTheAppStopped(t *testing.T) {
	f := newGenFixture(t, true)
	f.execSQL(t, `UPDATE shot SET state = 'GENERATING' WHERE id = ?`, f.shotID)

	n, err := f.svc.Recover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, domain.ShotPlanning, f.shot(t).State)
}

var characterIDRe = regexp.MustCompile(`character id="([^"]+)"`)

// extractAnswer makes the State Extractor say Mio is anxious at the hallway door and the camera is static.
func extractAnswer(user string) string {
	m := characterIDRe.FindStringSubmatch(user)
	if m == nil {
		return `{"changes":[{"target":"camera","field":"movement","value":"static"}]}`
	}
	return `{"changes":[
		{"target":"character","id":"` + m[1] + `","field":"emotion","value":"anxious"},
		{"target":"character","id":"` + m[1] + `","field":"position","value":"hallway_door"},
		{"target":"camera","field":"movement","value":"static"},
		{"target":"story","field":"previous_event","value":"phone_call_ended"}]}`
}

func (f genFixture) buildReferencePack(t *testing.T) {
	t.Helper()
	_, err := f.svc.Generation.BuildReferencePack(context.Background(), f.shotID)
	require.NoError(t, err)
	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)
}

func (f genFixture) generateAndWait(t *testing.T, shotID string, jobs int) {
	t.Helper()
	_, err := f.svc.Generation.GenerateShot(context.Background(), shotID, "")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return f.count(t, `SELECT COUNT(*) FROM job WHERE status = 'succeeded' AND kind = 'generate'`) == jobs
	}, 30*time.Second, 10*time.Millisecond)
}

func TestGenerateShot_StateExtractorAppendsWorldStateVersionAndTheNextShotStartsFromIt(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.extractor = extractAnswer
	f.buildReferencePack(t)

	f.generateAndWait(t, f.shotID, 2) // the Reference Pack job counts as the first

	// F-10: a State Diff and a new world_state_version were appended for the Shot.
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM world_state_version WHERE project_id = ?`, f.projectID))
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM state_diff WHERE shot_id = ? AND take_id IN (SELECT id FROM take)`, f.shotID))
	var diff string
	require.NoError(t, f.query(t, `SELECT diff FROM state_diff WHERE shot_id = ?`, f.shotID).Scan(&diff))
	assert.Contains(t, diff, `"field":"emotion"`)
	assert.Contains(t, diff, `"to":"anxious"`)
	world, err := f.svc.World.View(context.Background(), f.projectID, "")
	require.NoError(t, err)
	require.NotEmpty(t, world.World.Characters)
	assert.Equal(t, "anxious", world.World.Characters[0].Emotion)
	assert.Equal(t, "hallway_door", world.World.Characters[0].Position)
	assert.Equal(t, "static", world.World.Camera.Movement)
	assert.Equal(t, 1, world.World.Story.Scene, "the Shot is in the first Scene")
	assert.Equal(t, "phone_call_ended", world.World.Story.PreviousEvent)

	// F-09 / F-10: the next Shot's Prompt Planner input carries that state.
	shot2, err := f.svc.Plans.CreateShot(context.Background(), f.sceneID, service.ShotForm{Objective: "ドアを開ける", DurationSeconds: "4"})
	require.NoError(t, err)
	f.generateAndWait(t, shot2.Shots[1].ID, 3)
	planner := f.llm.prompts(llm.AgentPromptPlanner)
	require.Len(t, planner, 2)
	for _, want := range []string{`emotion="anxious"`, `position="hallway_door"`, `movement="static"`, "phone_call_ended"} {
		assert.Contains(t, planner[1], want)
	}
	assert.NotContains(t, planner[0], "anxious", "the first Shot had no earlier state")
	assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM world_state_version WHERE project_id = ?`, f.projectID), "versions are appended, never overwritten")
}

func TestGenerateShot_StateExtractorLeavesLockedFieldsAlone(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.extractor = extractAnswer
	f.buildReferencePack(t)
	var charID string
	require.NoError(t, f.query(t, `SELECT id FROM character WHERE project_id = ?`, f.projectID).Scan(&charID))
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode, value) VALUES ('l1', ?, 'character', ?, 'emotion', 'lock', 'null')`, f.projectID, charID)
		return err
	}))
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE character SET emotion = 'calm' WHERE id = ?`, charID)
		return err
	}))

	f.generateAndWait(t, f.shotID, 2)

	world, err := f.svc.World.View(context.Background(), f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, "calm", world.World.Characters[0].Emotion, "the LOCKed emotion is not updated")
	assert.Equal(t, "hallway_door", world.World.Characters[0].Position, "unlocked fields still follow the Shot")
	var diff string
	require.NoError(t, f.query(t, `SELECT diff FROM state_diff WHERE shot_id = ?`, f.shotID).Scan(&diff))
	assert.NotContains(t, diff, `"emotion"`, "LOCKed fields are excluded from the State Diff")
}

func TestGenerateShot_StateExtractorFailureDoesNotFailTheGeneration(t *testing.T) {
	f := newGenFixture(t, true) // the fake reasoner gives the State Extractor an unusable answer

	f.generateAndWait(t, f.shotID, 1)

	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take`), "the Takes are kept")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM world_state_version`), "the World State simply stays as it was")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM job WHERE status = 'failed'`))
}
