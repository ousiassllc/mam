package livetest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// batchForm is a batch of two prompts of the models, two results each.
func batchForm(models ...string) url.Values {
	return url.Values{"media": {"image"}, "model": models, "count": {"2"}, "aspect_ratio": {"16:9"}, "prompt": {"a red fox", "a blue fox"}, "negative": {"blurry", ""}}
}

// #928: a result, the history and the detail say whether the model may be used commercially, and the history can be filtered by it.
func TestAResultAndTheBatchGridSayWhetherTheModelThatMadeItMayBeUsedCommercially(t *testing.T) {
	s := newScreen(t)
	id := s.start(batchForm("flux-2", "gpt-image-2"))
	s.settled(id)

	history := s.get(paths.GenerationTestHistory())
	assert.Equal(t, 8, strings.Count(history, `data-testid="gt-history-item"`))
	assert.Equal(t, 8, strings.Count(history, `data-testid="gt-history-item" data-commercial="yes"`), "each result's item carries the answer of its model")
	assert.Contains(t, history, `<option value="flux-2" data-commercial="yes"`, "so does the model choice of the filter")
	assert.NotContains(t, history, `data-commercial-badge=`, "the tile is the picture only (#1004): the mark is in its detail")
	assert.Contains(t, history, `data-commercial-filter-target="#`, "the history can be filtered too")
	assert.Contains(t, history, `name="commercial"`, "the choice is a field of the history form, so it survives the paging")

	b, err := s.rig.Svc.Batch(t.Context(), id, true)
	require.NoError(t, err)
	require.NotEmpty(t, b.Results)
	assert.Contains(t, s.get(paths.GenerationTestPart("results", b.Results[0].ID)), `data-commercial-badge="yes"`)
	req := httptest.NewRequest(http.MethodGet, paths.GenerationTestPart("results", b.Results[0].ID), nil)
	req.Header.Set("HX-Target", "mam-modal-body")
	assert.Contains(t, s.htmx(req).Body.String(), `data-commercial-badge="yes"`, "and in the modal that a tile opens")
}

func TestTheHistoryKeepsTheCommercialFilterInTheNextLink(t *testing.T) {
	s := newScreen(t)
	form := batchForm("flux-2")
	form.Set("count", "20")
	s.settled(s.start(form))
	s.settled(s.start(form))

	body := s.get(paths.GenerationTestHistory() + "?commercial=ok")

	next := regexp.MustCompile(`hx-get="([^"]*offset=24[^"]*)"`).FindStringSubmatch(body)
	require.Len(t, next, 2)
	assert.Contains(t, next[1], "commercial=ok")
	assert.Regexp(t, `value="ok"\s+checked`, body)
}
