package llmroute

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/nlu"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

// Deps is what the operation needs of the service layer: the same llmassign.Service the settings screen and the MCP tool set_llm_assignment use, and the lists of the models of the
// providers (llmmodels.Service) for resolving a name and for the warning about a model that is in no list. List and Availability may be nil (no list service: a name is taken as it
// is, without a warning).
type Deps struct {
	View func() llmassign.View
	// Save writes the routes of the Agents in the map; an Agent that is missing keeps what is saved.
	Save         func(routes map[string]llmassign.Route) error
	List         func(ctx context.Context, provider string) (llmmodels.List, error)
	Availability func(provider string) (modelfilter.Availability, error)
}

// Handler is the chatops.Handler of OpSetLLMAssignment.
type Handler struct{ deps Deps }

// New returns the handler of OpSetLLMAssignment.
func New(d Deps) *Handler { return &Handler{deps: d} }

const (
	pinnedReason = "起動オプション（--llm-config / MAM_LLM_CONFIG）で設定ファイルが固定されているため、変更できません"
	globalNote   = "この設定は全プロジェクトに効きます（アプリ全体の設定です）。"
	liveNote     = "保存すると、次の LLM 呼び出しからすぐ使われます（再起動は要りません）。"
	restartNote  = "保存した割り当ては、次にアプリを起動したときから使われます。"
	maxChoices   = 5
)

var roleLabels = map[string]string{nlu.RolePrimary: "プライマリ", nlu.RoleFallback: "代替"}

// change is what the operation does to the saved assignment: the arguments with everything resolved (what the yes of the card carries), the Agent, its route before and after,
// and what to warn about.
type change struct {
	args     Args
	entry    llmassign.Entry
	after    llmassign.Route
	warnings []string
	live     bool
}

// ambiguity is a name that fits several: the card asks which.
type ambiguity struct {
	text    string
	choices []Args // the operation with each fit, one button each
	labels  []string
}

func (e *ambiguity) Error() string { return "the name " + e.text + " fits several" }

// Question shows the route of the Agent before and after, with the warnings, and says that it is global; nothing is saved. A name that fits several is asked as buttons.
func (h *Handler) Question(ctx context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Card, error) {
	c, err := h.resolve(ctx, op)
	var amb *ambiguity
	switch {
	case errors.As(err, &amb):
		return h.which(amb)
	case err != nil:
		return chatops.Card{}, err
	case c.after == c.entry.Route:
		return chatops.Card{}, chatops.ErrNoChange
	}
	chosen, err := commandinterpreter.NewOperation(OpSetLLMAssignment, c.args)
	if err != nil {
		return chatops.Card{}, err
	}
	text := c.entry.Label + " の" + roleLabels[c.args.Role] + "の LLM を変えますね？\n・いま: " + routeText(c.entry.Route) + "\n・変更後: " + routeText(c.after) + "\n"
	for _, w := range c.warnings {
		text += "・注意: " + w + "\n"
	}
	if c.entry.Unused {
		text += "・この Agent は今のコードでは使われません（割り当ては保存できます）。\n"
	}
	if c.live {
		text += "・" + liveNote + "\n"
	} else {
		text += "・" + restartNote + "\n"
	}
	return chatops.Card{Text: text + "・" + globalNote, Op: &chosen}, nil
}

func (h *Handler) which(amb *ambiguity) (chatops.Card, error) {
	card := chatops.Card{Text: "「" + amb.text + "」に当てはまるものが複数あります。どれにしますか？（全プロジェクトに効く設定です。選ぶまで何も変えません）"}
	for i, a := range amb.choices {
		op, err := commandinterpreter.NewOperation(OpSetLLMAssignment, a)
		if err != nil {
			return chatops.Card{}, err
		}
		card.Choices = append(card.Choices, chatops.Choice{Label: amb.labels[i], Op: op})
	}
	return card, nil
}

// Apply saves the route of the Agent (the other half of it as it is saved now), as the MCP tool does.
func (h *Handler) Apply(ctx context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Result, error) {
	c, err := h.resolve(ctx, op)
	var amb *ambiguity
	switch {
	case errors.As(err, &amb):
		return chatops.Result{}, chatops.Refuse("「%s」に当てはまるものが複数あります。はっきり言ってもう一度送ってください", amb.text)
	case err != nil:
		return chatops.Result{}, err
	case c.after == c.entry.Route:
		return chatops.Result{Text: "すでにその内容です。変更しませんでした。"}, nil
	}
	if err := h.deps.Save(map[string]llmassign.Route{c.entry.Agent: c.after}); err != nil {
		return chatops.Result{}, refusalOf(err)
	}
	text := c.entry.Label + " の" + roleLabels[c.args.Role] + "を " + targetText(c.target()) + " にしました。"
	if c.live {
		text += "次の LLM 呼び出しからすぐ使われます。"
	} else {
		text += "次にアプリを起動したときから使われます。"
	}
	text += globalNote
	for _, w := range c.warnings {
		text += "\n注意: " + w
	}
	if pending := h.deps.View().Pending; len(pending) > 0 {
		text += "\n再起動するまで効かない設定があります: " + strings.Join(pending, "、")
	}
	return chatops.Result{Text: text}, nil
}

func (c change) target() llmassign.Target {
	if c.args.Role == nlu.RoleFallback {
		return c.after.Fallback
	}
	return c.after.Primary
}

// resolve reads the operation and the saved assignment and works out the change.
func (h *Handler) resolve(ctx context.Context, op commandinterpreter.Operation) (change, error) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return change{}, err
	}
	a.Role = nlu.RoleOf(a.Role)
	v := h.deps.View()
	switch {
	case v.Pinned:
		return change{}, chatops.Refuse("%s", pinnedReason)
	case v.Problem != "":
		return change{}, chatops.Refuse("LLM の設定ファイルを読み込めないため、割り当てを変えられません（%s）", v.Problem)
	}
	entry, err := agentOf(v, a)
	if err != nil {
		return change{}, err
	}
	a.Agent = entry.Agent
	cur := entry.Route.Primary
	if a.Role == nlu.RoleFallback {
		cur = entry.Route.Fallback
	}
	provider, err := providerFor(v, a, cur)
	if err != nil {
		return change{}, err
	}
	if strings.TrimSpace(a.Model) == "" {
		return change{}, chatops.Refuse("どのモデルにしますか？「%s の LLM を Claude Opus にして」のように、モデル名を添えて送ってください", entry.Label)
	}
	m, err := h.modelFor(ctx, provider, a)
	if err != nil {
		return change{}, err
	}
	a.Provider, a.Model = provider, m.id
	after := entry.Route
	if a.Role == nlu.RoleFallback {
		after.Fallback = llmassign.Target{Provider: provider, Model: m.id}
	} else {
		after.Primary = llmassign.Target{Provider: provider, Model: m.id}
	}
	c := change{args: a, entry: entry, after: after, live: v.Live}
	if after != entry.Route {
		c.warnings = h.warningsFor(provider, m)
	}
	return c, nil
}

// agentOf is the Agent the operation names, among the Agents of the assignment.
func agentOf(v llmassign.View, a Args) (llmassign.Entry, error) {
	found := pickAgents(v.Entries, a.Agent)
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		labels := make([]string, 0, len(v.Entries))
		for _, e := range v.Entries {
			labels = append(labels, e.Label)
		}
		return llmassign.Entry{}, chatops.Refuse("どの Agent の LLM ですか？ 選べる Agent: %s", nlu.Few(labels, len(labels)))
	}
	amb := &ambiguity{text: a.Agent}
	for _, e := range found {
		pick := a
		pick.Agent = e.Agent
		amb.choices, amb.labels = append(amb.choices, pick), append(amb.labels, e.Label)
	}
	return llmassign.Entry{}, amb
}

// providerFor is the provider of the new target: the one named, else the one the model's name stands for, else the one the route has now. A provider the assignment does not
// have is refused.
func providerFor(v llmassign.View, a Args, cur llmassign.Target) (string, error) {
	var provider string
	if id, ok := providerOf(a.Provider); ok {
		provider = id
	} else if id, ok := providerOf(a.Model); ok {
		provider = id
	} else if strings.TrimSpace(a.Provider) != "" {
		return "", chatops.Refuse("「%s」というプロバイダーは選べません。選べるプロバイダー: %s", a.Provider, strings.Join(v.Providers, "、"))
	}
	if provider == "" {
		provider = cur.Provider
	}
	if !slices.Contains(v.Providers, provider) {
		return "", chatops.Refuse("プロバイダー %s は選べません。選べるプロバイダー: %s", provider, strings.Join(v.Providers, "、"))
	}
	return provider, nil
}

func targetText(t llmassign.Target) string { return t.Provider + " / " + t.Model }

func routeText(r llmassign.Route) string {
	return "プライマリ " + targetText(r.Primary) + " / 代替 " + targetText(r.Fallback)
}

// refusalOf is the reason a user can act on in an error of the LLM assignment; any other error is returned as it is.
func refusalOf(err error) error {
	var invalid *llmassign.InvalidError
	switch {
	case errors.As(err, &invalid):
		return &chatops.Refusal{Message: invalid.Message}
	case errors.Is(err, llmassign.ErrPinned):
		return &chatops.Refusal{Message: pinnedReason}
	}
	return err
}
