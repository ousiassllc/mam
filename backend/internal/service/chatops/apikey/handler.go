package apikey

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// KeyDeps is what the key operation needs of the service layer: the same CredentialService the settings screen and the MCP tool set_credential use. Save reports an input
// it refuses (an empty key, one with a blank in it) as *chatops.Refusal.
type KeyDeps struct {
	Configured func(provider string) (bool, error)
	Save       func(provider, apiKey string) error
}

// Key is the Handler of OpSetCredential; it is also the chatops.SecretSubmitter of its card.
type Key struct{ deps KeyDeps }

// NewKey returns the handler of OpSetCredential.
func NewKey(d KeyDeps) *Key { return &Key{deps: d} }

// whichProvider is what the chat says when the message names no provider.
const whichProvider = "どのサービスの API キーですか？「OpenAI の API キーを設定したい」のように、サービス名を添えて送ってください。"

// Question is the card with the masked field. The key is stored when the user presses 保存 in the card (Submit), which is the confirmation: nothing is stored before.
func (k *Key) Question(_ context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Card, error) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Card{}, err
	}
	label, ok := labelOf(a.Provider)
	if !ok {
		return chatops.Card{}, chatops.Refuse("%s 対応しているサービス: %s", whichProvider, providerLabels())
	}
	configured, err := k.deps.Configured(a.Provider)
	if err != nil {
		return chatops.Card{}, err
	}
	verb := "登録します"
	if configured {
		verb = "置き換えます"
	}
	text := label + " の API キーを" + verb + "。下の入力欄にキーを貼り付けて「保存」を押してください。キーはチャットには書かないでください。\n" +
		"・キーは OS のセキュアストレージに保存され、チャットの履歴・ログには残りません。\n" +
		"・API キーはアプリ全体の設定です。全プロジェクトで使われます。"
	if configured {
		text += "\n・保存すると、いま保存されているキーが置き換わります。"
	}
	return chatops.Card{Text: text, Secret: &domain.SecretField{Provider: a.Provider, Label: label, MaxBytes: credential.MaxAPIKeyBytes}}, nil
}

// Apply refuses: a key is never part of an operation, so a card button that carried one could carry it into the history. The key goes through Submit only.
func (k *Key) Apply(context.Context, domain.Project, commandinterpreter.Operation) (chatops.Result, error) {
	return chatops.Result{}, chatops.Refuse("API キーはカードの入力欄から入力してください")
}

// Submit stores the key the user typed into the card. The key is passed to Save and nowhere else: not logged, not kept, not in the answer.
func (k *Key) Submit(_ context.Context, _ domain.Project, field domain.SecretField, secret string) (chatops.Result, error) {
	if err := k.deps.Save(field.Provider, secret); err != nil {
		return chatops.Result{}, err
	}
	return chatops.Result{Text: "「" + field.Label + "」の API キーを保存しました。再起動なしですぐ使われます。この設定は全プロジェクト共通です。"}, nil
}

func providerLabels() string {
	var out string
	for i, p := range providers {
		if i > 0 {
			out += "、"
		}
		out += p.Label
	}
	return out
}

// ProviderView is the saved API provider of the cloud video models.
type ProviderView struct {
	Selected string
	// Pinned: the settings file was fixed by a start option; it cannot be changed.
	Pinned bool
}

// ProviderDeps is what the provider operation needs: the same modelsettings.Service as the settings screen and the MCP tool set_video_provider. Save reports an input it
// refuses and a pinned file as *chatops.Refusal.
type ProviderDeps struct {
	View       func() (ProviderView, error)
	Save       func(provider string) error
	Configured func(provider string) (bool, error)
}

// Provider is the Handler of OpSetVideoProvider.
type Provider struct{ deps ProviderDeps }

// NewProvider returns the handler of OpSetVideoProvider.
func NewProvider(d ProviderDeps) *Provider { return &Provider{deps: d} }

// Question says what the provider changes and whether the key it needs is stored.
func (p *Provider) Question(_ context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Card, error) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Card{}, err
	}
	next := viaprovider.Provider(a.Provider)
	if !next.Valid() {
		return chatops.Card{}, chatops.Refuse("呼び出し先は 公式・OpenRouter・fal.ai・Replicate から選んでください。例:「Kling は OpenRouter 経由で使いたい」")
	}
	view, err := p.deps.View()
	if err != nil {
		return chatops.Card{}, err
	}
	if view.Pinned {
		return chatops.Card{}, chatops.Refuse("起動オプション（--model-config）で設定ファイルが固定されているため、変更できません")
	}
	cur := viaprovider.Provider(view.Selected).Selected()
	if cur == next.Selected() {
		return chatops.Card{}, chatops.ErrNoChange
	}
	text := "クラウド動画モデル（Kling）の呼び出し先を「" + next.Label() + "」に変えますね？（いまの設定: " + cur.Label() + "）\n" +
		"・次の見積りと生成から、選んだ呼び出し先の単価と提供状況で動きます。\n"
	if vendor := next.Vendor(); vendor != "" {
		set, err := p.deps.Configured(vendor)
		if err != nil {
			return chatops.Card{}, err
		}
		label, _ := labelOf(vendor)
		if set {
			text += "・" + label + " の API キーは設定済みです。\n"
		} else {
			text += "・" + label + " の API キーがまだありません。変えたあとに、キーを入力するカードを出します。\n"
		}
	}
	return chatops.Card{Text: text + "・この設定は全プロジェクトに効きます（アプリ全体の設定です）。"}, nil
}

// Apply saves the provider, and asks for the key it needs when none is stored.
func (p *Provider) Apply(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (chatops.Result, error) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Result{}, err
	}
	next := viaprovider.Provider(a.Provider)
	if !next.Valid() {
		return chatops.Result{}, chatops.Refuse("呼び出し先が正しくありません")
	}
	if err := p.deps.Save(string(next)); err != nil {
		return chatops.Result{}, err
	}
	res := chatops.Result{Text: "クラウド動画モデル（Kling）の呼び出し先を「" + next.Label() + "」にしました。次の見積りと生成から使われます。この設定は全プロジェクトに効きます。"}
	vendor := next.Vendor()
	if vendor == "" {
		return res, nil
	}
	set, err := p.deps.Configured(vendor)
	if err != nil || set {
		return res, err
	}
	ask, err := commandinterpreter.NewOperation(OpSetCredential, Args{Provider: vendor})
	if err != nil {
		return res, nil
	}
	card, err := (&Key{deps: KeyDeps{Configured: p.deps.Configured}}).Question(ctx, project, ask)
	if err != nil {
		return res, err
	}
	msg, err := chatops.Message(ask, card)
	if err != nil {
		return res, err
	}
	res.Messages = append(res.Messages, msg)
	return res, nil
}
