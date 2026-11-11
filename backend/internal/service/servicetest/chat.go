package servicetest

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// The helpers the chat tests of service/ and of service/flowtests share (each package keeps its own fixture; these are the functions that need none).

var (
	// PNGBytes is the smallest body the upload checks read as an image.
	PNGBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	// MP4Bytes is the smallest body the upload checks read as a video.
	MP4Bytes = append([]byte("\x00\x00\x00\x18ftypisom"), bytes.Repeat([]byte{0}, 32)...)
)

// PostMessage posts in to the chat of the Project and requires it to succeed.
func PostMessage(t *testing.T, svc *service.Services, projectID string, in service.PostMessageInput) service.Workspace {
	t.Helper()
	ws, err := svc.Chat.PostMessage(context.Background(), projectID, in)
	require.NoError(t, err)
	return ws
}

// Answer replies to the card with the words of reply.
func Answer(t *testing.T, svc *service.Services, projectID, cardID, reply string) service.Workspace {
	t.Helper()
	return PostMessage(t, svc, projectID, service.PostMessageInput{Content: reply, ReplyToMessageID: cardID})
}

// Ceilings are the project and Shot budget ceilings of the Project (nil when unset).
func Ceilings(t *testing.T, svc *service.Services, projectID string) (project, shot *float64) {
	t.Helper()
	view, err := svc.Policy.Settings(context.Background(), projectID)
	require.NoError(t, err)
	return view.Policy.ProjectBudgetCeiling, view.Policy.ShotBudgetCeiling
}

// LastMessage takes a testenv.TestingT so that an Eventually condition can pass its *assert.CollectT.
func LastMessage(t testenv.TestingT, msgs []domain.ThreadMessage) domain.ThreadMessage {
	t.Helper()
	require.NotEmpty(t, msgs)
	return msgs[len(msgs)-1]
}

// WaitLastMessage waits until the last message of the thread says want: a state of the project that a job sets can be seen before the line that tells why is in the thread.
func WaitLastMessage(t *testing.T, thread func(testenv.TestingT, string) []domain.ThreadMessage, projectID, want string) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Contains(c, LastMessage(c, thread(c, projectID)).Content, want) }, 30*time.Second, 10*time.Millisecond)
}

// LastText is the text of the last message of the workspace.
func LastText(ws service.Workspace) string {
	return ws.Messages[len(ws.Messages)-1].Content
}

// OperationCards are the confirmation cards of the thread, oldest first.
func OperationCards(msgs []domain.ThreadMessage) []domain.ThreadMessage {
	var cards []domain.ThreadMessage
	for _, m := range msgs {
		if m.Kind == domain.KindQuestion && m.RefType == domain.RefOperation {
			cards = append(cards, m)
		}
	}
	return cards
}

// RequireInvalid requires err to be a ValidationError on field.
func RequireInvalid(t *testing.T, err error, field string) {
	t.Helper()
	var invalid *service.ValidationError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, field, invalid.Field)
}

// Upload is a file of data under name.
func Upload(name string, data []byte) service.Upload {
	return service.Upload{Name: name, Size: int64(len(data)), Open: func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}}
}
