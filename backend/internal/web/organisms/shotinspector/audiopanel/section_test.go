package audiopanel_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/shotinspector/audiopanel"
)

func render(t *testing.T, d audiopanel.Data) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, audiopanel.Section(d).Render(context.Background(), &sb))
	return sb.String()
}

func base() audiopanel.Data {
	return audiopanel.Data{
		ShotID: "s1", ProjectID: "p1", MaxSeconds: 60, Pictures: 1,
		Choices: []audiopanel.Choice{{AssetID: "a1", MimeType: audiofile.WAV, SizeBytes: 2048, CreatedAt: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}},
	}
}

func TestSection_ShowsTheAudioWithAPlayerThatFetchesNothingUntilPlayed(t *testing.T) {
	d := base()
	d.Current = &audiopanel.Current{AssetID: "a1", MimeType: audiofile.MP3, SizeBytes: 4096, Seconds: 12.34}

	html := render(t, d)

	assert.Contains(t, html, "MP3 · 12.3 秒 · 4.0 KB")
	assert.Contains(t, html, `<audio controls preload="none"`)
	assert.Contains(t, html, `aria-label="この Shot の音声"`, "the player has a name")
	assert.Contains(t, html, `src="/projects/p1/assets/a1"`)
	assert.Contains(t, html, "音声の長さの上限は 60 秒です")
	assert.NotContains(t, html, "data-audio-no-picture")
	assert.NotContains(t, html, "data-audio-problem")
}

func TestSection_TheSelectAndTheButtonsHaveNames(t *testing.T) {
	d := base()
	d.Current = &audiopanel.Current{AssetID: "a1", MimeType: audiofile.WAV}

	html := render(t, d)

	assert.Contains(t, html, `<label for="shot-audio-asset"`)
	assert.Contains(t, html, `id="shot-audio-asset"`)
	assert.Contains(t, html, ">つなぐ</button>")
	assert.Contains(t, html, ">はずす</button>")
	assert.Contains(t, html, `hx-sync="this:drop"`, "a second submit is dropped")
	assert.Contains(t, html, `hx-target="#shot-audio"`)
}

func TestSection_WithoutAudioItOffersNoUntieAndPlaysNothing(t *testing.T) {
	html := render(t, base())

	assert.Contains(t, html, "音声はつないでいません")
	assert.Contains(t, html, `<option value="" disabled selected>音声を選んでください</option>`)
	assert.NotContains(t, html, "<audio")
	assert.NotContains(t, html, "data-audio-untie")
}

func TestSection_AProblemAndAMissingPictureAreAlerts(t *testing.T) {
	d := base()
	d.Pictures = 0
	d.Current = &audiopanel.Current{AssetID: "a1", MimeType: audiofile.WAV}
	assert.Contains(t, render(t, d), `role="alert" data-audio-no-picture`)

	d.Current.Problem = "音声が長すぎます（70.0 秒。上限は 60 秒）。短い音声に選び直してください"
	html := render(t, d)
	assert.Contains(t, html, `role="alert" data-audio-problem`)
	assert.NotContains(t, html, "data-audio-no-picture", "one reason at a time: the tie itself is the first")
}

func TestSection_BusyAndLockedAreReadOnlyWithTheReason(t *testing.T) {
	for name, tc := range map[string]struct {
		set    func(*audiopanel.Data)
		reason string
	}{
		"busy":   {func(d *audiopanel.Data) { d.Busy = true }, "生成・Repair 中は音声を変えられません"},
		"locked": {func(d *audiopanel.Data) { d.Locked = true }, "LOCK されているため、音声を変えられません"},
	} {
		d := base()
		d.Current = &audiopanel.Current{AssetID: "a1", MimeType: audiofile.WAV}
		tc.set(&d)

		html := render(t, d)

		assert.Contains(t, html, tc.reason, name)
		assert.Contains(t, html, "<audio", "%s: the audio can still be played", name)
		assert.NotContains(t, html, "<form", name)
		assert.NotContains(t, html, "<select", name)
	}
}

func TestSection_ShowsHowToGetAnAudioInWhenTheProjectHasNone(t *testing.T) {
	d := base()
	d.Choices = nil

	html := render(t, d)

	assert.Contains(t, html, "data-audio-empty")
	assert.Contains(t, html, "WAV・MP3・FLAC・OGG・M4A、50 MiB まで")
	assert.NotContains(t, html, "<form")
}

func TestSection_NothingForAShotWithoutAnIDAndAnUnreadableAudioOffersNothing(t *testing.T) {
	assert.Empty(t, strings.TrimSpace(render(t, audiopanel.Data{})))

	d := base()
	d.Unavailable = true
	html := render(t, d)
	assert.Contains(t, html, "data-audio-unavailable")
	assert.NotContains(t, html, "<form")
}

func TestPlaceholder_LoadsTheSectionAfterTheInspectorIsShown(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, audiopanel.Placeholder("s1").Render(context.Background(), &sb))

	assert.Contains(t, sb.String(), `id="shot-audio" aria-label="音声"`)
	assert.Contains(t, sb.String(), `hx-get="/shots/s1/audio" hx-trigger="load" hx-swap="outerHTML"`)
}
