package service_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
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
	db, assets := testdb.Open(t), filepath.Join(t.TempDir(), "assets")
	return fixture{svc: servicetest.New(t, service.Deps{DB: db, AssetsDir: assets}), db: db, assetsDir: assets}
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

// thread takes a require.TestingT so that an Eventually condition can pass its *assert.CollectT: a failure then retries instead of failing the finished test.
func (f fixture) thread(t require.TestingT, projectID string) []domain.ThreadMessage {
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

func requireInvalid(t *testing.T, err error, field string) {
	t.Helper()
	var invalid *service.ValidationError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, field, invalid.Field)
}

func lastMessage(t require.TestingT, msgs []domain.ThreadMessage) domain.ThreadMessage {
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
		{"too long", string(bytes.Repeat([]byte("あ"), domain.MaxMessageRunes+1)), nil, "content"},
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
			requireInvalid(t, err, tt.field)

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
	f := newFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい。上品な雰囲気で")
	q := lastMessage(t, f.thread(t, p.ID)) // Objective

	ws := postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "Instagram広告", ReplyToMessageID: q.ID})
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
	postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "Instagram広告", ReplyToMessageID: q.ID})
	before := f.brief(t, p.ID)
	messages := len(f.thread(t, p.ID))

	_, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "YouTube広告", ReplyToMessageID: q.ID})
	assert.ErrorIs(t, err, service.ErrConflict)
	assert.Equal(t, before, f.brief(t, p.ID), "the Brief is unchanged")
	assert.Len(t, f.thread(t, p.ID), messages, "nothing is added to the thread")
}

func TestPostMessage_FreeInputAnswersTheNewestOpenQuestionAndExtractsOtherItems(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "動画を作りたい") // asks Content Type first
	postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "美容液のCMで、20〜30代女性向け"})

	b := f.brief(t, p.ID)
	assert.Equal(t, "商品CM", b.ContentType)
	assert.Equal(t, "美容液", b.Subject)
	assert.Equal(t, "20〜30代女性", b.Target)
}

func TestPostMessage_DelegatingLeavesTheItemToTheAIAndMovesOn(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "動画を作りたい")

	var last domain.ThreadMessage
	for range domain.RequiredBriefFields() {
		last = lastMessage(t, f.thread(t, p.ID))
		if last.Kind != domain.KindQuestion {
			break
		}
		postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: domain.DelegateOption, ReplyToMessageID: last.ID})
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
	ws := postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "10秒にしてほしい"})
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
	requireInvalid(t, err, "reply_to_message_id")
	_, err = f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "x", AttachmentAssetIDs: []string{foreign}})
	requireInvalid(t, err, "attachment_asset_ids") // assets of another project cannot be attached
	ids := []string{"1", "2", "3", "4", "5", "6", "7", "8"}
	_, err = f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "x", AttachmentAssetIDs: ids, Uploads: []service.Upload{upload("c.png", pngBytes)}})
	requireInvalid(t, err, "attachments") // existing assets and new files share one limit
	ws := postMessage(t, f.svc, other.ID, service.PostMessageInput{Content: "x", AttachmentAssetIDs: []string{foreign, foreign}})
	assert.Equal(t, []string{foreign}, ws.Messages[0].AttachmentAssetIDs, "the same asset is attached once")

	_, err = f.svc.Chat.PostMessage(ctx, "missing", service.PostMessageInput{Content: "x"})
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestPostMessage_AttachmentsSentLaterJoinTheBrief(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "動画を作りたい")

	postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "商品画像です", Uploads: []service.Upload{upload("a.png", pngBytes)}})
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
