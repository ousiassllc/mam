package assign

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// Deps is what the operation needs of the service layer: the same modelsettings.Service the settings screen and the MCP tool set_model_assignment use. Save writes the whole
// assignment (modelsettings.Service.Save); the handler builds it from the saved View, so that only the named route changes.
type Deps struct {
	View func() (modelsettings.View, error)
	Save func(modelsettings.Input) error
}

// Handler is the chatops.Handler of OpSetModelAssignment.
type Handler struct{ deps Deps }

// New returns the handler of OpSetModelAssignment.
func New(d Deps) *Handler { return &Handler{deps: d} }

const (
	pinnedReason = "起動オプション（--model-config）で設定ファイルが固定されているため、変更できません"
	appliedNow   = "保存するとすぐに反映されます（再起動は要りません）。"
	globalNote   = "この設定は全プロジェクトに効きます（アプリ全体の設定です）。"
)

// maxChoices is how many models a card offers as buttons (as chatops/install does); a name that fits more is asked to be narrowed down.
const maxChoices = 6

// Question shows the route before and after and warns about a model that cannot be used now; nothing is saved. A model name that fits several models is asked as buttons.
func (h *Handler) Question(_ context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Card, error) {
	p, a, err := h.plan(op)
	var amb *ambiguousError
	switch {
	case errors.As(err, &amb):
		return h.which(a, amb)
	case err != nil:
		return chatops.Card{}, err
	case p.unchanged:
		return chatops.Card{}, chatops.ErrNoChange
	}
	chosen, err := commandinterpreter.NewOperation(OpSetModelAssignment, p.args)
	if err != nil {
		return chatops.Card{}, err
	}
	text := p.title + "を変えますね？\n・いま: " + p.before + "\n・変更後: " + p.after + "\n"
	if p.warning != "" {
		text += "・注意: " + p.warning + "（割り当ては保存できます。生成は、使えるようになるまで別のモデルに替わるか、断られます）\n"
	}
	for _, n := range p.notes {
		text += "・" + n + "\n"
	}
	return chatops.Card{Text: text + "・" + appliedNow + "\n・" + globalNote, Op: &chosen}, nil
}

// which is the card of a model name that fits several models: one button per model, each carrying the operation with that model.
func (h *Handler) which(a Args, amb *ambiguousError) (chatops.Card, error) {
	card := chatops.Card{Text: "「" + amb.Text + "」に当てはまるモデルが複数あります。どれにしますか？（全プロジェクトに効く設定です。選ぶまで何も変えません）"}
	for _, name := range amb.Names {
		pick := a
		pick.Model = name
		op, err := commandinterpreter.NewOperation(OpSetModelAssignment, pick)
		if err != nil {
			return chatops.Card{}, err
		}
		card.Choices = append(card.Choices, chatops.Choice{Label: name, Op: op})
	}
	return card, nil
}

// Apply reads the saved assignment again, changes the named route only and saves the whole assignment, as the MCP tool does.
func (h *Handler) Apply(_ context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Result, error) {
	p, _, err := h.plan(op)
	var amb *ambiguousError
	switch {
	case errors.As(err, &amb):
		return chatops.Result{}, chatops.Refuse("「%s」に当てはまるモデルが複数あります。モデル名をはっきり言ってもう一度送ってください", amb.Text)
	case err != nil:
		return chatops.Result{}, err
	case p.unchanged:
		return chatops.Result{Text: "すでにその内容です。変更しませんでした。"}, nil
	}
	if err := h.deps.Save(p.input); err != nil {
		return chatops.Result{}, refusalOf(err)
	}
	text := p.title + "を変えました（" + p.after + "）。すぐに反映されます。" + globalNote
	if p.warning != "" {
		text += "\n注意: " + p.warning
	}
	return chatops.Result{Text: text}, nil
}

// plan reads the operation and the saved assignment, and works out the change. a is the operation's arguments.
func (h *Handler) plan(op commandinterpreter.Operation) (plan, Args, error) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return plan{}, a, err
	}
	v, err := h.deps.View()
	if err != nil {
		return plan{}, a, refusalOf(err)
	}
	if v.Pinned {
		return plan{}, a, chatops.Refuse("%s", pinnedReason)
	}
	p, err := planOf(v, a)
	return p, a, err
}

// refusalOf is the reason a user can act on in an error of the model settings; any other error is returned as it is.
func refusalOf(err error) error {
	var invalid *modelsettings.InvalidError
	switch {
	case errors.As(err, &invalid):
		return &chatops.Refusal{Message: invalid.Message}
	case errors.Is(err, modelsettings.ErrPinned):
		return &chatops.Refusal{Message: pinnedReason}
	}
	return err
}
