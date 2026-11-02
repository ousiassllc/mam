package gentest_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestAResultAndTheBatchGridSayWhetherTheModelThatMadeItMayBeUsedCommercially(t *testing.T) {
	s := newScreen(t)
	id := s.run(batchForm("flux-2", "gpt-image-2"))

	history := s.page(paths.GenerationTestHistory())
	assert.Equal(t, 8, strings.Count(history, `data-testid="gt-history-item"`))
	assert.Equal(t, 8, strings.Count(history, `data-testid="gt-history-item" data-commercial="yes"`), "each result's item carries the answer of its model")
	assert.Contains(t, history, `<option value="flux-2" data-commercial="yes"`, "so does the model choice of the filter")
	assert.Contains(t, history, `data-commercial-badge="yes"`, "the mark on the card")
	assert.Contains(t, history, `data-commercial-filter-target="#`, "the history can be filtered too")
	assert.Contains(t, history, `name="commercial"`, "the choice is a field of the history form, so it survives the paging")

	results := s.results(id)
	require.NotEmpty(t, results)
	detail := s.page(paths.GenerationTestPart("results", results[0].ID))
	assert.Contains(t, detail, `data-commercial-badge="yes"`)
}

func TestTheHistoryKeepsTheCommercialFilterInTheNextLink(t *testing.T) {
	s := newScreen(t)
	form := batchForm("flux-2")
	form.Set("count", "20")
	s.run(form)
	s.run(form)

	body := s.get(paths.GenerationTestHistory() + "?commercial=ok").Body.String()

	next := regexp.MustCompile(`hx-get="([^"]*offset=24[^"]*)"`).FindStringSubmatch(body)
	require.Len(t, next, 2)
	assert.Contains(t, next[1], "commercial=ok")
	assert.Regexp(t, `value="ok"\s+checked`, body)
}
