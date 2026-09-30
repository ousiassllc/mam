package organisms

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// prettyJSON indents raw for display; empty raw shows a dash and text that is not JSON is shown as is.
func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "—"
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}

func worldVersionText(v int) string {
	if v == 0 {
		return "なし（Shot 完了前、または記録前の Take）"
	}
	return "v" + strconv.Itoa(v)
}

func judgeTone(v domain.JudgeVerdict) atoms.BadgeTone {
	if v == domain.JudgePass {
		return atoms.BadgeOK
	}
	return atoms.BadgeError
}

func routingModeLabel(m domain.RoutingMode) string {
	switch m {
	case domain.RoutingAdaptive:
		return "実績で切り替え"
	case domain.RoutingPolicyDefault:
		return "Quality Policy のまま"
	case domain.RoutingFixed:
		return "ユーザーが固定"
	case domain.RoutingDisabled:
		return "ルーティング無効"
	}
	return string(m)
}

func routingTone(m domain.RoutingMode) atoms.BadgeTone {
	switch m {
	case domain.RoutingAdaptive:
		return atoms.BadgeInfo
	case domain.RoutingFixed:
		return atoms.BadgeWarn
	}
	return atoms.BadgeNeutral
}

func routingModelText(m domain.RoutingModel) string {
	if m.Resolution == "" {
		return string(m.Model)
	}
	return fmt.Sprintf("%s %s", m.Model, m.Resolution)
}

func routingCandidateName(c domain.RoutingCandidate) string {
	name := fmt.Sprintf("%s %s", c.Model, c.Resolution)
	if c.Anchor {
		name += "（Policy のプライマリ）"
	}
	return name
}

// routingRecordText is the Judge record of a candidate ("—" without one).
func routingRecordText(c domain.RoutingCandidate) string {
	if c.Samples == 0 {
		return "—"
	}
	return fmt.Sprintf("%d Take · %.0f 点 · %.0f%%", c.Samples, c.MeanScore, c.PassRate*100)
}

func routingExcludedLabel(reason string) string {
	switch reason {
	case "":
		return ""
	case domain.ExcludedCircuitOpen:
		return "サーキットブレーカー OPEN"
	case domain.ExcludedNoBackend:
		return "API キー未設定"
	case domain.ExcludedInsufficientEvidence:
		return "実績不足"
	case domain.ExcludedBelowMinimumScore:
		return "最低合格点に届かない"
	case domain.ExcludedMargin:
		return "差が小さい"
	case domain.ExcludedOutscored:
		return "他の候補が上位"
	}
	return reason
}

func routingAxisText(w domain.TakeScores) string {
	return fmt.Sprintf("Shot の性質による軸の重み: product %.2f · acting %.2f · camera %.2f · continuity %.2f", w.Product, w.Acting, w.Camera, w.Continuity)
}
