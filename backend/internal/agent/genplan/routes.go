package genplan

import (
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// RoutesOf is the Builder.Routes of a Generator that can say which models a generation would run on and why (*generation.Orchestrator.RouteReports), nil for one that
// cannot: its plan names no routes.
func RoutesOf(gen any) func() []modelroute.RouteReport {
	if r, ok := gen.(interface {
		RouteReports() []modelroute.RouteReport
	}); ok {
		return r.RouteReports
	}
	return nil
}

// relevantRoutes keeps the routes the plan runs on: the image purposes of the Reference Pack when a Shot gets one, and the video route of the Quality Policy.
// They are the entries of modelroute.Env.Report, so the chat, the settings screen and the MCP tools say the same of a model (#1112).
func relevantRoutes(all []modelroute.RouteReport, q domain.QualityPolicy, lines []Line) []modelroute.RouteReport {
	if len(lines) == 0 {
		return nil
	}
	pack := false
	for _, l := range lines {
		pack = pack || l.Pack
	}
	var out []modelroute.RouteReport
	for _, r := range all {
		switch {
		case r.Media == "image" && pack && (r.Key == string(modeladapter.PurposeNewReference) || r.Key == string(modeladapter.PurposeEditReference)):
			out = append(out, r)
		case r.Media == "video" && r.Key == string(q):
			out = append(out, r)
		}
	}
	return out
}

// routesText is the models the plan is expected to run on under the current assignment (a route is reported per image purpose and Quality Policy, not per Shot input, so it is
// told as an expectation, never as the model a Shot will use), each with its route (API / ChatGPT サブスク / ローカル), how its price is known, and, for a model that is not the
// assignment's, which model of the assignment was passed over and why. A model chosen in place of the assignment is said to match the conditions; it is never called
// the best one. "" when the plan carries no routes.
func (p Plan) routesText() string {
	var b strings.Builder
	for _, r := range p.Routes {
		if r.Use == nil {
			continue
		}
		role := "画像（用途: " + r.Key + "）"
		if r.Media == "video" {
			role = "動画（Quality Policy: " + r.Key + "）"
		}
		fmt.Fprintf(&b, "  - %s: %s（%s・%s）", role, r.Use.Model, r.Use.Route.Label(), r.Use.Basis.Label())
		if why := passedText(r); why != "" {
			b.WriteString("。" + why)
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return ""
	}
	return "・現在の割当で使う見込みのモデルと経路（Shot ごとの入力や実行時の状況で変わることがあります）:\n" + b.String()
}

// passedText says why the assignment's own model is not the one used ("" when it is).
func passedText(r modelroute.RouteReport) string {
	var parts []string
	for _, a := range r.Assigned {
		if !a.Available {
			parts = append(parts, a.Model+"＝"+a.UnavailableMessage)
		}
	}
	switch {
	case r.Status == modelroute.StatusCondition:
		return r.Reason + "（品質で選んだものではありません。使えない割り当て: " + strings.Join(parts, "、") + "）"
	case len(parts) > 0:
		return "割り当ての予備のモデルです（使えない割り当て: " + strings.Join(parts, "、") + "）"
	}
	return ""
}
