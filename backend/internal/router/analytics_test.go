package router_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyticsPage_ShowsEveryMetricOfSection28AndTheNorthStar(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液のCMを作りたい")

	for _, path := range []string{"/projects/" + id + "/analytics", "/analytics"} {
		rec := a.get(path, false)
		require.Equal(t, http.StatusOK, rec.Code, path)
		body := rec.Body.String()
		assert.Contains(t, body, "<html")
		assert.Contains(t, body, `data-testid="metric-north-star"`)
		for _, name := range []string{
			"First-pass Shot Acceptance Rate", "Final Shot Acceptance Rate", "Average Retakes per Shot",
			"Human Interventions per Finished Video", "Time to First Rough Cut", "Time to Final Video",
			"Cost per Accepted Second", "Character Continuity Failure Rate", "Product Accuracy Failure Rate",
			"Prompt Adherence Rate", "AUTO Completion Rate", "User Override Rate", "Export / Completion Rate",
		} {
			assert.Contains(t, body, name, path)
		}
		assert.Contains(t, body, "外部へは送信しません")
		assert.NotContains(t, body, "NaN")
	}
	assert.NotContains(t, a.get("/projects/"+id+"/analytics", true).Body.String(), "<html", "HTMX gets the metrics only")
	assert.Equal(t, http.StatusNotFound, a.get("/projects/missing/analytics", false).Code)
	assert.Contains(t, a.get("/projects/"+id, false).Body.String(), `href="/projects/`+id+`/analytics"`, "the workspace links to the metrics")
	assert.Contains(t, a.get("/", false).Body.String(), `href="/analytics"`)
}

func TestAnalytics_HandOperationsAreRecordedAndCountAgainstTheNorthStar(t *testing.T) {
	ta := newTakeApp(t)
	count := func(kind string, override int) (n int) {
		require.NoError(t, ta.db.Reader().QueryRow(
			`SELECT COUNT(*) FROM human_intervention WHERE project_id = ? AND kind = ? AND override = ?`, ta.project, kind, override).Scan(&n))
		return n
	}
	// t2 is the AI Pick: choosing t1 contradicts it, choosing the AI Pick itself does not.
	ta.execSQL(`UPDATE take SET status = 'ai_pick' WHERE id = 't2'`)
	require.Equal(t, http.StatusOK, ta.form(http.MethodPost, "/takes/t2/select", nil).Code)
	assert.Equal(t, 1, count("select_take", 0))
	ta.execSQL(`UPDATE take SET status = 'ai_pick' WHERE id = 't2'`)
	require.Equal(t, http.StatusOK, ta.form(http.MethodPost, "/takes/t1/select", nil).Code)
	assert.Equal(t, 1, count("select_take", 1))
	require.Equal(t, http.StatusOK, ta.form(http.MethodDelete, "/takes/t1", nil).Code)
	assert.Equal(t, 1, count("reject_take", 0))

	view, err := ta.svc.Analytics.Metrics(t.Context(), ta.project)
	require.NoError(t, err)
	assert.Equal(t, 1, view.Metrics.Shots, "one Shot has Takes")
	assert.Equal(t, 1, view.Metrics.OverrideEvents)
	assert.Equal(t, 1.0, view.Metrics.UserOverride.Value())
	assert.Equal(t, 0.0, view.Metrics.NorthStar.Value(), "the Project has not finished and was touched by hand")
}

func TestAnalytics_TakeoverAndManualSettingsAreRecordedButAutoSettingsAreNot(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液のCMを作りたい")
	kinds := func() (out []string) {
		rows, err := a.db.Reader().Query(`SELECT kind FROM human_intervention WHERE project_id = ? ORDER BY rowid`, id)
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var k string
			require.NoError(t, rows.Scan(&k))
			out = append(out, k)
		}
		return out
	}

	require.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/mode", url.Values{"mode": {"director"}}).Code)
	assert.Equal(t, []string{"switch_to_director"}, kinds())
	require.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/locks", url.Values{"parameter": {"story"}, "mode": {"auto"}}).Code)
	assert.Equal(t, []string{"switch_to_director"}, kinds(), "resetting a parameter to AUTO is no hand operation")
	require.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/locks", url.Values{"parameter": {"story"}, "mode": {"lock"}}).Code)
	assert.Equal(t, []string{"switch_to_director", "lock_change"}, kinds())
}
