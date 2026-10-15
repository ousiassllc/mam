package editorpanel

import (
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// audioSummary is the one-line summary of an Audio Timeline version: its number, the layers it
// holds, its length, loudness and what it cost.
func audioSummary(v *domain.AudioTimelineVersion) string {
	t := v.Timeline
	var layers []string
	if t.Mode == "" { // made by the simple Audio Production of M1
		layers = append(layers, fmt.Sprintf("セリフ %d 行", len(t.Clips)))
	} else {
		for _, l := range []struct {
			layer domain.AudioLayer
			label string
		}{{domain.LayerDialogue, "Dialogue"}, {domain.LayerNarration, "Narration"}, {domain.LayerBGM, "BGM"}, {domain.LayerAmbience, "Ambience"}, {domain.LayerSE, "SE"}, {domain.LayerFoley, "Foley"}} {
			if n := t.Count(l.layer); n > 0 {
				layers = append(layers, fmt.Sprintf("%s %d", l.label, n))
			}
		}
		if n := len(t.LipSync); n > 0 {
			layers = append(layers, fmt.Sprintf("Lip Sync %d", n))
		}
	}
	text := fmt.Sprintf("v%d / ", v.VersionNumber)
	if len(layers) > 0 {
		text += strings.Join(layers, " / ") + " / "
	}
	text += fmt.Sprintf("%.1f 秒 / %.1f LUFS", t.DurationSeconds, t.MeasuredLUFS)
	if t.EstimatedCost > 0 {
		text += fmt.Sprintf(" / 見積 $%.2f", t.EstimatedCost)
	}
	return text
}

var tempoLabels = map[string]string{domain.TempoFast: "速い", domain.TempoMedium: "標準", domain.TempoSlow: "ゆっくり"}

// audioSceneText is what the Audio Director read of a Scene.
func audioSceneText(s domain.AudioScene) string {
	text := fmt.Sprintf("Scene %d", s.Index)
	if s.Title != "" {
		text += " " + s.Title
	}
	text += fmt.Sprintf("（%.1f〜%.1f 秒）: 編集テンポ %s", s.Start, s.End, tempoLabels[s.Tempo])
	if s.Emotion != "" {
		text += " / 感情 " + s.Emotion
	}
	return text
}

// audioSkippedText is an optional layer or step the Audio Director left out, and why.
func audioSkippedText(s domain.SkippedAudio) string {
	what := strings.ToUpper(string(s.Layer))
	if s.Layer == domain.LayerLipSync {
		what = "Lip Sync"
	}
	if s.Prompt != "" {
		what += "「" + s.Prompt + "」"
	}
	reason := map[string]string{domain.SkipPolicy: "Quality Policy の範囲外", domain.SkipBudget: "予算上限", domain.SkipError: "作れませんでした"}[s.Reason]
	if s.Detail != "" {
		return fmt.Sprintf("%s: %s（%s）", what, reason, s.Detail)
	}
	return what + ": " + reason
}
