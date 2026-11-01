// Package audiosummary writes the chat message after the Audio Director finished (spec/api/endpoints.md): what was made, the loudness,
// the estimated cost, and the layers it could not make and why.
package audiosummary

import (
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Of is the chat message after the Audio Director finished. notes are what the service tells besides (the providers a run switched to the
// cloud, and why: audiosettings.Service.Settle): each is a line of its own at the end.
func Of(tl domain.AudioTimeline, branch bool, notes ...string) string {
	text := made(tl, branch)
	for _, note := range notes {
		text += "\n" + note
	}
	return text
}

// made is what was made, and what could not be.
func made(tl domain.AudioTimeline, branch bool) string {
	var sb strings.Builder
	sb.WriteString("音声を作成しました")
	if branch {
		sb.WriteString("（Branch）")
	}
	if tl.MixPath == "" {
		sb.WriteString("（セリフも Take の音声もなく、足す音もないため、無音の動画になります）。")
		writeSkipped(&sb, tl.Skipped)
		return sb.String()
	}
	var layers []string
	for _, l := range []struct {
		layer domain.AudioLayer
		label string
	}{{domain.LayerDialogue, "Dialogue"}, {domain.LayerNarration, "Narration"}, {domain.LayerBGM, "BGM"}, {domain.LayerAmbience, "Ambience"}, {domain.LayerSE, "SE"}, {domain.LayerFoley, "Foley"}} {
		if n := tl.Count(l.layer); n > 0 {
			layers = append(layers, fmt.Sprintf("%s %d", l.label, n))
		}
	}
	if n := len(tl.LipSync); n > 0 {
		layers = append(layers, fmt.Sprintf("Lip Sync %d", n))
	}
	if len(layers) == 0 {
		layers = []string{"Take の音声のみ"}
	}
	fmt.Fprintf(&sb, "（%s、ラウドネス %.1f LUFS", strings.Join(layers, " / "), tl.MeasuredLUFS)
	if tl.EstimatedCost > 0 {
		fmt.Fprintf(&sb, "、見積コスト $%.2f", tl.EstimatedCost)
	}
	sb.WriteString("）。")
	writeSkipped(&sb, tl.Skipped)
	return sb.String()
}

// writeSkipped lists the layers the Audio Director could not make and why (spec/api/endpoints.md);
// the layers the Quality Policy chose not to pay for are not failures and stay out.
func writeSkipped(sb *strings.Builder, skipped []domain.SkippedAudio) {
	var notes []string
	for _, sk := range skipped {
		if sk.Reason != domain.SkipPolicy {
			notes = append(notes, skippedText(sk))
		}
	}
	if len(notes) > 0 {
		sb.WriteString("\n作れなかった音:\n・" + strings.Join(notes, "\n・"))
	}
}

func skippedText(sk domain.SkippedAudio) string {
	what := strings.ToUpper(string(sk.Layer))
	if sk.Layer == domain.LayerLipSync {
		what = "Lip Sync"
	}
	if sk.Prompt != "" {
		what += "「" + sk.Prompt + "」"
	}
	reason := map[string]string{domain.SkipBudget: "予算上限", domain.SkipError: "生成に失敗"}[sk.Reason]
	if sk.Detail != "" {
		return fmt.Sprintf("%s（%s: %s）", what, reason, sk.Detail)
	}
	return fmt.Sprintf("%s（%s）", what, reason)
}
