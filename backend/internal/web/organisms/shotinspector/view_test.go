package shotinspector_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/shotinspector"
)

// The Inspector draws a form only in the Shot states in which the server accepts it (#334): the spec
// save and the version restore are refused (409) while the Shot is generated or repaired, the Reference
// Pack add while it is generated, repaired or judged. The 演出指示 form (a paid Interpreter call) drops a second submit (#669).
func TestView_LeavesOutTheFormsTheServerRefusesInTheShotState(t *testing.T) {
	now := time.Now()
	for _, state := range domain.ShotStates() {
		var sb strings.Builder
		require.NoError(t, shotinspector.View(shotinspector.Data{
			Project: domain.Project{ID: "p"},
			Shot:    domain.Shot{ID: "s1", SceneID: "sc", State: state},
			Library: []domain.ReferenceItem{{ID: "r1", Name: "ref"}},
			Versions: []domain.VersionEntry{
				{Number: 2, CreatedAt: now},
				{Number: 1, CreatedAt: now},
			},
		}).Render(context.Background(), &sb))
		html := sb.String()
		specBlocked := state == domain.ShotGenerating || state == domain.ShotRepairing
		pinBlocked := state != domain.ShotPlanning && state != domain.ShotEditing
		assert.Equal(t, !specBlocked, strings.Contains(html, `hx-put="/shots/s1/shot-spec"`), "spec form in %s", state)
		assert.Equal(t, !specBlocked, strings.Contains(html, `/shots/s1/versions/1/restore`), "restore button in %s", state)
		assert.Equal(t, !pinBlocked, strings.Contains(html, `hx-post="/shots/s1/references"`), "reference form in %s", state)
		assert.Equal(t, specBlocked, strings.Contains(html, "data-spec-edit-blocked"), "spec notice in %s", state)
		assert.Equal(t, pinBlocked, strings.Contains(html, "data-pin-reference-blocked"), "reference notice in %s", state)
		assert.Contains(t, html, `hx-post="/shots/s1/director-command" hx-target="#shot-inspector" hx-swap="outerHTML" hx-sync="this:drop" hx-disabled-elt="find button[type=submit]"`, "director-command form in %s", state)
	}
}

// The Shot Inspector holds independent inputs (the spec JSON, the 演出指示, the Reference Pack form). The answer of one
// of them is its own section, or the pasted-over input of the others is lost (#695).
func TestShotInspectorAnswersCarryOnlyTheirOwnSection(t *testing.T) {
	now := time.Now()
	d := shotinspector.Data{
		Project:  domain.Project{ID: "p"},
		Shot:     domain.Shot{ID: "s1", SceneID: "sc", State: domain.ShotEditing},
		Library:  []domain.ReferenceItem{{ID: "r1", Name: "ref"}},
		Versions: []domain.VersionEntry{{Number: 2, CreatedAt: now}, {Number: 1, CreatedAt: now}},
	}
	spec, changed, pack, whole := renderHTML(t, shotinspector.SpecSection(d)), renderHTML(t, shotinspector.SpecChanged(d)),
		renderHTML(t, shotinspector.ReferencePackSection(d)), renderHTML(t, shotinspector.View(d))

	assert.Contains(t, spec, `id="shot-spec-text"`)
	assert.Contains(t, spec, `hx-target="#shot-spec"`)
	assert.Contains(t, pack, `id="reference-pack"`)
	assert.Contains(t, pack, `hx-target="#reference-pack"`)
	assert.Contains(t, changed, `id="shot-spec"`)
	assert.Contains(t, changed, `id="version-history-shot-s1"`)
	assert.Contains(t, changed, `hx-swap-oob="true"`, "the version history that gained a version replaces the one of the page")
	assert.Contains(t, changed, `hx-target="#shot-spec"`, "a restore answers into the spec section")
	assert.NotContains(t, whole, `hx-swap-oob`)
	assert.Contains(t, whole, `hx-target="#shot-spec"`)
	assert.Contains(t, whole, `hx-target="#reference-pack"`)

	for name, html := range map[string]string{"spec": spec, "spec saved or restored": changed, "reference pack": pack} {
		assert.NotContains(t, html, `id="director-command"`, name)
		assert.NotContains(t, html, `id="shot-inspector"`, name)
	}
	assert.NotContains(t, spec, `id="reference-pack"`)
	assert.NotContains(t, pack, `id="shot-spec-text"`)
	assert.Equal(t, 1, strings.Count(whole, `hx-target="#shot-inspector"`), "only the 演出指示 answers with the whole inspector")
}

func renderHTML(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}
