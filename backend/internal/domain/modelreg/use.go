package modelreg

import "strings"

// RouteKind is how a model is reached: the route its calls go through and the way it is paid.
type RouteKind string

// The route kinds. Their values are what the screens and the MCP tools say (`route_kind`).
const (
	// RouteAPI is a vendor's API: paid by the call, with an API key.
	RouteAPI RouteKind = "api"
	// RouteSubscription is the ChatGPT subscription's image model (the plan pays; no price per call).
	RouteSubscription RouteKind = "chatgpt-subscription"
	// RouteLocal is a model on this PC or on an endpoint the user registered (ComfyUI, OpenAI-compatible): free of charge.
	RouteLocal RouteKind = "local"
)

// subscriptionSuffix ends the identifier of a model made with the ChatGPT subscription ("gpt-image-2.6-flare:chatgpt-subscription").
const subscriptionSuffix = ":chatgpt-subscription"

// RouteOf is the route kind of the model with this identifier, read from the identifier alone: "<name>:chatgpt-subscription" is the
// subscription, "comfyui:<endpoint>/<name>", "openai-compat:<endpoint>/<name>" and "comfy:<workflow>@v<N>" are local, any other is an API.
func RouteOf(id string) RouteKind {
	switch {
	case strings.HasSuffix(id, subscriptionSuffix):
		return RouteSubscription
	case strings.HasPrefix(id, WorkflowPrefix), strings.HasPrefix(id, "comfyui:"), strings.HasPrefix(id, "openai-compat:"):
		return RouteLocal
	}
	return RouteAPI
}

// Label is the word the screens show for the route.
func (k RouteKind) Label() string {
	switch k {
	case RouteSubscription:
		return "ChatGPT サブスク"
	case RouteLocal:
		return "ローカル"
	}
	return "API"
}

// Selection is why the model of a generation was the one used.
type Selection string

const (
	// SelectionAssignment is the model of the assignment: it is the assignment's (the Quality Policy's or the image purpose's primary / alternative).
	SelectionAssignment Selection = "assignment"
	// SelectionCondition is a model chosen because the assignment could not be used and it matches the conditions of the
	// request (it is available and fits the purpose, the input, the resolution and length, the age gate and the licence). It says
	// nothing about the quality of the model.
	SelectionCondition Selection = "condition_match"
)

// CostBasis is how the price of a model is known.
type CostBasis string

const (
	// CostPriced means the unit price is known (USD).
	CostPriced CostBasis = "priced"
	// CostFree means a model on this PC, free of charge (0 USD).
	CostFree CostBasis = "free"
	// CostUnknown means no unit price (the subscription's plan pays, or an API model without a price in the settings). It is not counted in
	// an estimate, and it is not the same as free.
	CostUnknown CostBasis = "unknown"
)

// Label is the word of the cost basis.
func (b CostBasis) Label() string {
	switch b {
	case CostFree:
		return "無料"
	case CostUnknown:
		return "単価不明"
	}
	return "従量課金"
}

// Use is the model a generation used, how it is reached, and why it was chosen. It is recorded with a Take (take.model_parameters.model_use)
// and shown on the Job, the Take and the MCP tools.
type Use struct {
	// Model is the model that made the output.
	Model     string    `json:"model"`
	Route     RouteKind `json:"route_kind"`
	Selection Selection `json:"selection"`
	// Reason is the selection in one sentence for the user.
	Reason string `json:"reason"`
	// Passed are the models of the assignment that were not used and why (empty for SelectionAssignment).
	Passed    []Passed  `json:"passed_models,omitempty"`
	CostBasis CostBasis `json:"cost_basis"`
}

// Passed is a model of the assignment that could not be used, and why.
type Passed struct {
	Model string `json:"model"`
	Why   string `json:"why"`
}

// Label is what a card says after the model: the route, the cost basis, and "条件に合うモデル" when the model was chosen in place of the assignment.
func (u Use) Label() string {
	label := u.Route.Label() + "・" + u.CostBasis.Label()
	if u.Selection == SelectionCondition {
		label += "・条件に合うモデルを自動選択"
	}
	return label
}

// Explain is Reason with the models of the assignment that were passed over and why: the sentence of the Generation Trace.
func (u *Use) Explain() string {
	if u == nil {
		return ""
	}
	text := u.Reason
	for _, p := range u.Passed {
		text += "／" + p.Model + "＝" + p.Why
	}
	return text
}

// Suffix is the label in parentheses after a model's name ("（ローカル・無料）"), "" for a model with no recorded use (the receiver may be nil).
func (u *Use) Suffix() string {
	if u == nil {
		return ""
	}
	return "（" + u.Label() + "）"
}
