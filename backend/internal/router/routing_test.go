package router_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// seedJudgedTakes gives the Project n Takes of the model that the Judge scored, as if earlier Shots
// had been generated and judged (Multi-model routing weighs this record).
func (g *genApp) seedJudgedTakes(model domain.ModelName, n int, score int, verdict domain.JudgeVerdict) {
	g.t.Helper()
	g.execSQL(`INSERT INTO shot (id, scene_id, order_index, objective, duration_seconds) VALUES ('seed-shot', ?, 9, 'seed', 4)`, g.sceneID)
	g.execSQL(`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot, model_adapter_output) VALUES ('seed-pv', 'seed-shot', 1, '{}', '{}')`)
	scores := fmt.Sprintf(`{"product":%d,"acting":%d,"camera":%d,"continuity":%d}`, score, score, score, score)
	for i := range n {
		id := fmt.Sprintf("seed-take-%d", i)
		g.execSQL(`INSERT INTO take (id, prompt_version_id, model_name, scores) VALUES (?, 'seed-pv', ?, ?)`, id, string(model), scores)
		g.execSQL(`INSERT INTO judge_result (id, take_id, result) VALUES (?, ?, ?)`, "seed-judge-"+id, id, string(verdict))
	}
}

// routingOf reads the routing decision stored with the newest Take of the model.
func (g *genApp) routingOf(model domain.ModelName) domain.RoutingDecision {
	g.t.Helper()
	var params string
	require.NoError(g.t, g.db.Reader().QueryRow(
		`SELECT model_parameters FROM take WHERE model_name = ? AND id NOT LIKE 'seed-%' ORDER BY created_at DESC LIMIT 1`, string(model)).Scan(&params))
	var stored struct {
		Routing *domain.RoutingDecision `json:"routing"`
	}
	require.NoError(g.t, json.Unmarshal([]byte(params), &stored))
	require.NotNil(g.t, stored.Routing, "every new Take records why its model was chosen: %s", params)
	return *stored.Routing
}

// generateAfterSeed starts a generation and waits for its 3 Takes on top of the seeded ones.
func (g *genApp) generateAfterSeed(seeded int) {
	g.t.Helper()
	require.Equal(g.t, http.StatusAccepted, g.form(http.MethodPost, "/shots/"+g.shotID+"/generate", nil).Code)
	g.waitTakes(seeded + 3)
	require.Eventually(g.t, func() bool {
		return strings.Contains(g.get("/shots/"+g.shotID, true).Body.String(), "· 計画中")
	}, 30*time.Second, 10*time.Millisecond)
}

func (g *genApp) countTakes(q string) int {
	g.t.Helper()
	var n int
	require.NoError(g.t, g.db.Reader().QueryRow(q).Scan(&n))
	return n
}

func (g *genApp) takeOf(model domain.ModelName) string {
	g.t.Helper()
	var id string
	require.NoError(g.t, g.db.Reader().QueryRow(`SELECT id FROM take WHERE model_name = ? AND id NOT LIKE 'seed-%' LIMIT 1`, string(model)).Scan(&id))
	return id
}

func TestRouting_WithoutJudgeRecordTheQualityPolicyStaysAndTheTakeSaysSo(t *testing.T) {
	g := newGenApp(t, true)
	g.generateAndWait()

	d := g.routingOf(domain.ModelSeedance2)

	assert.Equal(t, domain.RoutingPolicyDefault, d.Mode)
	assert.Equal(t, domain.ModelSeedance2, d.Primary.Model)
	assert.NotEmpty(t, d.Reason)

	body := g.get("/takes/"+g.takeOf(domain.ModelSeedance2)+"/trace", true).Body.String()
	assert.Contains(t, body, "Model routing")
	assert.Contains(t, body, "Quality Policy のまま")
	assert.Contains(t, body, "実績不足", "the challengers are listed with the reason they were not chosen")
}

func TestRouting_AModelWithAGoodJudgeRecordTakesOverAndTheTraceExplainsWhy(t *testing.T) {
	g := newGenApp(t, true)
	g.seedJudgedTakes(domain.ModelKling3, 6, 95, domain.JudgePass)

	g.generateAfterSeed(6)

	d := g.routingOf(domain.ModelKling3)
	assert.Equal(t, domain.RoutingAdaptive, d.Mode)
	assert.Equal(t, domain.ModelSeedance2, d.PolicyPrimary.Model, "BALANCED starts with Seedance 2.0")
	assert.Equal(t, domain.ModelKling3, d.Primary.Model)
	assert.Equal(t, domain.ModelSeedance2, d.Fallback.Model, "the old primary is the alternative")
	assert.Equal(t, 0, g.countTakes(`SELECT COUNT(*) FROM take WHERE model_name = 'seedance-2.0'`), "every Take was made by the routed model")

	body := g.get("/takes/"+g.takeOf(domain.ModelKling3)+"/trace", true).Body.String()
	assert.Contains(t, body, "実績で切り替え")
	assert.Contains(t, body, "kling-3.0 (720p) replaces seedance-2.0")
	assert.Contains(t, body, "6 Take", "the Judge record it was weighed by")
}

func TestRouting_ALockedVideoModelIsNotRoutedAwayAndIsRecordedAsFixed(t *testing.T) {
	g := newGenApp(t, true)
	g.seedJudgedTakes(domain.ModelKling3, 6, 95, domain.JudgePass)
	require.Equal(t, http.StatusOK, g.form(http.MethodPut, "/shots/"+g.shotID+"/locks", url.Values{"parameter": {"video_model"}, "mode": {"lock"}, "value": {"seedance-2.0"}}).Code)

	g.generateAfterSeed(6)

	d := g.routingOf(domain.ModelSeedance2)
	assert.Equal(t, domain.RoutingFixed, d.Mode)
	assert.Equal(t, 0, g.countTakes(`SELECT COUNT(*) FROM take WHERE model_name = 'kling-3.0' AND id NOT LIKE 'seed-%'`))
}
