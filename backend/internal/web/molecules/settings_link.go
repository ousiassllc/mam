package molecules

import "github.com/ousiassllc/mam/backend/internal/web/paths"

// globalSettingsLabels are the categories of the global settings screen a chat notice (ref_type=global_settings) can
// point at, by the slug that its ref_id holds, with what the link says.
var globalSettingsLabels = map[string]string{
	paths.SettingsAPIKeys:     "API キーの設定を開く",
	paths.SettingsLLM:         "テキスト生成の設定を開く",
	paths.SettingsModels:      "画像・動画モデルの設定を開く",
	paths.SettingsLocalModels: "ローカル・代替モデルの設定を開く",
	paths.SettingsMCP:         "MCP サーバーの設定を開く",
	paths.SettingsFFmpeg:      "ffmpeg の設定を開く",
	paths.SettingsUpdate:      "バージョンとアップデートを開く",
}

// globalSettingsTarget is the category the ref_id of a chat notice names and what its link says. An empty or unknown
// slug is the overview: the address is never made of what the message holds.
func globalSettingsTarget(refID string) (slug, label string) {
	if label, ok := globalSettingsLabels[refID]; ok {
		return refID, label
	}
	return paths.SettingsOverview, "設定画面を開く"
}
