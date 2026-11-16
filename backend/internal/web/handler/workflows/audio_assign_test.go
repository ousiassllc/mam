package workflows_test

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// An imported speech-to-video graph carries the audio assignment (its LoadAudio node is guessed), the editor offers the audio row where the other values are, and a
// saved choice of the node input is kept (Issue #1153).
func TestAnImportedSpeechToVideoGraphCarriesTheAudioAssignmentTheEditorShows(t *testing.T) {
	s := newScreen(t)
	rec := s.upload(paths.WorkflowsPart("import"), map[string]string{"name": "Talk", "kind": "video", "api_json": graphOf(t, "audio2video-wan22-s2v-14b")}, "", true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	id := idOf(t, s, "Talk")

	edit := s.get(paths.Workflow(id, "edit")).Body.String()
	assert.Contains(t, edit, `name="assign_audio"`)
	assert.Contains(t, edit, "音声ファイル")
	audio := regexp.MustCompile(`(?s)<select[^>]*name="assign_audio".*?</select>`).FindString(edit)
	assert.Regexp(t, `<option value="58\|audio"[^>]* selected`, audio, "the LoadAudio node of the graph is the guess")

	d, err := s.svc.Get(t.Context(), id)
	require.NoError(t, err)
	var got localcfg.Assignment
	for _, a := range d.Spec.Assignments {
		if a.Param == localcfg.ParamAudio {
			got = a
		}
	}
	assert.Equal(t, localcfg.Assignment{Param: localcfg.ParamAudio, Node: "58", Input: "audio"}, got)
}

// idOf is the id of the saved workflow of the name.
func idOf(t *testing.T, s *screen, name string) string {
	t.Helper()
	list, err := s.svc.List(t.Context())
	require.NoError(t, err)
	for _, w := range list {
		if w.Name == name {
			return w.ID
		}
	}
	require.FailNow(t, "no workflow named "+name)
	return ""
}
