package commercialtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// 動画の種類 (Issue #1014): the model list is narrowed by the server by the kind of a video model, the answer says so with a chip, the panel offers the
// group, and the card of a video model shows its kinds as badges (the other models have none).
func TestTheListIsNarrowedByTheServerByTheVideoMode(t *testing.T) {
	s := newScreen(t)

	t2v := s.get(paths.ModelManagerPart("models") + "?mf=1&commercial=all&video_modes=t2v").Body.String()
	assert.Contains(t, t2v, `id="mm-model-m-b"`, "m-b is the video model of the test catalog (t2v)")
	assert.NotContains(t, t2v, `id="mm-model-m-a"`, "an image model has no video mode")
	assert.NotContains(t, t2v, `id="mm-model-m-c"`)
	assert.Contains(t, t2v, "動画の種類: T2V（文字から動画）", "the chip of the filter in force")

	flf2v := s.get(paths.ModelManagerPart("models") + "?mf=1&commercial=all&video_modes=flf2v").Body.String()
	assert.NotContains(t, flf2v, `id="mm-model-m-b"`)

	bad := s.get(paths.ModelManagerPart("models") + "?mf=1&video_modes=v2v").Body.String()
	assert.Contains(t, bad, "video_modes", "a word that is none is named by its key")
}

func TestThePanelOffersTheVideoModesAndTheCardOfAVideoModelShowsItsKinds(t *testing.T) {
	s := newScreen(t)

	page := s.page()
	assert.Contains(t, page, "動画の種類")
	for _, m := range []string{"t2v", "i2v", "flf2v", "vace", "s2v", "r2v"} {
		assert.Contains(t, page, `name="video_modes" value="`+m+`"`, m)
	}

	body := s.get(paths.ModelManagerPart("models")).Body.String()
	assert.Contains(t, cardOf(t, body, "m-b"), `data-testid="model-badge-video-mode"`)
	assert.Contains(t, cardOf(t, body, "m-b"), "動画の種類: T2V（文字から動画）")
	assert.NotContains(t, cardOf(t, body, "m-a"), `model-badge-video-mode`)
}
