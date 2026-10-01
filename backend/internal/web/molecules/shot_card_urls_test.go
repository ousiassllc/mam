package molecules_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

func TestShotCard_EscapesImportedShotAndTakeIDsInEveryURL(t *testing.T) {
	html := render(t, molecules.ShotCard(molecules.ShotCardData{
		ProjectID: "p",
		Shot:      domain.Shot{ID: "s?1#x", SceneID: "sc", State: domain.ShotGenerating},
		Takes:     []domain.Take{{ID: "t?1%x", ShotID: "s?1#x", Status: domain.TakeAIPick, FilePath: "t.mp4"}},
	}))

	assert.Contains(t, html, `/shots/s%3F1%23x`)
	assert.Contains(t, html, `/ws/shots/s%3F1%23x`)
	assert.Contains(t, html, `/takes/t%3F1%25x/select`)
	assert.Contains(t, html, `/takes/t%3F1%25x`)
	assert.NotContains(t, html, `/shots/s?1`)
	assert.NotContains(t, html, `/takes/t?1`)
	assert.Contains(t, html, `/projects/p/scenes/sc?shot=s%3F1%23x`)
	assert.NotContains(t, html, `?shot=s?1`)
}

func TestTakeCard_DoesNotLoadAVideoOrPosterFromAnotherHost(t *testing.T) {
	card := func(content string) string {
		return render(t, molecules.TakeCard(domain.ThreadMessage{ChatMessage: domain.ChatMessage{ID: "m", Kind: domain.KindTake, RefID: "t1", Content: content}}))
	}
	local := card(`{"title":"T","video_url":"/takes/t1/video","poster_url":"/takes/t1/thumbnail"}`)
	assert.Contains(t, local, `video-src="/takes/t1/video"`)
	assert.Contains(t, local, `poster="/takes/t1/thumbnail"`)

	assert.NotContains(t, card(`{"title":"T","video_url":"/\\evil.example/x.mp4"}`), "evil.example")
	assert.NotContains(t, card(`{"title":"T","video_url":"/takes/t1/video","poster_url":"/\\evil.example/p.jpg"}`), "evil.example")
	assert.NotContains(t, card(`{"title":"T","video_url":"/%5Cevil.example/x.mp4"}`), "evil.example")
}
