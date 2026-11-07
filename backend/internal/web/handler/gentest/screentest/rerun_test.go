package screentest_test

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// #1109: a second "この内容で実行する" used to be drawn into the same place (#gt-batch, outerHTML) as the first, and so were the polls and the
// cancel of every batch: one slot for every run. Each run now has a block of its own, added under the earlier ones.

func block(id string) string { return `id="gt-batch-` + id + `"` }

// form of one run: a model and a size, so that two runs differ in what they were set to.
func runForm(model, aspect, count string) url.Values {
	f := batchForm(model)
	f.Set("aspect_ratio", aspect)
	f.Set("count", count)
	return f
}

func TestRunsAreAddedToTheEndOfTheListAndNeverReplaceEachOther(t *testing.T) {
	s := newScreen(t)

	page := s.page(paths.GenerationTest())
	assert.Contains(t, page, `id="gt-batches"`, "the list the blocks of the runs are added to")
	assert.NotContains(t, page, ` id="gt-batch"`, "no single slot every run is drawn into")

	form := s.confirmed(runForm("flux-2", "1:1", "1"))
	confirm := s.upload(paths.GenerationTestPart("estimate"), runForm("flux-2", "1:1", "1"), "", nil).Body.String()
	assert.Contains(t, confirm, `hx-target="#gt-batches"`, "the answer of the run goes to the list")
	assert.Contains(t, confirm, `hx-swap="beforeend"`, "and is added after the blocks that are there")
	assert.NotContains(t, confirm, `hx-target="#gt-batch"`)

	first, firstID := s.start(form)
	s.settled(firstID)
	second, secondID := s.start(s.confirmed(runForm("flux-2", "16:9", "2")))
	s.settled(secondID)

	require.NotEqual(t, firstID, secondID, "every run is a batch of its own")
	assert.Contains(t, first.Body.String(), block(firstID))
	assert.Contains(t, second.Body.String(), block(secondID), "the answer of the second run is the block of the second batch")
	assert.NotContains(t, second.Body.String(), block(firstID), "it takes nothing of the first run's block")
	assert.NotContains(t, second.Body.String(), ` id="gt-batch"`, "and has no id the first run's block shares")
}

func TestEveryBlockNamesItsRunAndItsSettingsAndMatchesTheHistory(t *testing.T) {
	s := newScreen(t)
	_, firstID := s.start(s.confirmed(runForm("flux-2", "1:1", "1")))
	_, secondID := s.start(s.confirmed(runForm("flux-2", "16:9", "2")))
	firstBody, secondBody := s.settled(firstID), s.settled(secondID)

	assert.Contains(t, firstBody, `data-batch="`+firstID+`"`)
	assert.Contains(t, secondBody, `data-batch="`+secondID+`"`)
	assert.Contains(t, firstBody, "縦横比")
	assert.Contains(t, firstBody, `<dd class="text-ink">1:1</dd>`)
	assert.NotContains(t, firstBody, `<dd class="text-ink">16:9</dd>`, "a block shows its own settings, not another run's")
	assert.Contains(t, secondBody, `<dd class="text-ink">16:9</dd>`)
	assert.NotContains(t, secondBody, `<dd class="text-ink">1:1</dd>`)
	assert.Contains(t, firstBody, "ID "+firstID[:8])
	assert.Contains(t, secondBody, "ID "+secondID[:8])

	// The key of the block is the batch the history's results carry: each result of the history is in the grid of the block of its batch.
	for id, body := range map[string]string{firstID: firstBody, secondID: secondBody} {
		results := s.results(id)
		require.NotEmpty(t, results)
		for _, r := range results {
			assert.Equal(t, id, r.BatchID)
			assert.Contains(t, body, `data-result="`+r.ID+`"`, "the result of this run is in this run's block")
		}
	}
	assert.Len(t, s.results(firstID), 2, "two prompts at a count of 1")
	assert.Len(t, s.results(secondID), 4, "two prompts at a count of 2")
}

func TestOnlyARunningBlockPollsAndItPollsItselfAndNoOtherBlock(t *testing.T) {
	s := newScreen(t)
	finishedID := s.run(runForm("nano-banana", "1:1", "1"))
	release := s.rig.Fake.Hold(domain.ModelFlux2)
	rec, runningID := s.start(s.confirmed(runForm("flux-2", "16:9", "1")))
	<-s.rig.Fake.Started()

	body := s.get(paths.GenerationTestPart("batches", runningID)).Body.String()
	assert.Contains(t, rec.Body.String(), `hx-trigger="every 2s"`)
	assert.Regexp(t, regexp.MustCompile(`hx-get="`+regexp.QuoteMeta(paths.GenerationTestPart("batches", runningID))+`"`), body, "it asks for its own batch")
	assert.NotContains(t, body, paths.GenerationTestPart("batches", finishedID), "and not for the other run's")
	assert.Contains(t, body, `hx-target="this"`, "the answer replaces this block and no other")
	assert.Contains(t, body, block(runningID))

	done := s.get(paths.GenerationTestPart("batches", finishedID)).Body.String()
	assert.NotContains(t, done, "every 2s", "a run that is over asks for nothing (no poll stays on an old run)")
	assert.NotContains(t, done, `hx-trigger`)
	assert.Contains(t, done, block(finishedID))

	close(release)
	assert.NotContains(t, s.settled(runningID), `hx-trigger`)
}

func TestEachBlockFollowsItsOwnRunSuccessFailureAndCancel(t *testing.T) {
	s := newScreen(t)
	s.rig.Fake.FailWith(domain.ModelNanoBanana, errors.New("the model refused"))
	ok := s.run(runForm("flux-2", "1:1", "1"))
	failed := s.run(runForm("nano-banana", "1:1", "1"))
	release := s.rig.Fake.Hold(domain.ModelFlux2)
	_, running := s.start(s.confirmed(runForm("flux-2", "16:9", "1")))
	<-s.rig.Fake.Started()

	state := func(id string) string {
		body := s.get(paths.GenerationTestPart("batches", id)).Body.String()
		m := regexp.MustCompile(`data-testid="gt-batch" data-state="([a-z]+)"`).FindStringSubmatch(body)
		require.Len(t, m, 2, body)
		return m[1]
	}
	assert.Equal(t, "succeeded", state(ok))
	assert.Equal(t, "failed", state(failed))
	assert.Equal(t, "running", state(running))

	rec := s.do(http.MethodPost, paths.GenerationTestPart("batches", running, "cancel"), nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), block(running), "the answer of a cancel is the block it was asked on")
	assert.NotContains(t, rec.Body.String(), block(ok))
	close(release)
	s.settled(running)
	assert.Equal(t, "succeeded", state(ok), "what happens to a later run leaves the earlier blocks as they were")
	assert.Equal(t, "failed", state(failed))
}

func TestARefusedRunIsABlockWithoutABatchAndLeavesTheOthers(t *testing.T) {
	s := newScreen(t)
	_, id := s.start(s.confirmed(runForm("flux-2", "1:1", "1")))
	s.settled(id)

	form := s.confirmed(runForm("flux-2", "16:9", "1"))
	form.Set("estimate_id", "not-the-estimate")
	rec := s.do(http.MethodPost, paths.GenerationTestPart("run"), form)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="gt-batch-error"`)
	assert.NotContains(t, rec.Body.String(), `data-batch=`, "it is no run: it has no batch to key it")
	assert.NotContains(t, rec.Body.String(), block(id))
	assert.NotContains(t, rec.Body.String(), "hx-trigger", "nothing to poll")
}
