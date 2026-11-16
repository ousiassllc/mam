package reftest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/audiochunks"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// withAudioLength makes the rig's S2V model one whose clip is as long as its sound (Issue #1153).
func withAudioLength() gentesttest.Option {
	return func(r *gentesttest.Rig, _ *gentest.Deps) {
		r.Config.LocalModels[localcfg.Key(gentesttest.EndpointName, "s2v")].Workflow.AudioChunks = &audiochunks.Spec{ChunkLength: 77}
	}
}

// The length select means nothing to a model whose clip is as long as its sound: with only such models chosen the form says so and has no select; with a
// mix the select stays and says those models ignore it; with none chosen it is the ordinary form (Issue #1153).
func TestTheVideoFormSaysTheLengthFollowsTheSoundInsteadOfTheSelect(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels(), withAudioLength())
	form := func(models ...string) string {
		q := paths.GenerationTest() + "?tier=detail&media=video"
		for _, m := range models {
			q += "&model=" + m
		}
		return s.page(q)
	}
	s2v, t2v := string(gentesttest.S2VModel), string(gentesttest.T2VModel)

	only := form(s2v)
	assert.Contains(t, only, `data-testid="gt-length-note"`)
	assert.Contains(t, only, "長さは音声に合わせます")
	assert.NotContains(t, only, `id="gt-duration"`, "no select to choose a length from")

	mixed := form(s2v, t2v)
	assert.Contains(t, mixed, "音声から動画を作るモデルは、この長さを使わず音声の長さに合わせます")
	assert.Contains(t, mixed, `id="gt-duration"`, "the other model still has its lengths")

	plain := form(t2v)
	assert.NotContains(t, plain, `data-testid="gt-length-note"`)
	assert.Contains(t, plain, `id="gt-duration"`)
}
