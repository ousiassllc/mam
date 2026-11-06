package runtimechoices

import (
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// Item is the model as the filter reads it (modelfilter.Condition.Match; #1011): what it makes (tts is "音声", llm is "LLM"), the languages it speaks, its
// video memory, the license's answer on commercial use and the search words. It runs on this PC, so its origin and provider are local. Every model of a runtime can
// be chosen (a model whose weights are not here is fetched after the choice), so none is held back by "選べるものだけ"; the weights of the models that are not in
// force are not tracked (the runtime checks the weights of the model in force only), so the item does not say whether it is installed and the screens and the tool
// do not offer that part.
func (v View) Item() modelfilter.Item {
	kind := modelfilter.KindLLM
	if v.Kind == "tts" {
		kind = modelfilter.KindAudio
	}
	return modelfilter.Item{
		ID: v.ID, Name: v.Name, Kinds: []modelfilter.Kind{kind}, Languages: v.Languages, Adult: v.Uncensored, Uncensored: v.Uncensored,
		VRAMMinGB: v.VRAMMinGB, Commercial: v.LicenseInfo.Commercial, Installed: true, Origin: modelfilter.OriginLocal, Provider: "local",
		Tags: []string{v.Kind, v.LicenseInfo.Name}, Available: true,
	}
}

// Where is the views the condition lets through, in their order (an empty condition keeps them all).
func Where(views []View, c modelfilter.Condition) []View {
	return modelfilter.Filter(c, views, View.Item)
}
