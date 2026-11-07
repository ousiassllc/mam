// Package llmlocal is the card "ローカル LLM サーバー" of the category "テキスト生成（LLM）" of the settings screen: a link to the page "ローカル LLM" of the model manager (#1104),
// where the server the calls of the provider "local" go to is chosen and its address and key are set, and one line that says where the calls go now. It has no field of its own:
// the settings of the local LLM server were moved to the model manager, so that there is one place for them.
package llmlocal

// Data is what the card is drawn from.
type Data struct {
	// State is where the calls go now ("今は この PC の llama.cpp に呼び出します"); Problem is in a few words why that server cannot be used ("未導入"), empty when it can.
	State, Problem string
}
