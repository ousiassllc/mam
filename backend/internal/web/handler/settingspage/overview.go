package settingspage

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	updatehandler "github.com/ousiassllc/mam/backend/internal/web/handler/appupdate"
	localhandler "github.com/ousiassllc/mam/backend/internal/web/handler/localmodels"
	mcphandler "github.com/ousiassllc/mam/backend/internal/web/handler/mcpsettings"
	llmhandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/llm"
	mediahandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/media"
	"github.com/ousiassllc/mam/backend/internal/web/handler/toolstatus"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	localview "github.com/ousiassllc/mam/backend/internal/web/organisms/localmodels"
	mcpview "github.com/ousiassllc/mam/backend/internal/web/organisms/mcppanel"
	llmview "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llm"
	modelview "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/overview"
	updateview "github.com/ousiassllc/mam/backend/internal/web/organisms/updatepanel"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// overviewBody is the cards of the overview in the order of the navigation, then the model manager (another screen).
func overviewBody(c *gin.Context, s Services) templ.Component {
	var cards []molecules.ToolCard
	for _, cat := range categories {
		if cat.slug == paths.SettingsOverview || !cat.shown(s) {
			continue
		}
		cards = append(cards, s.card(c, cat.slug))
	}
	cards = append(cards, molecules.ToolCard{
		ID: "model-manager", Href: templ.SafeURL(paths.ModelManager()), Icon: atoms.IconPackage, Title: "モデルマネージャー",
		Description: "ComfyUI とローカルモデルの導入・試し生成。別の画面を開きます。", StatusURL: paths.ToolStatus(paths.ToolModelManager),
	})
	return pages.SettingsOverviewBody(overview.Data{Cards: cards})
}

// card is the card of a category: its icon, what it is for and the state the server knows (the one thing it cannot know
// at once, the connection of the local endpoints, is read after the page is shown).
func (s Services) card(c *gin.Context, slug string) molecules.ToolCard {
	card := molecules.ToolCard{ID: slug, Href: templ.SafeURL(paths.SettingsCategory(slug)), Title: find(slug).title}
	switch slug {
	case paths.SettingsAPIKeys:
		card.Icon, card.Description = atoms.IconKey, "外部モデルの API キーと、ChatGPT のログイン。"
		card.Status = s.keysStatus(c)
	case paths.SettingsLLM:
		card.Icon, card.Description = atoms.IconText, "Agent ごとに、使う LLM とその代替を選ぶ。"
		card.Status = llmStatus(llmhandler.SectionData(s.LLM))
	case paths.SettingsModels:
		card.Icon, card.Description = atoms.IconMedia, "画像の用途・動画の Quality Policy ごとに、使うモデルを選ぶ。"
		card.Status = modelsStatus(mediahandler.SectionData(s.Models))
	case paths.SettingsLocalModels:
		card.Icon, card.Description = atoms.IconLocal, "自分の PC や tailnet 上の ComfyUI・OpenAI 互換のサーバーを登録する。"
		card.Status, card.StatusURL = localStatus(localhandler.SectionData(s.LocalModels))
	case paths.SettingsMCP:
		card.Icon, card.Description = atoms.IconPlug, "エージェントから画面と同じ操作をするための、待ち受け先とトークン。"
		card.Status = mcpStatus(mcphandler.SectionData(c.Request.Context(), s.MCP))
	case paths.SettingsUpdate:
		card.Icon, card.Description = atoms.IconUpdate, "いまの版と、新しい版の確認。"
		card.Status = updateStatus(updatehandler.SectionData(s.Update))
	}
	return card
}

// keysStatus is the API keys and the ChatGPT sign-in, as the card of the home page says them.
func (s Services) keysStatus(c *gin.Context) []molecules.StatusLine {
	src := toolstatus.Services{Credentials: s.Keys, ChatGPT: s.ChatGPT}.Sources()[paths.ToolSettings]
	lines, err := src(c.Request.Context())
	if err != nil {
		return nil
	}
	return lines
}

func line(tone atoms.BadgeTone, format string, args ...any) molecules.StatusLine {
	return molecules.StatusLine{Tone: tone, Text: fmt.Sprintf(format, args...)}
}

// llmStatus is how many Agents run another LLM than the default.
func llmStatus(d llmview.Data) []molecules.StatusLine {
	rows := append(append([]llmview.Row{}, d.Rows...), d.Unused...)
	changed := 0
	for _, r := range rows {
		if r.Customized() {
			changed++
		}
	}
	switch {
	case d.Problem != "":
		return []molecules.StatusLine{line(atoms.BadgeWarn, "設定ファイルを読めません")}
	case d.Pinned:
		return []molecules.StatusLine{line(atoms.BadgeNeutral, "起動オプションで固定")}
	case changed == 0:
		return []molecules.StatusLine{line(atoms.BadgeNeutral, "既定のまま")}
	}
	return []molecules.StatusLine{line(atoms.BadgeInfo, "変更 %d / %d 件", changed, len(rows))}
}

// modelsStatus is whether the image / video models are the defaults.
func modelsStatus(d modelview.Data) []molecules.StatusLine {
	switch {
	case d.Problem != "":
		return []molecules.StatusLine{line(atoms.BadgeWarn, "設定ファイルを読めません")}
	case d.Pinned:
		return []molecules.StatusLine{line(atoms.BadgeNeutral, "起動オプションで固定")}
	case d.Customized:
		return []molecules.StatusLine{line(atoms.BadgeInfo, "変更あり")}
	}
	return []molecules.StatusLine{line(atoms.BadgeNeutral, "既定のまま")}
}

// localStatus is how many endpoints and models there are, and the URL that reads whether the endpoints answer (none
// without an endpoint).
func localStatus(d localview.Data) ([]molecules.StatusLine, string) {
	if d.Problem != "" {
		return []molecules.StatusLine{line(atoms.BadgeWarn, "設定ファイルを読めません")}, ""
	}
	if len(d.Endpoints) == 0 {
		return []molecules.StatusLine{line(atoms.BadgeNeutral, "接続先なし")}, ""
	}
	models := 0
	for _, e := range d.Endpoints {
		models += len(e.Models)
	}
	return []molecules.StatusLine{line(atoms.BadgeNeutral, "接続先 %d 件・モデル %d 件", len(d.Endpoints), models)}, paths.LocalModelsHealth()
}

// mcpStatus is where the server listens and how many tokens work.
func mcpStatus(d mcpview.Data) []molecules.StatusLine {
	var out []molecules.StatusLine
	switch {
	case d.Problem != "" && d.Addr == "":
		out = append(out, line(atoms.BadgeWarn, "待ち受けていません"))
	case d.Addr == "":
		out = append(out, line(atoms.BadgeNeutral, "待ち受けていません"))
	default:
		out = append(out, line(atoms.BadgeOK, "待ち受け: %s", d.Addr))
	}
	active := 0
	for _, t := range d.Tokens {
		if !t.Revoked {
			active++
		}
	}
	return append(out, line(atoms.BadgeNeutral, "有効なトークン %d 件", active))
}

// updateStatus is the version and the result of the last check.
func updateStatus(d updateview.Data) []molecules.StatusLine {
	out := []molecules.StatusLine{line(atoms.BadgeNeutral, "版: %s", d.Version)}
	if d.Status != "" {
		out = append(out, molecules.StatusLine{Tone: updateTone(d.Tone), Text: d.Status})
	}
	return out
}

func updateTone(t updateview.Tone) atoms.BadgeTone {
	switch t {
	case updateview.ToneOK:
		return atoms.BadgeOK
	case updateview.ToneWarn:
		return atoms.BadgeWarn
	case updateview.ToneError:
		return atoms.BadgeError
	}
	return atoms.BadgeNeutral
}
