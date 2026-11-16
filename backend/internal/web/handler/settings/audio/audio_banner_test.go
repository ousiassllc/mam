package audio_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// Several weights of one provider whose commercial use is not clear are one cause: one banner with one line each, and the screen has no yellow box.
func TestAudioDetail_SeveralUnclearLicencesAreOneBannerWithALineEachAndNoYellow(t *testing.T) {
	rt := machine(true)
	v := rt.views["gpt-sovits"]
	v.Weights.Assets = []localruntime.AssetView{
		{Name: "whisper-tiny", License: catalog.ResourceLicense{Commercial: modellicense.Unknown}},
		{Name: "hubert-base", License: catalog.ResourceLicense{Commercial: modellicense.Unknown}},
	}
	rt.views["gpt-sovits"] = v
	s := newScreen(t, rt, nil)

	detail := s.detail("voice", "local")

	assert.Equal(t, 1, strings.Count(detail, `data-testid="audio-unclear-voice_provider"`), "one banner for the one cause")
	assert.Contains(t, detail, "商用利用が確認できていない（不可・不明）重み・依存ファイルが 2 件あります")
	assert.Equal(t, 2, strings.Count(detail, "<li class=\"break-words\">「"), "a line for each weight")
	assert.Contains(t, detail, "「whisper-tiny」")
	assert.Contains(t, detail, "「hubert-base」")
	for _, yellow := range []string{"text-warning", "border-warning", "bg-warning"} {
		assert.NotContains(t, detail, yellow, "the detail does not speak in the colour of a state")
	}
	panel := withoutCommercialMarks(audioPanel(s.page()))
	for _, yellow := range []string{"text-warning", "border-warning", "bg-warning"} {
		assert.NotContains(t, panel, yellow, "no yellow box on the screen but the commercial marks")
	}
}

// A PC that cannot run the local providers: the refusal is one banner (the reason and the saved local choice are its lines) and the card states are quiet chips.
func TestAudioScreen_AnUnsupportedPCIsOneQuietBannerAndAMutedChip(t *testing.T) {
	s := newScreen(t, machine(false), nil)
	require.NoError(t, os.WriteFile(s.path, []byte(`{"voice_provider":"local"}`), 0o600))

	detail := s.detail("voice", "local")

	assert.Equal(t, 1, strings.Count(detail, `data-testid="audio-unsupported-voice_provider"`))
	assert.Contains(t, detail, "保存済みの設定はローカルですが、この PC では動かせないため、今は使われません")
	assert.Equal(t, 1, strings.Count(detail, "保存済みの設定はローカルですが"), "the line is in the banner, not once more")
	panel := audioPanel(s.page())
	for _, yellow := range []string{"text-warning", "border-warning", "bg-warning"} {
		assert.NotContains(t, detail, yellow)
		assert.NotContains(t, panel, yellow)
	}
	assert.Contains(t, panel, "bg-accent-soft text-mute", "the state of a card is the muted chip")
}

// audioPanel is the part of the page that is the screen's own (the shell has its alert and connection toasts).
func audioPanel(page string) string {
	_, rest, _ := strings.Cut(page, `data-testid="audio-panel"`)
	panel, _, _ := strings.Cut(rest, "</main>")
	return panel
}

// withoutCommercialMarks drops the "商用注意" marks, which stay on their own tone.
func withoutCommercialMarks(html string) string {
	return strings.ReplaceAll(html, `bg-warning/20 text-warning">商用注意`, `">商用注意`)
}
