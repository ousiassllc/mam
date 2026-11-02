// Package tierform decides the tier of the image form of the generation test (Issue #977): which of かんたん / こだわり / ComfyUI the form is in,
// the model the easy tier's choice of what to make picks, the quality buttons that model offers, and the values the detail tier takes over from
// the easy one. The handlers of web/handler/gentest use it for the page, the form that is swapped and the estimate; it is a package of its own
// because the handler directory is at its line limit.
package tierform

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/intent"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/advform"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentesttier"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// State is what the form says of the tiers. Tier is "" when it says none (the default is the easy tier). Intent and Quality merge the easy tier's
// own fields with the ones the detail tier carries; FromEasy says the quality came from the easy tier's buttons just now (the switch to the
// detail tier), not from what the detail tier carried.
type State struct {
	Tier, Intent, Quality string
	FromEasy              bool
}

// Read reads the tier fields of a form.
func Read(v url.Values) State {
	st := State{Tier: v.Get(gentesttier.FieldTier), Intent: v.Get(gentesttier.FieldIntent), Quality: v.Get(gentesttier.FieldQuality)}
	switch st.Tier {
	case gentesttier.TierEasy, gentesttier.TierDetail, gentesttier.TierComfy:
	default:
		st.Tier = ""
	}
	st.FromEasy = st.Quality != ""
	if st.Intent == "" {
		st.Intent = v.Get(gentesttier.FieldKeepIntent)
	}
	if st.Quality == "" {
		st.Quality = v.Get(gentesttier.FieldKeepQuality)
	}
	return st
}

// IntentID is the intent of the form: the one named when it is one of the choices, else the first.
func (s State) IntentID() string {
	if _, ok := intent.Get(s.Intent); ok {
		return s.Intent
	}
	return intent.All()[0].ID
}

// In is what Apply decides the tier from.
type In struct {
	State
	Media     string
	Refs      int      // how many input images the form holds
	Models    []string // the models that are ticked
	Aspect    string
	Prompts   []gentesttier.Prompt
	Translate bool
	Count     string
	MaxPrompt int
	// Adv are the detailed settings of the form: the detail tier that follows the easy one gets the quality's steps and cfg put in.
	Adv *advform.Fields
}

// Out is the tier of the form with the models it ticks: the one the easy tier picked, else the ones the form named.
type Out struct {
	Data   gentesttier.Data
	Models []string
}

// Apply decides the tier of the form. A video form has none. The easy tier (the default) ticks the model its intent picks; the detail tier keeps
// the models the form named and takes the quality's steps and cfg over; both keep the easy tier's choices so that the way back finds them.
func Apply(ctx context.Context, svc *gentest.Service, in In) Out {
	out := Out{Models: in.Models}
	if in.Media == gentest.MediaVideo {
		return out
	}
	d := gentesttier.Data{Tier: in.Tier, KeepIntent: in.Intent, KeepQuality: in.Quality}
	switch in.Tier {
	case gentesttier.TierComfy:
		out.Data = d
		return out
	case gentesttier.TierDetail:
		d.Carried = Carry(in, svc.Models(ctx, gentest.MediaImage))
	default:
		d.Tier = gentesttier.TierEasy
		d.Easy, out.Models = easy(ctx, svc, in)
	}
	out.Data = d
	return out
}

// Resolve gives the request of an easy-tier form the model its intent picks (and the role face to the inputs of the same-face intent); a form of
// another tier is returned as it is.
func Resolve(ctx context.Context, svc *gentest.Service, st State, req gentest.Request) (gentest.Request, error) {
	if st.Tier != gentesttier.TierEasy {
		return req, nil
	}
	req.Models = nil // the model is the intent's to choose; the hidden card of the form only shows it
	req, _, err := svc.ApplyIntent(ctx, req, st.IntentID())
	return req, err
}

// Pick is the part of the easy tier that follows the choice (the model, the hint and the quality buttons) and the model it picked.
func Pick(ctx context.Context, svc *gentest.Service, st State, refs int) (gentesttier.EasyData, []string) {
	return easy(ctx, svc, In{State: st, Refs: refs})
}

func easy(ctx context.Context, svc *gentest.Service, in In) (gentesttier.EasyData, []string) {
	id := in.IntentID()
	e := gentesttier.EasyData{
		Prompts: in.Prompts, Translate: in.Translate, Count: in.Count, MaxPrompt: in.MaxPrompt, Sizes: sizes(in.Aspect), ModelsURL: paths.ModelManager(),
	}
	for _, i := range intent.All() {
		e.Intents = append(e.Intents, gentesttier.IntentChoice{ID: i.ID, Label: i.Label, Hint: i.Hint, Selected: i.ID == id})
	}
	rec, err := svc.Recommend(ctx, id, in.Refs > 0)
	if err != nil {
		e.Reason = err.Error()
		return e, nil
	}
	if rec.Intent.NeedsReferences && in.Refs == 0 {
		e.IntentHint = "顔の写真を入力画像に入れてください。入れた画像は「顔だけ」の参照として使います"
	}
	e.ModelNote, e.Reason = rec.Note, rec.Reason
	if !rec.Picked() {
		e.Qualities, e.QualityReason = nil, "モデルが決まると、品質を選べます"
		return e, nil
	}
	e.ModelLabel, e.CanEstimate = rec.Model.Label, true
	e.Qualities, e.QualityReason = qualities(rec.Model, in.Quality)
	return e, []string{rec.Model.ID}
}

// qualities are the three buttons of a model (the chosen one lit; standard when the form names none or one the model does not offer), or why there
// are none.
func qualities(m modelcatalog.Info, chosen string) ([]gentesttier.QualityChoice, string) {
	caps := m.Sampling
	switch {
	case caps.None():
		return nil, "このモデルはステップ数・CFG を受け取らないため、品質は選べません（モデルの既定で作ります）"
	case caps.Steps != nil && caps.Steps.Locked != "":
		return nil, "このモデルは速さのモードがステップ数・CFG を決めるため、品質は選べません"
	case caps.Presets.None():
		return nil, "このモデルには品質のプリセットがないため、品質は選べません（標準の値で作ります）"
	}
	q, err := gentest.ParseQuality(chosen)
	if err != nil || !caps.Presets.Offers(q) {
		q = gentest.QualityStandard
	}
	var out []gentesttier.QualityChoice
	for _, c := range gentest.Qualities() {
		out = append(out, gentesttier.QualityChoice{ID: string(c), Label: c.Label(), Selected: c == q})
	}
	return out, ""
}

// sizes are the buttons 縦 / 横 / 正方形 (the aspect ratios the image models take: domain.BriefAspectRatios); an aspect ratio the form holds that
// is none of them is kept as a fourth button, so that switching tiers does not change the size.
func sizes(aspect string) []gentesttier.SizeChoice {
	if aspect == "" {
		aspect = "1:1"
	}
	out := []gentesttier.SizeChoice{{Value: "9:16", Label: "縦"}, {Value: "16:9", Label: "横"}, {Value: "1:1", Label: "正方形"}}
	found := false
	for i := range out {
		out[i].Selected = out[i].Value == aspect
		found = found || out[i].Selected
	}
	if !found {
		out = append(out, gentesttier.SizeChoice{Value: aspect, Label: aspect, Selected: true})
	}
	return out
}

// Carry puts the steps and cfg of the quality the easy tier had into the detail tier's fields (the ones the form does not already hold), from the
// first ticked model that has presets, and says so in a line; "" when nothing was carried.
func Carry(in In, models []modelcatalog.Info) string {
	if !in.FromEasy || in.Adv == nil {
		return ""
	}
	q, err := gentest.ParseQuality(in.Quality)
	if err != nil || q == gentest.QualityStandard {
		return ""
	}
	for _, m := range models {
		if !contains(in.Models, m.ID) || m.Sampling.None() || m.Sampling.Presets.None() || (m.Sampling.Steps != nil && m.Sampling.Steps.Locked != "") {
			continue
		}
		p := m.Sampling.Presets.For(q)
		var what []string
		if p.Steps != nil && in.Adv.Steps == "" {
			in.Adv.Steps = strconv.Itoa(*p.Steps)
			what = append(what, fmt.Sprintf("ステップ数 %d", *p.Steps))
		}
		if p.CFG != nil && in.Adv.CFG == "" {
			in.Adv.CFG = strconv.FormatFloat(*p.CFG, 'f', -1, 64)
			what = append(what, "CFG "+in.Adv.CFG)
		}
		if len(what) > 0 {
			return fmt.Sprintf("かんたんで選んだ「%s」（%s・%s）を入れました。変えた項目には印が付き、元に戻せます", q.Label(), m.Label, strings.Join(what, "・"))
		}
		return ""
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
