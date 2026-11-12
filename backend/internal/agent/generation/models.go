package generation

import "github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"

// routes is what the models of a generation are chosen by: the running assignment judged against what can be used now (spec/architecture/model-adapters.md §3.1).
func (o *Orchestrator) routes() modelroute.Env { return modelroute.ForAdapter(o.adapter) }

// RouteReports is, for every image purpose and Quality Policy, the model a generation would run on if it started now, with its route and why (modelroute.Env.Report): the
// one list the settings screen and the MCP tools show, so the announcement of an automatic start can name the same models and routes (#1112). It reads state only.
func (o *Orchestrator) RouteReports() []modelroute.RouteReport { return o.routes().Report() }
