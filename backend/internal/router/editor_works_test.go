package router_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// gatedLLM holds the Editor Judge's call until the test lets it go.
type gatedLLM struct {
	inner   llm.Completer
	entered chan struct{}
	release chan struct{}
}

func (g gatedLLM) Complete(ctx context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	if agent == llm.AgentEditor {
		g.entered <- struct{}{}
		<-g.release
	}
	return g.inner.Complete(ctx, agent, req)
}

func worksOf(e *editorApp) []worktrack.Record { return e.svc.Editor.Works() }

// The editing stages that have no row in the job table are recorded for the job list (worktrack): the Editor Judge runs and then
// ends, and the audio stage and the export follow, newest first. A stage the Project is not ready for is refused and records nothing.
func TestEditor_StagesAreRecordedForTheJobListFromRunningToTheirEnd(t *testing.T) {
	gate := gatedLLM{entered: make(chan struct{}, 1), release: make(chan struct{})}
	e := newEditorAppWith(t, func(d *service.Deps) {
		gate.inner = d.LLM
		d.LLM = gate
	})
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	tl := e.timeline()

	require.Equal(t, http.StatusConflict, e.form(http.MethodPost, "/projects/"+e.project+"/export", nil).Code)
	assert.Empty(t, worksOf(e), "a refused stage is not a job")

	judged := make(chan int, 1)
	go func() { judged <- e.form(http.MethodPost, "/edit-timelines/"+tl.ID+"/judge", nil).Code }()
	select {
	case <-gate.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the Editor Judge never asked the LLM")
	}
	running := worksOf(e)
	require.Len(t, running, 1)
	assert.Equal(t, worktrack.KindEditorJudge, running[0].Kind)
	assert.Equal(t, worktrack.Running, running[0].State)
	assert.Equal(t, e.project, running[0].ProjectID)
	assert.NotEmpty(t, running[0].Text)
	close(gate.release)
	require.Equal(t, http.StatusOK, <-judged)
	assert.Equal(t, worktrack.Succeeded, worksOf(e)[0].State)
	assert.NotNil(t, worksOf(e)[0].FinishedAt)
	assert.Equal(t, "AUDIO", e.state())

	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/final-qc", nil).Code)
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/export", url.Values{"codec": {"h264"}}).Code)
	kinds := []worktrack.Kind{}
	for _, w := range worksOf(e) {
		assert.Equal(t, worktrack.Succeeded, w.State)
		assert.Equal(t, e.project, w.ProjectID)
		kinds = append(kinds, w.Kind)
	}
	assert.Equal(t, []worktrack.Kind{worktrack.KindExport, worktrack.KindAudio, worktrack.KindEditorJudge}, kinds, "newest first")
}

// A stage that fails ends as failed with the words the editor shows.
func TestEditor_AFailedJudgeIsRecordedAsFailed(t *testing.T) {
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	e.llm.answer = "this is not json"
	rec := e.form(http.MethodPost, "/edit-timelines/"+e.timeline().ID+"/judge", nil)
	require.GreaterOrEqual(t, rec.Code, 400, rec.Body.String())
	works := worksOf(e)
	require.Len(t, works, 1)
	assert.Equal(t, worktrack.Failed, works[0].State)
	assert.NotEmpty(t, works[0].Error)
	assert.Equal(t, "EDITING", e.state(), "the Project went back to editing")
}
