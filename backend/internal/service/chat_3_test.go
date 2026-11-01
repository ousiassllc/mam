package service_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func TestSetConfirmationLevel_ValidatesAndRemembersCustomSelection(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")

	var invalid *service.ValidationError
	_, err := f.svc.Projects.SetConfirmationLevel(ctx, p.ID, "sometimes", nil)
	require.ErrorAs(t, err, &invalid)
	_, err = f.svc.Projects.SetConfirmationLevel(ctx, p.ID, domain.ConfirmCustomCheck, []domain.Checkpoint{"bogus"})
	require.ErrorAs(t, err, &invalid)
	_, err = f.svc.Projects.SetConfirmationLevel(ctx, "missing", domain.ConfirmKeyCheck, nil)
	assert.ErrorIs(t, err, service.ErrNotFound)
	got, err := f.svc.Projects.SetConfirmationLevel(ctx, p.ID, domain.ConfirmCustomCheck, nil)
	require.NoError(t, err)
	assert.Equal(t, []domain.Checkpoint{domain.CheckpointScript, domain.CheckpointFinal}, got.CustomCheckpoints, "an empty selection falls back to script + final")

	got, err = f.svc.Projects.SetConfirmationLevel(ctx, p.ID, domain.ConfirmCustomCheck,
		[]domain.Checkpoint{domain.CheckpointRoughCut, domain.CheckpointScript})
	require.NoError(t, err)
	assert.Equal(t, []domain.Checkpoint{domain.CheckpointScript, domain.CheckpointRoughCut}, got.CustomCheckpoints, "stored in pipeline order")

	_, err = f.svc.Projects.SetConfirmationLevel(ctx, p.ID, domain.ConfirmCustomCheck, []domain.Checkpoint{domain.CheckpointCharacterDesign})
	require.ErrorAs(t, err, &invalid, "a checkpoint nothing in the pipeline asks at cannot be selected")
	got, err = f.svc.Projects.SetConfirmationLevel(ctx, p.ID, domain.ConfirmFullAuto, nil)
	require.NoError(t, err)
	assert.Equal(t, domain.ConfirmFullAuto, got.ConfirmationLevel)
	assert.Len(t, got.CustomCheckpoints, 2, "the custom selection is remembered")
	assert.Contains(t, lastMessage(t, f.thread(t, p.ID)).Content, "FULL AUTO", "the change is recorded in the thread")
}

func TestUpdateBrief_SavesEditsAndValidates(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい", upload("ref.mp4", mp4Bytes))

	view, err := f.svc.Projects.UpdateBrief(ctx, p.ID, service.BriefForm{
		Values: map[domain.BriefField]string{
			domain.BriefSubject: "化粧水", domain.BriefAspectRatio: "16:9", domain.BriefDuration: "",
		},
		AdditionalInstruction: " 夜のシーン ",
		References:            "https://example.com/a\n\nhttps://example.com/b\n",
	})
	require.NoError(t, err)
	assert.Equal(t, "化粧水", view.Brief.Subject)
	assert.Empty(t, view.Brief.Duration, "clearing a field makes the AI ask again")
	assert.Equal(t, "夜のシーン", view.Brief.AdditionalInstruction)
	assert.Len(t, view.Brief.References, 3, "the uploaded video reference is kept next to the two URLs")
	assert.Contains(t, lastMessage(t, f.thread(t, p.ID)).Content, "Brief を更新しました")
	_, err = f.svc.Projects.UpdateBrief(ctx, p.ID, service.BriefForm{Values: map[domain.BriefField]string{domain.BriefAspectRatio: "4:5"}}) // no Brief ratio: generation would make 16:9
	requireInvalid(t, err, string(domain.BriefAspectRatio))
	_, err = f.svc.Projects.UpdateBrief(ctx, p.ID, service.BriefForm{References: "ftp://example.com"})
	requireInvalid(t, err, "references")
	assert.Equal(t, "化粧水", f.brief(t, p.ID).Subject, "rejected edits are not stored")

	_, err = f.svc.Projects.UpdateBrief(ctx, "missing", service.BriefForm{})
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestWorkspace_UnknownProjectIsNotFound(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Chat.Workspace(context.Background(), "missing")
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = f.svc.Chat.Messages(context.Background(), "missing", "x")
	assert.ErrorIs(t, err, service.ErrNotFound)
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestCreateProject_StopsCopyingAnUploadThatIsLargerThanItClaims(t *testing.T) {
	f := newFixture(t)
	lying := service.Upload{Name: "lie.png", Size: 64, Open: func() (io.ReadCloser, error) {
		return io.NopCloser(io.MultiReader(bytes.NewReader(pngBytes), zeros{})), nil
	}}

	_, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "hi", Uploads: []service.Upload{lying}})
	var invalid *service.ValidationError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "サイズが上限")
	entries, _ := os.ReadDir(filepath.Join(f.assetsDir))
	for _, e := range entries {
		sub, _ := os.ReadDir(filepath.Join(f.assetsDir, e.Name()))
		assert.Empty(t, sub, "the oversized file was removed")
	}
}

func TestPostMessage_ReturnsTheQuestionCardsItClosed(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "動画を作りたい") // asks Content Type
	q := lastMessage(t, f.thread(t, p.ID))

	// A reply closes the card it targets ...
	ws := postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "商品CM", ReplyToMessageID: q.ID})
	require.Len(t, ws.Updated, 1)
	assert.Equal(t, q.ID, ws.Updated[0].ID)
	assert.False(t, ws.Updated[0].Open)

	// ... and a free-text answer closes the card of the field it decided, although nothing was replied to.
	open := lastMessage(t, ws.Messages)
	require.Equal(t, domain.KindQuestion, open.Kind)
	ws = postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "Instagram広告"})
	require.Len(t, ws.Updated, 1)
	assert.Equal(t, open.ID, ws.Updated[0].ID)
	assert.False(t, ws.Updated[0].Open)
}

func TestMessages_ManyProgressCardsAndRepliesOutsideTheBatchAreAllResolved(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	jobs := repository.NewJobRepository(f.db)
	running := map[string]bool{}
	for i := range 100 {
		job, err := jobs.Create(ctx, domain.JobGenerate, nil)
		require.NoError(t, err)
		if i%2 == 0 {
			require.NoError(t, jobs.MarkRunning(ctx, job.ID))
			running[job.ID] = true
		}
		require.NoError(t, f.svc.Chat.Announce(ctx, p.ID, domain.ChatMessage{Kind: domain.KindProgress, RefID: job.ID, Content: "生成中"}))
	}
	// a card whose job no longer exists still renders, without a job
	require.NoError(t, f.svc.Chat.Announce(ctx, p.ID, domain.ChatMessage{Kind: domain.KindProgress, RefID: "gone", Content: "生成中"}))

	progress := 0
	for _, m := range f.thread(t, p.ID) {
		if m.Kind != domain.KindProgress {
			continue
		}
		progress++
		if m.RefID == "gone" {
			assert.Nil(t, m.Job)
			continue
		}
		require.NotNil(t, m.Job, m.RefID)
		assert.Equal(t, m.RefID, m.Job.ID)
		want := domain.JobQueued
		if running[m.RefID] {
			want = domain.JobRunning
		}
		assert.Equal(t, want, m.Job.Status)
	}
	assert.Equal(t, 101, progress)

	// the reply target of a message after the cursor lies outside the returned batch
	thread := f.thread(t, p.ID)
	q := thread[0]
	ws := postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "返信", ReplyToMessageID: q.ID})
	var reply domain.ThreadMessage
	for _, m := range ws.Messages {
		if m.Content == "返信" {
			reply = m
		}
	}
	require.NotNil(t, reply.ReplyTo)
	assert.Equal(t, q.ID, reply.ReplyTo.ID)
}

func TestEscalation_KeepsTheStateOfAProjectThatCannotWaitForApproval(t *testing.T) {
	for _, state := range []domain.ProjectState{domain.StateAudio, domain.StateReady, domain.StateExported} {
		t.Run(string(state), func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t)
			p := f.createProject(t, "動画")
			setState(t, f, p.ID, state)
			require.NoError(t, f.svc.Chat.RaiseEscalation(ctx, p.ID, "リトライ上限に達しました", []string{"続行"}, "shot1"))
			got, err := f.svc.Projects.Get(ctx, p.ID)
			require.NoError(t, err)
			assert.Equal(t, state, got.State, "the state machine has no move to AWAITING_APPROVAL from here")
			esc := lastMessage(t, f.thread(t, p.ID))
			assert.Equal(t, domain.KindEscalation, esc.Kind, "the card is still posted")
			postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "続行", ReplyToMessageID: esc.ID})
			got, _ = f.svc.Projects.Get(ctx, p.ID)
			assert.Equal(t, state, got.State, "the reply does not push the Project into REPAIRING")
		})
	}
}

// An Escalation posted without holding the Project is only a card: it does not keep the Final approval
// from taking the Project to READY, and answering it later does not send the approved Project to REPAIRING.
func TestEscalation_PostedWithoutHoldingTheProjectNeitherBlocksTheFinalApprovalNorResumesIt(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	setState(t, f, p.ID, domain.StateAudio)
	require.NoError(t, f.svc.Chat.RaiseEscalation(ctx, p.ID, "リトライ上限に達しました", []string{"続行"}, "shot1"))
	esc := lastMessage(t, f.thread(t, p.ID))
	setState(t, f, p.ID, domain.StateFinalQC)
	posted, err := f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointFinal, "Final", "e1")
	require.NoError(t, err)
	require.True(t, posted)
	card := lastMessage(t, f.thread(t, p.ID))

	project, _, err := f.svc.Chat.RespondApproval(ctx, p.ID, card.ID, service.DecisionApprove, "")
	require.NoError(t, err)
	assert.Equal(t, domain.StateReady, project.State, "the unanswered Shot Escalation does not hold the Project")

	postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "続行", ReplyToMessageID: esc.ID})
	got, _ := f.svc.Projects.Get(ctx, p.ID)
	assert.Equal(t, domain.StateReady, got.State, "answering it does not undo the approval")
}

// An Escalation that held the Project (it was in AWAITING_APPROVAL) still blocks until answered, and the
// answer moves it to REPAIRING.
func TestEscalation_HoldingTheProjectStillResumesToRepairingWhenAnswered(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	setState(t, f, p.ID, domain.StateEditing)
	require.NoError(t, f.svc.Chat.RaiseEscalation(ctx, p.ID, "リトライ上限に達しました", []string{"続行"}, "shot1"))
	got, _ := f.svc.Projects.Get(ctx, p.ID)
	require.Equal(t, domain.StateAwaitingApproval, got.State)
	esc := lastMessage(t, f.thread(t, p.ID))
	postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "続行", ReplyToMessageID: esc.ID})

	got, _ = f.svc.Projects.Get(ctx, p.ID)
	assert.Equal(t, domain.StateRepairing, got.State)
}

// A Take card is posted before its Judge result exists: when the result is saved, the card in the
// live thread is told to redraw (its player draws the Issues as markers); other cards stay as they are.
func TestChatService_TakeJudgedRefreshesOnlyTheCardsOfThatTake(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	for _, takeID := range []string{"take1", "take2"} {
		require.NoError(t, f.svc.Chat.Announce(ctx, p.ID, domain.ChatMessage{Kind: domain.KindTake, RefID: takeID,
			Content: domain.EncodeTakeCardBody(domain.TakeCardBody{Title: takeID})}))
	}
	var take1 domain.ThreadMessage
	for _, m := range f.thread(t, p.ID) {
		if m.RefID == "take1" {
			take1 = m
		}
	}
	require.NotEmpty(t, take1.ID)
	events, cancel, err := f.svc.Chat.Subscribe(ctx, p.ID)
	require.NoError(t, err)
	defer cancel()
	f.svc.Chat.TakeJudged(ctx, "take1")

	select {
	case e := <-events:
		assert.Equal(t, service.Event{Type: service.EventCardUpdated, MessageID: take1.ID}, e)
	case <-time.After(time.Second):
		t.Fatal("no card.updated for the Take card")
	}
	select {
	case e := <-events:
		t.Fatalf("unexpected second event %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}
