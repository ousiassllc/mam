package service_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func setState(t *testing.T, f fixture, projectID string, state domain.ProjectState) {
	t.Helper()
	err := f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE project SET state = ? WHERE id = ?`, state, projectID)
		return err
	})
	require.NoError(t, err)
}

func TestRequestApproval_FollowsTheConfirmationLevel(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")

	posted, err := f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointScript, "脚本 v1（3 Scene / 15秒）", "sv1")
	require.NoError(t, err)
	assert.True(t, posted, "KEY CHECK asks at the script")
	got, _ := f.svc.Projects.Get(ctx, p.ID)
	assert.Equal(t, domain.StateAwaitingApproval, got.State)
	card := lastMessage(t, f.thread(t, p.ID))
	assert.Equal(t, domain.KindApproval, card.Kind)
	assert.Equal(t, domain.ApprovalPending, card.ApprovalStatus)
	assert.Equal(t, "script_version", card.RefType)
	posted, err = f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointSceneComplete, "Scene 1", "sc1")
	require.NoError(t, err)
	assert.False(t, posted, "KEY CHECK does not ask at each Scene")
	posted, err = f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointMainVisual, "Main Visual", "mv1")
	require.NoError(t, err)
	assert.False(t, posted, "nothing in the pipeline produces a Main Visual to ask about, so KEY CHECK does not promise it")

	_, err = f.svc.Projects.SetConfirmationLevel(ctx, p.ID, domain.ConfirmFullAuto, nil)
	require.NoError(t, err)
	posted, err = f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointFinal, "Final", "e1")
	require.NoError(t, err)
	assert.False(t, posted, "FULL AUTO never asks for approval")

	_, err = f.svc.Projects.SetConfirmationLevel(ctx, p.ID, domain.ConfirmCustomCheck, []domain.Checkpoint{domain.CheckpointRoughCut})
	require.NoError(t, err)
	posted, err = f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointRoughCut, "Rough Cut", "e1")
	require.NoError(t, err)
	assert.True(t, posted, "CUSTOM CHECK asks at the selected checkpoint")
	posted, err = f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointScript, "脚本 v2", "sv2")
	require.NoError(t, err)
	assert.False(t, posted)
}

func TestRespondApproval_StaysAwaitingUntilAnsweredThenMovesOn(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	_, err := f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointScript, "脚本 v1", "sv1")
	require.NoError(t, err)
	card := lastMessage(t, f.thread(t, p.ID))
	project, updated, err := f.svc.Chat.RespondApproval(ctx, p.ID, card.ID, service.DecisionApprove, "いいですね")
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalApproved, updated.ApprovalStatus)
	assert.Equal(t, domain.StateVisualDesign, project.State, "script approved: on to visual design")

	thread := f.thread(t, p.ID)
	user := thread[len(thread)-2]
	assert.Equal(t, domain.RoleUser, user.Role)
	assert.Equal(t, card.ID, user.ReplyToMessageID, "the answer is recorded as a reply to the card")
	assert.Contains(t, user.Content, "いいですね")
	_, _, err = f.svc.Chat.RespondApproval(ctx, p.ID, card.ID, service.DecisionApprove, "")
	assert.ErrorIs(t, err, service.ErrConflict, "a card is answered exactly once")
}

func TestRespondApproval_RequestChangesSendsTheProjectBack(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	_, err := f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointScript, "脚本 v1", "sv1")
	require.NoError(t, err)
	card := lastMessage(t, f.thread(t, p.ID))

	project, updated, err := f.svc.Chat.RespondApproval(ctx, p.ID, card.ID, service.DecisionRequestChanges, "もっと短く")
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalChangesRequested, updated.ApprovalStatus)
	assert.Equal(t, domain.StatePlanning, project.State)
	assert.Contains(t, f.thread(t, p.ID)[len(f.thread(t, p.ID))-2].Content, "もっと短く")

	var n, override int
	require.NoError(t, f.db.Reader().QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(override), 0) FROM human_intervention WHERE project_id = ? AND kind = 'request_changes'`, p.ID).Scan(&n, &override))
	assert.Equal(t, 1, n, "only a checkpoint request for changes is a human intervention")
	assert.Equal(t, 1, override)
}

// The answer to a request for changes says what happens: only the script is redone from the comment.
// A Rough Cut or Final goes back to EDITING and nothing reads the comment, so the answer says so
// instead of promising that it is applied.
func TestRespondApproval_RequestChangesAnswerSaysWhatHappens(t *testing.T) {
	ctx := context.Background()
	for cp, want := range map[domain.Checkpoint]string{
		domain.CheckpointScript:   "脚本を作り直します",
		domain.CheckpointRoughCut: "自動では反映されない",
		domain.CheckpointFinal:    "自動では反映されない",
	} {
		t.Run(string(cp), func(t *testing.T) {
			f := newFixture(t)
			p := f.createProject(t, "動画")
			_, err := f.svc.Chat.RequestApproval(ctx, p.ID, cp, "確認", "x1")
			require.NoError(t, err)
			card := lastMessage(t, f.thread(t, p.ID))
			_, _, err = f.svc.Chat.RespondApproval(ctx, p.ID, card.ID, service.DecisionRequestChanges, "短く")
			require.NoError(t, err)

			answer := lastMessage(t, f.thread(t, p.ID)).Content
			assert.Contains(t, answer, want)
			if cp != domain.CheckpointScript {
				assert.NotContains(t, answer, "やり直します")
			}
		})
	}
}

func TestRespondApproval_ValidatesTheRequest(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	text := f.thread(t, p.ID)[0]
	_, _, err := f.svc.Chat.RespondApproval(ctx, p.ID, text.ID, "maybe", "")
	requireInvalid(t, err, "decision")
	_, _, err = f.svc.Chat.RespondApproval(ctx, p.ID, text.ID, service.DecisionApprove, "")
	requireInvalid(t, err, "message") // only approval cards can be approved

	_, _, err = f.svc.Chat.RespondApproval(ctx, p.ID, "missing", service.DecisionApprove, "")
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, _, err = f.svc.Chat.RespondApproval(ctx, "missing", text.ID, service.DecisionApprove, "")
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestPostMessage_ReplyingToApprovalCardInWordsAnswersIt(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	_, err := f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointScript, "脚本 v1", "sv1")
	require.NoError(t, err)
	card := lastMessage(t, f.thread(t, p.ID))

	postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "もっと短くしてください", ReplyToMessageID: card.ID})
	project, updated, err := f.svc.Chat.Card(ctx, p.ID, card.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalChangesRequested, updated.ApprovalStatus, "any answer that is not an approval is feedback")
	assert.Equal(t, domain.StatePlanning, project.State)

	// A second card, answered with an approval word.
	_, err = f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointRoughCut, "Rough Cut", "e1")
	require.NoError(t, err)
	card2 := lastMessage(t, f.thread(t, p.ID))
	postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "承認します。", ReplyToMessageID: card2.ID})
	project, updated2, err := f.svc.Chat.Card(ctx, p.ID, card2.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalApproved, updated2.ApprovalStatus)
	assert.Equal(t, domain.StateAudio, project.State)
}

func TestEscalation_AnyReplyResumesTheProject(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	setState(t, f, p.ID, domain.StateRepairing) // a Repair of a Shot raised it
	require.NoError(t, f.svc.Chat.RaiseEscalation(ctx, p.ID, "リトライ上限に達しました", []string{"続行", "中止"}, "shot1"))
	got, _ := f.svc.Projects.Get(ctx, p.ID)
	assert.Equal(t, domain.StateAwaitingApproval, got.State, "escalation holds the project even at FULL AUTO")
	esc := lastMessage(t, f.thread(t, p.ID))
	assert.True(t, esc.Open)
	postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "続行", ReplyToMessageID: esc.ID})
	got, _ = f.svc.Projects.Get(ctx, p.ID)
	assert.Equal(t, domain.EscalationResumeState, got.State)
	_, again, err := f.svc.Chat.Card(ctx, p.ID, esc.ID)
	require.NoError(t, err)
	assert.False(t, again.Open)
}

func TestAnnounce_PostsCardsAndRejectsBlockingKinds(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	require.NoError(t, f.svc.Chat.Announce(ctx, p.ID, domain.ChatMessage{Kind: domain.KindKeyframe, RefID: "asset1", Content: "Main Visual"}))
	require.NoError(t, f.svc.Chat.Announce(ctx, p.ID, domain.ChatMessage{Kind: domain.KindTake, RefID: "take1",
		Content: domain.EncodeTakeCardBody(domain.TakeCardBody{Title: "Take 2", Badge: "AI Pick"})}))
	msgs := f.thread(t, p.ID)
	assert.Equal(t, domain.RefAsset, msgs[len(msgs)-2].RefType)
	assert.Equal(t, domain.RefTake, msgs[len(msgs)-1].RefType)

	var invalid *service.ValidationError
	require.ErrorAs(t, f.svc.Chat.Announce(ctx, p.ID, domain.ChatMessage{Kind: domain.KindApproval}), &invalid)
	require.ErrorAs(t, f.svc.Chat.Announce(ctx, p.ID, domain.ChatMessage{Kind: domain.KindTake}), &invalid, "cards need a reference")
	got, _ := f.svc.Projects.Get(ctx, p.ID)
	assert.Equal(t, domain.StateDraft, got.State, "announcements never block the project")
}

func TestProgressCard_ShowsTheLiveJobAndNotifiesOnJobChange(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	jobs := repository.NewJobRepository(f.db)
	job, err := jobs.Create(ctx, domain.JobGenerate, nil)
	require.NoError(t, err)
	require.NoError(t, f.svc.Chat.Announce(ctx, p.ID, domain.ChatMessage{Kind: domain.KindProgress, RefID: job.ID, Content: "Scene 02 を生成中"}))
	card := lastMessage(t, f.thread(t, p.ID))
	require.NotNil(t, card.Job)
	assert.Equal(t, domain.JobQueued, card.Job.Status)
	events, cancel, err := f.svc.Chat.Subscribe(ctx, p.ID)
	require.NoError(t, err)
	defer cancel()
	require.NoError(t, jobs.MarkRunning(ctx, job.ID))
	f.svc.Chat.JobChanged(ctx, job.ID)

	select {
	case e := <-events:
		assert.Equal(t, service.Event{Type: service.EventCardUpdated, MessageID: card.ID}, e)
	case <-time.After(time.Second):
		t.Fatal("no event for the progress card")
	}
	_, live, err := f.svc.Chat.Card(ctx, p.ID, card.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobRunning, live.Job.Status)
}

func TestEvents_AreNotificationsOnlyAndReachOnlyTheirProject(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	other := f.createProject(t, "別")
	mine, cancel, err := f.svc.Chat.Subscribe(ctx, p.ID)
	require.NoError(t, err)
	defer cancel()
	theirs, cancelOther, err := f.svc.Chat.Subscribe(ctx, other.ID)
	require.NoError(t, err)
	defer cancelOther()

	ws := postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "Instagram広告"})

	select {
	case e := <-mine:
		assert.Equal(t, service.EventMessageCreated, e.Type)
		assert.Equal(t, ws.Messages[len(ws.Messages)-1].ID, e.MessageID, "points at the newest message; carries no body")
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	select {
	case e := <-theirs:
		t.Fatalf("event leaked into another project: %+v", e)
	default:
	}
	_, _, err = f.svc.Chat.Subscribe(ctx, "missing")
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestHub_SlowSubscriberKeepsTheNewestEvent(t *testing.T) {
	hub := service.NewHub()
	ch, cancel := hub.Subscribe("p")
	defer cancel()
	for i := 0; i < 100; i++ {
		hub.Publish("p", service.Event{Type: service.EventMessageCreated, MessageID: string(rune('a' + i%26))})
	}
	hub.Publish("p", service.Event{Type: service.EventCardUpdated, MessageID: "last"})

	var got []service.Event
	for len(ch) > 0 {
		got = append(got, <-ch)
	}
	require.NotEmpty(t, got)
	assert.Equal(t, "last", got[len(got)-1].MessageID)
	assert.LessOrEqual(t, len(got), 16, "the buffer is bounded")
	cancel()
	cancel()                          // idempotent
	hub.Publish("p", service.Event{}) // no subscribers: must not block or panic
}
