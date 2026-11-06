package settingspage

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	updatehandler "github.com/ousiassllc/mam/backend/internal/web/handler/appupdate"
	mmoverview "github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/overview"
	audiohandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/audio"
	llmhandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/llm"
	localhandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/local"
	mcphandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/mcp"
	mediahandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/media"
	"github.com/ousiassllc/mam/backend/internal/web/handler/toolstatus"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	audioview "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/audio"
	llmview "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llm"
	localview "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/local"
	mcpview "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/mcp"
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
	cards = append(cards, localview.ManagerCard())
	if s.FFmpeg != nil || s.FaceFusion != nil {
		cards = append(cards, s.companionCard())
	}
	return pages.SettingsOverviewBody(overview.Data{Cards: cards, Guide: s.Guide})
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
		card.Status = llmStatus(llmhandler.SectionData(s.LLM, nil))
	case paths.SettingsModels:
		card.Icon, card.Description = atoms.IconMedia, "画像の用途・動画の Quality Policy ごとに、使うモデルを選ぶ。"
		card.Status = modelsStatus(mediahandler.SectionData(s.Models))
	case paths.SettingsLocalModels:
		card.Icon, card.Description = atoms.IconLocal, "自分の PC や tailnet 上の ComfyUI・OpenAI 互換のサーバーを登録する。"
		card.Status, card.StatusURL = localStatus(localhandler.SectionData(s.LocalModels))
	case paths.SettingsAudio:
		card.Icon, card.Description = atoms.IconMedia, "声と口パクを、クラウドで作るか、この PC のローカルモデルで作るかを選ぶ。"
		card.Status = audioStatus(audiohandler.SectionData(c.Request.Context(), s.Audio))
	case paths.SettingsMCP:
		card.Icon, card.Description = atoms.IconPlug, "エージェントから画面と同じ操作をするための、待ち受け先とトークン。"
		card.Status = mcpStatus(mcphandler.SectionData(c.Request.Context(), s.MCP))
	case paths.SettingsUpdate:
		card.Icon, card.Description = atoms.IconUpdate, "いまの版と、新しい版の確認。"
		card.Status = updateStatus(updatehandler.SectionData(s.Update))
	case paths.SettingsLicenses:
		card.Icon, card.Description = atoms.IconPackage, "MAM が使っているソフトウェアのライセンス文。"
		card.Status = []molecules.StatusLine{line(atoms.BadgeNeutral, "%d 件", s.catalog().Len())}
	}
	return card
}

// companionCard is the card of the companion software (ffmpeg, FaceFusion): another screen, the model manager, where they are installed and set up (#1106). Its state
// is the one of the card of the overview of the model manager.
func (s Services) companionCard() molecules.ToolCard {
	text, state := mmoverview.Companion(s.FFmpeg, s.FaceFusion)
	tone := atoms.BadgeNeutral
	switch state {
	case "full":
		tone = atoms.BadgeOK
	case "attention":
		tone = atoms.BadgeWarn
	}
	return molecules.ToolCard{
		ID: "companion", Href: templ.SafeURL(paths.ModelManagerCategory(paths.MMCompanion)), Icon: atoms.IconPackage, Title: "付随ソフト",
		Description: "ffmpeg・FaceFusion など、別に入れるソフトの入れ方と場所。モデルマネージャーの画面を開きます。",
		Status:      []molecules.StatusLine{{Tone: tone, Text: text, Attrs: templ.Attributes{"data-state": state}}},
	}
}

// keysStatus is the API keys and the ChatGPT sign-in, as the card of the overview says them.
func (s Services) keysStatus(c *gin.Context) []molecules.StatusLine {
	lines, err := toolstatus.Services{Credentials: s.Keys, ChatGPT: s.ChatGPT}.Settings(c.Request.Context())
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

// audioStatus is who speaks and who redoes the mouths, and whether the fallback to the cloud is on.
func audioStatus(d audioview.Data) []molecules.StatusLine {
	if d.Problem != "" {
		return []molecules.StatusLine{line(atoms.BadgeWarn, "%s", problemHead(d.Problem))}
	}
	out := []molecules.StatusLine{
		line(atoms.BadgeNeutral, "声: %s", providerName(d.Voice)),
		line(atoms.BadgeNeutral, "口パク: %s", providerName(d.LipSync)),
	}
	if d.Fallback {
		out = append(out, line(atoms.BadgeInfo, "ローカルが使えないときはクラウド"))
	}
	return out
}

// providerName is the provider a card has chosen, as the overview says it: a saved local provider this PC cannot run is not the one used.
func providerName(p audioview.Provider) string {
	for _, which := range []string{audioview.WhichLocal, audioview.WhichIrodori} {
		if l := p.As(which); l.ChosenLocal() {
			return l.Local.Name + "（この PC）"
		}
	}
	return p.CloudLabel
}

// problemHead is the first sentence of a problem: the card has room for one line.
func problemHead(problem string) string {
	if head, _, ok := strings.Cut(problem, "。"); ok {
		return head
	}
	return problem
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
