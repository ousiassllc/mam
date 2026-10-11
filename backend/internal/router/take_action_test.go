package router_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The server holds to the actions it offers (select_url / reject_url): an excluded Take cannot be
// brought back by a direct POST, and a Take that is already selected or excluded is refused again, so
// none of them adds a human intervention.
func TestTakes_RefuseWhatTheyDoNotOfferAndRecordNoIntervention(t *testing.T) {
	ta := newTakeApp(t)
	interventions := func() int {
		var n int
		require.NoError(t, ta.db.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM human_intervention`).Scan(&n))
		return n
	}
	require.Equal(t, http.StatusOK, ta.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, ta.form(http.MethodDelete, "/takes/t2", nil).Code)
	before := interventions()

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/takes/t2/select"}, // excluded
		{http.MethodPost, "/takes/t1/select"}, // already selected
		{http.MethodDelete, "/takes/t2"},      // already excluded
	} {
		rec := ta.form(c.method, c.path, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, c.method+" "+c.path+": "+rec.Body.String())
	}

	assert.Equal(t, before, interventions())
	assert.Equal(t, "rejected", ta.statusOf("t2"), "the excluded Take stays excluded")
	assert.Equal(t, "selected", ta.statusOf("t1"))
}

func (ta *takeApp) statusOf(id string) string {
	ta.t.Helper()
	for _, tk := range ta.takes().Takes {
		if tk.ID == id {
			return tk.Status
		}
	}
	ta.t.Fatalf("no take %s", id)
	return ""
}

// The comparison names a Take's status as the Shot card does (molecules.TakeStatusBadge), so the
// client holds no label of its own.
func TestTakeAPI_NamesEachStatusAsTheShotCardDoes(t *testing.T) {
	ta := newTakeApp(t)
	list := func() string { return ta.get("/api/v1/shots/"+ta.shotID+"/takes", false).Body.String() }
	assert.Contains(t, list(), `"status_label":"候補"`)

	require.Equal(t, http.StatusOK, ta.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, ta.form(http.MethodDelete, "/takes/t2", nil).Code)
	assert.Contains(t, list(), `"status_label":"採用"`)
	assert.Contains(t, list(), `"status_label":"除外"`)
}
