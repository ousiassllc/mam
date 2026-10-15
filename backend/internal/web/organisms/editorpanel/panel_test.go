package editorpanel_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/editorpanel"
)

// roughCutButton is the opening tag of the Rough Cut button of the panel.
var roughCutButton = regexp.MustCompile(`<button[^>]*edit/rough-cut[^>]*>`)

// disabledAttr matches the disabled attribute, not the disabled: variants of the class list or hx-disabled-elt.
var disabledAttr = regexp.MustCompile(`\sdisabled[\s=>]`)

// The Rough Cut button can be pressed only in the Project states the server accepts a Rough Cut in
// (#337): from the others it answers 409, after the confirmation dialog of a re-make.
func TestEditorPanel_RoughCutButtonFollowsTheStatesTheServerEditsIn(t *testing.T) {
	for _, state := range domain.ProjectStates() {
		for _, timelineID := range []string{"", "tl1"} {
			var sb strings.Builder
			require.NoError(t, editorpanel.Actions(editorpanel.Data{
				Project:    domain.Project{ID: "p", State: state},
				TimelineID: timelineID,
			}).Render(context.Background(), &sb))
			button := roughCutButton.FindString(sb.String())
			require.NotEmpty(t, button, "state %s timeline %q", state, timelineID)
			assert.Equal(t, !state.CanEdit(), disabledAttr.MatchString(button), "state %s timeline %q: %s", state, timelineID, button)
		}
	}
}

// exportButton is the opening tag of the export button of the panel.
var exportButton = regexp.MustCompile(`<button[^>]*type="submit"[^>]*>\s*書き出す`)

func renderPanelActions(t *testing.T, d editorpanel.Data) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, editorpanel.Actions(d).Render(context.Background(), &sb))
	return sb.String()
}

// The export button can be pressed only when the server accepts the export (#339): the Project is
// READY / EXPORTED and the series shown has audio made for its latest Edit Timeline. A Branch without
// audio (404) or with audio of an older edit (409) gets a disabled button and says why.
func TestEditorPanel_ExportButtonFollowsWhatTheServerExports(t *testing.T) {
	current := &domain.AudioTimelineVersion{Timeline: domain.AudioTimeline{EditTimelineID: "tl2"}}
	stale := &domain.AudioTimelineVersion{Timeline: domain.AudioTimeline{EditTimelineID: "tl1"}}
	cases := []struct {
		name    string
		state   domain.ProjectState
		audio   *domain.AudioTimelineVersion
		render  bool
		enabled bool
		blocker string
	}{
		{"current audio", domain.StateReady, current, true, true, ""},
		{"exported again", domain.StateExported, current, true, true, ""},
		{"no audio", domain.StateReady, nil, true, false, "音声がない"},
		{"audio of an older edit", domain.StateReady, stale, true, false, "最新の編集に対応していない"},
		{"not exportable state", domain.StateAudio, current, true, false, ""},
		{"no ffmpeg", domain.StateReady, current, false, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := renderPanelActions(t, editorpanel.Data{
				Project: domain.Project{ID: "p", State: tc.state}, BranchID: "b1", TimelineID: "tl2", Audio: tc.audio, CanRender: tc.render,
			})
			button := exportButton.FindString(body)
			require.NotEmpty(t, button)
			assert.Equal(t, !tc.enabled, disabledAttr.MatchString(button), button)
			if tc.blocker == "" {
				assert.NotContains(t, body, "data-export-blocked")
			} else {
				assert.Contains(t, body, "data-export-blocked")
				assert.Contains(t, body, tc.blocker)
			}
		})
	}
}

var (
	finalQCButton = regexp.MustCompile(`<button[^>]*final-qc[^>]*>`)
	audioButton   = regexp.MustCompile(`<button[^>]*audio/generate[^>]*>`)
)

// Final QC always inspects the trunk (#340): its button follows the audio of the trunk, whatever the
// Branch shown has, and a Branch panel says when the trunk audio is what is missing.
func TestEditorPanel_FinalQCButtonFollowsTheAudioOfTheTrunk(t *testing.T) {
	audio := &domain.AudioTimelineVersion{}
	cases := []struct {
		name         string
		branch       string
		own, trunk   *domain.AudioTimelineVersion
		enabled      bool
		trunkMissing bool
	}{
		{"trunk with audio", "", audio, audio, true, false},
		{"trunk without audio", "", nil, nil, false, false},
		{"branch audio only", "b1", audio, nil, false, true},
		{"trunk audio only", "b1", nil, audio, true, false},
		{"both", "b1", audio, audio, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := renderPanelActions(t, editorpanel.Data{
				Project: domain.Project{ID: "p", State: domain.StateAudio}, BranchID: tc.branch, TimelineID: "tl1", Audio: tc.own, TrunkAudio: tc.trunk, CanRender: true,
			})
			button := finalQCButton.FindString(body)
			require.NotEmpty(t, button)
			assert.Equal(t, !tc.enabled, disabledAttr.MatchString(button), button)
			assert.Equal(t, tc.trunkMissing, strings.Contains(body, "data-qc-trunk-missing"))
		})
	}
}

// The audio button needs an Edit Timeline in the series shown: the server answers 404 without one.
func TestEditorPanel_AudioButtonNeedsAnEditOfTheSeries(t *testing.T) {
	for _, timelineID := range []string{"", "tl1"} {
		body := renderPanelActions(t, editorpanel.Data{
			Project: domain.Project{ID: "p", State: domain.StateAudio}, BranchID: "b1", TimelineID: timelineID, CanRender: true,
		})
		button := audioButton.FindString(body)
		require.NotEmpty(t, button)
		assert.Equal(t, timelineID == "", disabledAttr.MatchString(button), "timeline %q: %s", timelineID, button)
	}
}

// The stage actions and the reload after a timeline save replace the regions that show a result, never the
// inputs: the export options chosen and the voice sample being picked survive them (#731).
func TestEditorPanel_ActionsFragmentCarriesNoInputOfTheForms(t *testing.T) {
	d := editorpanel.Data{
		Project: domain.Project{ID: "p", State: domain.StateReady}, TimelineID: "tl1", CanRender: true,
		Audio: &domain.AudioTimelineVersion{Timeline: domain.AudioTimeline{EditTimelineID: "tl1"}}, Voices: []domain.VoiceClone{{Speaker: "ユイ"}},
		Exports: []domain.Export{{ID: "e1", AspectRatio: "16:9", Resolution: "1080p", Codec: "h264"}},
	}
	body := renderFragment(t, editorpanel.ActionsFragment(d))

	for _, input := range []string{`name="aspect_ratio"`, `name="resolution"`, `name="codec"`, `name="speaker"`, `name="sample"`, `id="editor-actions"`, `id="editor-voice-form"`} {
		assert.NotContains(t, body, input)
	}
	for _, region := range []string{"editor-edit-stage", "editor-audio-stage", "editor-voices", "editor-final-qc", "editor-export-submit", "editor-exports", "project-state"} {
		assert.Regexp(t, `<[^>]*hx-swap-oob="true"[^>]*id="`+region+`"|<[^>]*id="`+region+`"[^>]*hx-swap-oob="true"`, body, region)
	}
	assert.Contains(t, body, "ユイ（クローン音声）")
	assert.Contains(t, body, "16:9 / 1080p / h264")
	assert.False(t, disabledAttr.MatchString(exportButton.FindString(body)), "the export button follows the state")
}

// The panel holds the inputs outside what the answers replace; no button or form targets #editor-actions.
func TestEditorPanel_InputsAreOutsideTheRegionsThatAreReplaced(t *testing.T) {
	body := renderPanelActions(t, editorpanel.Data{Project: domain.Project{ID: "p"}, Defaults: domain.ExportOptions{AspectRatio: "9:16", Resolution: "2160p", Codec: "h265"}})

	for _, input := range []string{`name="aspect_ratio"`, `name="resolution"`, `name="codec"`, `name="speaker"`, `name="sample"`} {
		assert.Contains(t, body, input)
	}
	assert.Contains(t, body, `hx-swap="none"`)
	assert.NotContains(t, body, `hx-target="#editor-actions"`)
	assert.NotContains(t, body, "hx-swap-oob")
	assert.Regexp(t, `<option value="9:16" selected`, body)
	assert.NotRegexp(t, `<option value="9:16"[^>]*hx-swap-oob`, body)
}

// A cloned voice empties the voice form, out of band; every other stage leaves it.
func TestEditorPanel_VoiceClonedEmptiesTheVoiceForm(t *testing.T) {
	body := renderFragment(t, editorpanel.VoiceCloned(editorpanel.Data{Project: domain.Project{ID: "p"}}))

	assert.Regexp(t, `<form[^>]*hx-swap-oob="true"[^>]*id="editor-voice-form"|<form[^>]*id="editor-voice-form"[^>]*hx-swap-oob="true"`, body)
	assert.Contains(t, body, `name="sample"`)
	assert.Contains(t, body, `id="editor-voices"`)
}

func renderFragment(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}
