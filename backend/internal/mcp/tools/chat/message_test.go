package chat

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestMessageOf_CardsAreDecodedAndCarryNoBrowserURLs(t *testing.T) {
	score := 0.82
	take := messageOf(domain.ThreadMessage{ChatMessage: domain.ChatMessage{
		ID: "m1", Kind: domain.KindTake, RefType: domain.RefTake, RefID: "t1",
		Content: domain.EncodeTakeCardBody(domain.TakeCardBody{
			Title: "Take 2", Badge: "AI Pick", Score: &score, ShotID: "sh1", VideoURL: "/takes/t1/video", PosterURL: "/takes/t1/poster",
		}),
	}})
	require.NotNil(t, take.Take)
	assert.Equal(t, "t1", take.Take.TakeID)
	assert.Equal(t, "sh1", take.Take.ShotID)
	assert.Equal(t, "Take 2", take.Text)
	assert.Equal(t, &score, take.Take.Score)

	esc := messageOf(domain.ThreadMessage{
		ChatMessage: domain.ChatMessage{
			Kind: domain.KindEscalation, Content: domain.EncodeCardBody(domain.CardBody{Text: "リトライ上限", Options: []string{"続行", "中止"}, NonBlocking: true}),
		},
		Open: true,
	})
	assert.Equal(t, "リトライ上限", esc.Text)
	assert.Equal(t, []string{"続行", "中止"}, esc.Options)
	assert.True(t, esc.NonBlocking)
	assert.True(t, esc.Open)

	progress := messageOf(domain.ThreadMessage{
		ChatMessage: domain.ChatMessage{Kind: domain.KindProgress, RefType: domain.RefJob, RefID: "j1", Content: "生成中"},
		Job:         &domain.Job{ID: "j1", Kind: domain.JobGenerate, Status: domain.JobRunning},
	})
	require.NotNil(t, progress.Job)
	assert.Equal(t, "running", progress.Job.Status)
	assert.True(t, progress.Pending, "a running job's card can still change")
}

// An imported message keeps the ID of its bundle, so the ID's size says nothing about the order: the schema tells the client to follow the returned order and use a returned ID as the cursor.
func TestMessage_TheIDDescriptionDoesNotPromiseThatIDsGrowOverTime(t *testing.T) {
	f, ok := reflect.TypeFor[Message]().FieldByName("ID")
	require.True(t, ok)
	desc := f.Tag.Get("jsonschema")
	assert.NotContains(t, desc, "増える")
	for _, want := range []string{"返す順が時系列", "after", "ID の大小は時系列ではない"} {
		assert.Contains(t, desc, want)
	}
}
