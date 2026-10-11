package service_test

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

type fixture struct {
	svc       *service.Services
	db        *repository.DB
	assetsDir string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	testenv.Parallel(t)
	db := testdb.Open(t)
	assets := filepath.Join(t.TempDir(), "assets")
	return fixture{svc: service.New(service.Deps{DB: db, AssetsDir: assets}), db: db, assetsDir: assets}
}

var (
	pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	mp4Bytes = append([]byte("\x00\x00\x00\x18ftypisom"), bytes.Repeat([]byte{0}, 32)...)
	movBytes = append([]byte("\x00\x00\x00\x14ftypqt  "), bytes.Repeat([]byte{0}, 32)...)
)

func upload(name string, data []byte) service.Upload {
	return service.Upload{Name: name, Size: int64(len(data)), Open: func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}}
}

func (f fixture) createProject(t *testing.T, content string, uploads ...service.Upload) domain.Project {
	t.Helper()
	p, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: content, Uploads: uploads})
	require.NoError(t, err)
	return p
}

func (f fixture) thread(t *testing.T, projectID string) []domain.ThreadMessage {
	t.Helper()
	ws, err := f.svc.Chat.Workspace(context.Background(), projectID)
	require.NoError(t, err)
	return ws.Messages
}

func (f fixture) brief(t *testing.T, projectID string) domain.Brief {
	t.Helper()
	view, err := f.svc.Projects.Brief(context.Background(), projectID)
	require.NoError(t, err)
	return view.Brief
}

func lastMessage(t *testing.T, msgs []domain.ThreadMessage) domain.ThreadMessage {
	t.Helper()
	require.NotEmpty(t, msgs)
	return msgs[len(msgs)-1]
}

func TestCreateProject_ExtractsBriefAndAsksForTheFirstMissingItem(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい。上品な雰囲気で")

	assert.Equal(t, domain.StateDraft, p.State)
	assert.Equal(t, domain.ModeAuto, p.Mode)
	assert.Equal(t, domain.ConfirmKeyCheck, p.ConfirmationLevel)
	assert.Equal(t, "美容液", p.Name)

	brief := f.brief(t, p.ID)
	assert.Equal(t, "美容液", brief.Subject)
	assert.Equal(t, "15秒", brief.Duration)
	assert.Equal(t, "9:16", brief.AspectRatio)

	msgs := f.thread(t, p.ID)
	require.Len(t, msgs, 2)
	assert.Equal(t, domain.RoleUser, msgs[0].Role)
	q := msgs[1]
	assert.Equal(t, domain.KindQuestion, q.Kind)
	assert.Equal(t, string(domain.BriefObjective), q.RefID, "Content Type and Subject were extracted; Objective is next")
	assert.True(t, q.Open)
	assert.NotEmpty(t, domain.DecodeCardBody(q.Content).Options)
}

func TestCreateProject_TakesAttachmentsIntoAssetsAndReferences(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "商品CMを作りたい", upload("product.png", pngBytes), upload("ref.mp4", mp4Bytes))

	brief := f.brief(t, p.ID)
	require.Len(t, brief.Assets, 1)
	require.Len(t, brief.References, 1)

	first := f.thread(t, p.ID)[0]
	require.Len(t, first.Attachments, 2)
	file, err := f.svc.Projects.Asset(context.Background(), p.ID, brief.Assets[0])
	require.NoError(t, err)
	assert.Equal(t, "image/png", file.Asset.MimeType)
	stored, err := os.ReadFile(file.Path)
	require.NoError(t, err)
	assert.Equal(t, pngBytes, stored)

	_, err = f.svc.Projects.Asset(context.Background(), "other-project", brief.Assets[0])
	assert.ErrorIs(t, err, service.ErrNotFound, "assets are only reachable through their own project")
}

func TestCreateProject_RejectsInvalidInputAndLeavesNothingBehind(t *testing.T) {
	huge := service.Upload{Name: "big.png", Size: service.MaxImageBytes + 1, Open: func() (io.ReadCloser, error) {
		t.Fatal("an oversized upload must be rejected before it is read")
		return nil, nil
	}}
	tooMany := make([]service.Upload, service.MaxAttachments+1)
	for i := range tooMany {
		tooMany[i] = upload("a.png", pngBytes)
	}
	tests := []struct {
		name    string
		content string
		uploads []service.Upload
		field   string
	}{
		{"empty", "  ", nil, "content"},
		{"too long", string(bytes.Repeat([]byte("あ"), service.MaxMessageRunes+1)), nil, "content"},
		{"unsupported extension", "hi", []service.Upload{upload("run.exe", pngBytes)}, "attachments"},
		{"content does not match the extension", "hi", []service.Upload{upload("fake.png", []byte("MZ\x90\x00 not an image"))}, "attachments"},
		{"webm claim with mp4 body", "hi", []service.Upload{upload("v.webm", mp4Bytes)}, "attachments"},
		{"quicktime body named mp4", "hi", []service.Upload{upload("v.mp4", movBytes)}, "attachments"},
		{"empty file", "hi", []service.Upload{upload("empty.png", nil)}, "attachments"},
		{"oversized image", "hi", []service.Upload{huge}, "attachments"},
		{"too many attachments", "hi", tooMany, "attachments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			_, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: tt.content, Uploads: tt.uploads})
			var invalid *service.ValidationError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, tt.field, invalid.Field)

			list, err := f.svc.Projects.List(context.Background())
			require.NoError(t, err)
			assert.Empty(t, list, "no project is created")
			entries, _ := os.ReadDir(f.assetsDir)
			assert.Empty(t, entries, "no file is left on disk")
		})
	}
}

func TestCreateProject_AcceptsEveryAllowedFormat(t *testing.T) {
	f := newFixture(t)
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0}, 16)...)
	gif := append([]byte("GIF89a"), bytes.Repeat([]byte{0}, 16)...)
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), bytes.Repeat([]byte{0}, 16)...)
	webm := append([]byte{0x1A, 0x45, 0xDF, 0xA3}, bytes.Repeat([]byte{0}, 16)...)
	p := f.createProject(t, "x動画", upload("a.JPG", jpeg), upload("b.gif", gif), upload("c.webp", webp),
		upload("d.webm", webm), upload("e.mov", movBytes), upload("f.mp4", mp4Bytes), upload("g.png", pngBytes))
	assert.Len(t, f.thread(t, p.ID)[0].Attachments, 7)
}

func TestPostMessage_AnswersTheOpenQuestionAndAsksTheNextOne(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい。上品な雰囲気で")
	q := lastMessage(t, f.thread(t, p.ID)) // Objective

	ws, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "Instagram広告", ReplyToMessageID: q.ID})
	require.NoError(t, err)
	require.Len(t, ws.Messages, 2, "the user's answer and the next question")
	assert.Equal(t, q.ID, ws.Messages[0].ReplyTo.ID)
	assert.Equal(t, "Instagram広告", f.brief(t, p.ID).Objective)
	assert.Equal(t, string(domain.BriefTarget), ws.Messages[1].RefID)

	thread := f.thread(t, p.ID)
	assert.False(t, thread[1].Open, "the answered question is closed")
	assert.True(t, thread[len(thread)-1].Open)
}

// A stale screen (or a double click) answers a card that was answered already: the text must not
// be written to the next open item of the Brief.
func TestPostMessage_ReplyToAnAnsweredQuestionIsAConflictAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい。上品な雰囲気で")
	q := lastMessage(t, f.thread(t, p.ID)) // Objective
	_, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "Instagram広告", ReplyToMessageID: q.ID})
	require.NoError(t, err)
	before := f.brief(t, p.ID)
	messages := len(f.thread(t, p.ID))

	_, err = f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "YouTube広告", ReplyToMessageID: q.ID})

	assert.ErrorIs(t, err, service.ErrConflict)
	assert.Equal(t, before, f.brief(t, p.ID), "the Brief is unchanged")
	assert.Len(t, f.thread(t, p.ID), messages, "nothing is added to the thread")
}

func TestPostMessage_FreeInputAnswersTheNewestOpenQuestionAndExtractsOtherItems(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画を作りたい") // asks Content Type first

	_, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "美容液のCMで、20〜30代女性向け"})
	require.NoError(t, err)

	b := f.brief(t, p.ID)
	assert.Equal(t, "商品CM", b.ContentType)
	assert.Equal(t, "美容液", b.Subject)
	assert.Equal(t, "20〜30代女性", b.Target)
}

func TestPostMessage_DelegatingLeavesTheItemToTheAIAndMovesOn(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画を作りたい")

	var last domain.ThreadMessage
	for range domain.RequiredBriefFields() {
		last = lastMessage(t, f.thread(t, p.ID))
		if last.Kind != domain.KindQuestion {
			break
		}
		_, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: domain.DelegateOption, ReplyToMessageID: last.ID})
		require.NoError(t, err)
	}

	brief := f.brief(t, p.ID)
	assert.Empty(t, brief.Missing())
	assert.Len(t, brief.Delegated, len(domain.RequiredBriefFields()))
	done := lastMessage(t, f.thread(t, p.ID))
	assert.Equal(t, domain.KindText, done.Kind)
	assert.Contains(t, done.Content, "Brief が揃いました")
}

func TestPostMessage_KeepsWorkingWhileTheProjectIsBusyAndDoesNotTouchTheBrief(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	require.NoError(t, f.svc.Chat.Announce(ctx, p.ID, domain.ChatMessage{Kind: domain.KindText, Content: "生成中"}))
	before := f.brief(t, p.ID)
	setState(t, f, p.ID, domain.StateGenerating)

	ws, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "10秒にしてほしい"})
	require.NoError(t, err)
	assert.Len(t, ws.Messages, 1, "input is accepted during generation; interpreting it is the DIRECTOR MODE feature")
	assert.Equal(t, before, f.brief(t, p.ID))
}

func TestPostMessage_RejectsUnknownReplyTargetsAndAttachments(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	other := f.createProject(t, "別の動画", upload("a.png", pngBytes))
	foreign := f.brief(t, other.ID).Assets[0]

	_, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "x", ReplyToMessageID: "missing"})
	var invalid *service.ValidationError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "reply_to_message_id", invalid.Field)

	_, err = f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "x", AttachmentAssetIDs: []string{foreign}})
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "attachment_asset_ids", invalid.Field, "assets of another project cannot be attached")

	_, err = f.svc.Chat.PostMessage(ctx, "missing", service.PostMessageInput{Content: "x"})
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestPostMessage_AttachmentsSentLaterJoinTheBrief(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画を作りたい")

	_, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "商品画像です", Uploads: []service.Upload{upload("a.png", pngBytes)}})
	require.NoError(t, err)
	assert.Len(t, f.brief(t, p.ID).Assets, 1)
}

func TestPostMessage_AttachmentsOfAnApprovalOrEscalationReplyJoinTheBrief(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")

	_, err := f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointScript, "脚本 v1", "sv1")
	require.NoError(t, err)
	card := lastMessage(t, f.thread(t, p.ID))
	_, err = f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "この雰囲気で", ReplyToMessageID: card.ID,
		Uploads: []service.Upload{upload("look.png", pngBytes), upload("ref.mp4", mp4Bytes)}})
	require.NoError(t, err)
	brief := f.brief(t, p.ID)
	assert.Len(t, brief.Assets, 1, "an image attached to an Approval reply is an Asset")
	assert.Len(t, brief.References, 1, "a video attached to an Approval reply is a Reference")

	require.NoError(t, f.svc.Chat.RaiseEscalation(ctx, p.ID, "リトライ上限に達しました", []string{"続行", "中止"}, "shot1"))
	esc := lastMessage(t, f.thread(t, p.ID))
	_, err = f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "続行", ReplyToMessageID: esc.ID,
		Uploads: []service.Upload{upload("other.png", pngBytes)}})
	require.NoError(t, err)
	assert.Len(t, f.brief(t, p.ID).Assets, 2, "an image attached to an Escalation reply is an Asset")
}

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

	var invalid *service.ValidationError
	_, _, err := f.svc.Chat.RespondApproval(ctx, p.ID, text.ID, "maybe", "")
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "decision", invalid.Field)

	_, _, err = f.svc.Chat.RespondApproval(ctx, p.ID, text.ID, service.DecisionApprove, "")
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "message", invalid.Field, "only approval cards can be approved")

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

	_, err = f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "もっと短くしてください", ReplyToMessageID: card.ID})
	require.NoError(t, err)
	project, updated, err := f.svc.Chat.Card(ctx, p.ID, card.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalChangesRequested, updated.ApprovalStatus, "any answer that is not an approval is feedback")
	assert.Equal(t, domain.StatePlanning, project.State)

	// A second card, answered with an approval word.
	_, err = f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointRoughCut, "Rough Cut", "e1")
	require.NoError(t, err)
	card2 := lastMessage(t, f.thread(t, p.ID))
	_, err = f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "承認します。", ReplyToMessageID: card2.ID})
	require.NoError(t, err)
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

	_, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "続行", ReplyToMessageID: esc.ID})
	require.NoError(t, err)
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

	ws, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "Instagram広告"})
	require.NoError(t, err)

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

	var invalid *service.ValidationError
	_, err = f.svc.Projects.UpdateBrief(ctx, p.ID, service.BriefForm{Values: map[domain.BriefField]string{domain.BriefAspectRatio: "縦"}})
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, string(domain.BriefAspectRatio), invalid.Field)
	_, err = f.svc.Projects.UpdateBrief(ctx, p.ID, service.BriefForm{References: "ftp://example.com"})
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "references", invalid.Field)
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
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画を作りたい") // asks Content Type
	q := lastMessage(t, f.thread(t, p.ID))

	// A reply closes the card it targets ...
	ws, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "商品CM", ReplyToMessageID: q.ID})
	require.NoError(t, err)
	require.Len(t, ws.Updated, 1)
	assert.Equal(t, q.ID, ws.Updated[0].ID)
	assert.False(t, ws.Updated[0].Open)

	// ... and a free-text answer closes the card of the field it decided, although nothing was replied to.
	open := lastMessage(t, ws.Messages)
	require.Equal(t, domain.KindQuestion, open.Kind)
	ws, err = f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "Instagram広告"})
	require.NoError(t, err)
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
	ws, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "返信", ReplyToMessageID: q.ID})
	require.NoError(t, err)
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

			_, err = f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "続行", ReplyToMessageID: esc.ID})
			require.NoError(t, err)
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

	_, err = f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "続行", ReplyToMessageID: esc.ID})
	require.NoError(t, err)
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

	_, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "続行", ReplyToMessageID: esc.ID})
	require.NoError(t, err)

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
