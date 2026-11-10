// Package credentials is the "API キー" section of the "API キーとログイン" category of the settings screen
// (spec/components/settings-screen-categories.md §1.1–1.2): one card for each provider, and the modal that sets its key.
// It shows whether a key is set and never the key.
package credentials

import (
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/settingsui"
)

// ProviderView is one card: a provider, what its key is for and whether it is set. It never carries the key.
type ProviderView struct {
	Provider   string
	Label      string
	Purpose    string // what the key is used for, in one line
	Configured bool
	// CanTest is whether the provider has a free, read-only call that the modal's "接続テスト" can make with the key.
	CanTest bool
	// KeyURL is the page of the provider where a key is created (an official page), "" when none is known.
	KeyURL string
}

// Data is the API keys on the settings screen: which provider has a key. The value of a key is never part of it.
// Unavailable means the OS key store could not be read (no Secret Service, locked, ...): the panel then says so and
// cannot set a key. Notice is what succeeded although the key store cannot be read afterwards (a key that was
// saved, so that it is not taken for a failure).
type Data struct {
	Providers   []ProviderView
	Unavailable bool
	Notice      string
	// Error is why the last change of the API provider was refused (role="alert").
	Error string
	// Provider is the choice of the API provider of the cloud video models at the top of the panel (spec/architecture/model-adapters.md §9.6);
	// nil leaves it out (the key store cannot be read, or the app has no model settings).
	Provider *ProviderData
}

// ProviderChoice is one radio of the choice of the API provider.
type ProviderChoice struct {
	Value, Label string
	Checked      bool
}

// ProviderModel is how the chosen provider serves one video model (Kling 3.0, Kling 4.0): whether it does and at what price, why not,
// and whether the key it needs is stored.
type ProviderModel struct {
	Label     string
	Supported bool
	// Prices is the unit price by resolution ("720p $0.084/秒 ・ 1080p $0.112/秒"); empty when it is not served.
	Prices string
	// Why is why the provider does not serve the model, and where it can be used.
	Why string
	// KeyLabel is the provider whose key the model needs now ("OpenRouter"); KeySet whether it is stored. Both empty for the official provider (the keys of the cards below).
	KeyLabel string
	KeySet   bool
}

// ProviderData is the choice of the API provider and how it serves the models.
type ProviderData struct {
	Choices []ProviderChoice
	// Pinned: the settings file was fixed by a start-up option; the choice is shown and cannot be changed.
	Pinned bool
	// Problem is a settings file that cannot be read: the choice is not offered.
	Problem string
	Models  []ProviderModel
}

// ProviderID is the id of the choice of the API provider: the form the answer of a change replaces with the panel.
const ProviderID = "api-provider"

// Form is what the modal of a provider is drawn from: the provider and, after a refusal, why. The key that was typed is
// not part of it, so that a refusal never writes a key back into the page.
type Form struct {
	Provider ProviderView
	// Error is why the last save was refused (422, 503); the field it names carries aria-invalid.
	Error string
	// Unavailable is a key store that cannot be read: nothing can be saved.
	Unavailable bool
}

// RowID is the id of the card of a provider: what a save replaces out of band.
func RowID(provider string) string { return "credential-row-" + provider }

// FocusKey names the button that opens the modal of a provider (it stays the same when "設定" becomes "変更").
func FocusKey(provider string) string { return "credential-" + provider }

// ListID is the id of the list of cards.
const ListID = "credential-list"

func (p ProviderView) card(oob, unavailable bool) settingsui.Card {
	c := settingsui.Card{
		ID: RowID(p.Provider), TestID: "credential-row", Attrs: map[string]any{"data-provider": p.Provider},
		Title: p.Label, Description: p.Purpose, OOB: oob,
	}
	if unavailable {
		c.Tone, c.State = atoms.BadgeWarn, "状態を確認できません"
	} else if p.Configured {
		c.Tone, c.State = atoms.BadgeOK, "設定済み"
	} else {
		c.Tone, c.State = atoms.BadgeNeutral, "未設定"
	}
	return c
}
