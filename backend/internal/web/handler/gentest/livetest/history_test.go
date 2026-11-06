package livetest_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func fluxForm(count string) url.Values {
	return url.Values{"media": {"image"}, "model": {"flux-2"}, "count": {count}, "prompt": {"fox"}, "negative": {""}}
}

// #925: the part of the history that changes (the spend and the results, with the refresh button) asks for itself every 2 s
// while a result waits or runs, leaves the filter alone, and stops asking when the last one has finished.
func TestHistoryRefreshesItselfWhileAResultRunsAndStopsWhenItIsDone(t *testing.T) {
	s := newScreen(t)
	release := s.rig.Fake.Hold(domain.ModelFlux2)
	id := s.start(fluxForm("1"))
	<-s.rig.Fake.Started()

	whole := s.get(paths.GenerationTestHistory() + "?model=flux-2&media=image")
	assert.Contains(t, whole, `data-testid="gt-filter"`, "the list asked for as it is has the filter")
	live := regexp.MustCompile(`id="gt-history-live"[^>]*hx-get="([^"]+)"[^>]*hx-trigger="every 2s"`).FindStringSubmatch(whole)
	require.Len(t, live, 2, "the part polls while a result runs: %s", whole)
	refresh := strings.ReplaceAll(live[1], "&amp;", "&")
	assert.Contains(t, refresh, "live=1")
	assert.Contains(t, refresh, "model=flux-2", "the poll keeps the filter")
	assert.Contains(t, refresh, "media=image")
	assert.Contains(t, whole, `data-testid="refresh"`)
	assert.Contains(t, whole, `aria-label="更新"`)
	assert.Contains(t, whole, `data-testid="refresh-updated"`)

	part := s.get(refresh)
	assert.NotContains(t, part, `data-testid="gt-filter"`, "a poll never swaps the filter the reader is using")
	assert.NotContains(t, part, "<html")
	assert.Contains(t, part, `data-active="true"`)
	assert.Contains(t, part, `hx-trigger="every 2s"`)
	assert.Contains(t, part, `data-status="running"`)

	release <- struct{}{}
	s.settled(id)
	done := s.get(refresh)
	assert.Contains(t, done, `data-status="succeeded"`, "a finished result shows at once")
	assert.NotContains(t, done, `data-status="running"`)
	assert.Contains(t, done, `data-active="false"`)
	assert.NotContains(t, done, `hx-trigger`, "nothing runs any more, so it stops asking")
	assert.Contains(t, done, `data-testid="refresh"`, "the button stays for the reader")
}

// A cancelled result is not running: the part shows it as soon as the batch has ended and stops asking.
func TestHistoryStopsPollingWhenTheBatchWasCancelled(t *testing.T) {
	s := newScreen(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		v := c.Vendors[modeladapter.VendorFlux]
		v.MaxConcurrency = 1
		c.Vendors[modeladapter.VendorFlux] = v
	}))
	release := s.rig.Fake.Hold(domain.ModelFlux2)
	id := s.start(fluxForm("3"))
	<-s.rig.Fake.Started()
	assert.Contains(t, s.get(paths.GenerationTestHistory()+"?live=1"), `hx-trigger="every 2s"`)

	require.Equal(t, http.StatusOK, s.do(http.MethodPost, paths.GenerationTestPart("batches", id, "cancel"), nil).Code)
	release <- struct{}{}
	s.settled(id)
	body := s.get(paths.GenerationTestHistory() + "?live=1")
	assert.Contains(t, body, `data-status="cancelled"`)
	assert.NotContains(t, body, `hx-trigger`)
	assert.NotContains(t, body, `data-testid="gt-filter"`)
}

// The refresh button of a page of the history reads that page again, not the first one (#1079).
func TestHistoryRefreshKeepsThePageThatIsShown(t *testing.T) {
	s := newScreen(t)
	for range 2 {
		s.settled(s.start(fluxForm("20")))
	}
	body := s.get(paths.GenerationTestHistory() + "?model=flux-2&page=2")
	button := regexp.MustCompile(`<button[^>]*hx-get="([^"]+)"[^>]*data-testid="refresh"`).FindStringSubmatch(body)
	require.Len(t, button, 2)
	assert.Equal(t, "/generation-test/history?live=1&amp;model=flux-2&amp;page=2", button[1])
	assert.Equal(t, 16, strings.Count(s.get(strings.ReplaceAll(button[1], "&amp;", "&")), `data-testid="gt-history-item"`))
}

// While a result of the second page runs, the polling asks for the second page again (it does not fall back to the first), and a page past the last
// one that the polling is on (the results were deleted) comes back as the last page, with its own address to ask for.
func TestHistoryPollingOfAPageAsksForThatPage(t *testing.T) {
	s := newScreen(t)
	for range 2 {
		s.settled(s.start(fluxForm("20")))
	}
	release := s.rig.Fake.Hold(domain.ModelFlux2)
	s.start(fluxForm("1")) // the newest result runs: the first page has it, the second page has the older ones
	<-s.rig.Fake.Started()
	defer func() { release <- struct{}{} }()

	first := s.get(paths.GenerationTestHistory() + "?model=flux-2")
	assert.Contains(t, first, `hx-trigger="every 2s"`, "a running result is on the first page")
	assert.Contains(t, first, `hx-get="/generation-test/history?live=1&amp;model=flux-2"`)
	second := s.get(paths.GenerationTestHistory() + "?model=flux-2&page=2")
	assert.NotContains(t, second, `hx-trigger="every 2s"`, "no result of the second page runs: no polling there")

	beyond := s.get(paths.GenerationTestHistory() + "?model=flux-2&page=99&live=1")
	assert.Contains(t, beyond, `hx-get="/generation-test/history?live=1&amp;model=flux-2&amp;page=2"`, "the refresh reads the last page now")
}
