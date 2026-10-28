package onboarding

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	svc "github.com/ousiassllc/mam/backend/internal/service/onboarding"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	chatgpthandler "github.com/ousiassllc/mam/backend/internal/web/handler/chatgptsettings"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/onboarding"
)

// credentialsStatus says whether the credentials step has what it asks for: an API key, or a ChatGPT sign-in. Neither is
// a warning (generation cannot run), not an error: the step may be skipped. A key store that cannot be read is said so.
func (h Handlers) credentialsStatus(c *gin.Context) view.CredentialsStatus {
	var set []string
	statuses, err := h.Keys.Status()
	if err != nil {
		_ = c.Error(err)
		return view.CredentialsStatus{Tone: atoms.BadgeWarn, Text: "API キーの状態を確認できません（キーストアを利用できません）"}
	}
	keys := 0
	for _, s := range statuses {
		if s.Configured {
			keys++
		}
	}
	if keys > 0 {
		set = append(set, fmt.Sprintf("API キー %d 件", keys))
	}
	if h.ChatGPT != nil {
		d := chatgpthandler.SectionData(h.ChatGPT)
		if d.Official.SignedIn || d.Image.SignedIn || d.Codex.SignedIn {
			set = append(set, "ChatGPT ログイン済み")
		}
	}
	if len(set) == 0 {
		return view.CredentialsStatus{Tone: atoms.BadgeWarn, Text: "API キーも ChatGPT ログインもまだありません。どちらか 1 つを入れると生成が使えます。あとで設定することもできます"}
	}
	return view.CredentialsStatus{Tone: atoms.BadgeOK, Text: "設定済み: " + strings.Join(set, "・")}
}

// CredentialsStatus answers the state line of the credentials step alone (GET /onboarding/credentials/status): the step asks
// for it again when a modal closes.
func (h Handlers) CredentialsStatus(c *gin.Context) {
	settingshttp.NoStore(c)
	settingshttp.Render(c, http.StatusOK, view.CredentialsStatusLine(h.credentialsStatus(c)))
}

// CodexPath sets the codex the user installed, or clears it (PUT /onboarding/codex-path, field path): the macOS step. It is
// the setting of the screen's "自分で入れた codex のパス" (chatgpt.Service.Codex().SetCustomPath). Another system, or no
// ChatGPT sign-in, has no such step: 404.
func (h Handlers) CodexPath(c *gin.Context) {
	if h.ChatGPT == nil || h.Guide.OS() != svc.OSMac {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	path := c.PostForm("path")
	err := h.ChatGPT.Codex().SetCustomPath(path)
	switch {
	case err == nil && path == "":
		settingshttp.Render(c, http.StatusOK, view.CodexResult(true, "パスの指定を解除しました"))
	case err == nil:
		settingshttp.Render(c, http.StatusOK, view.CodexResult(true, "パスを設定しました。mam は実行して確かめません。ログインやモデルの取得で使われます"))
	default:
		settingshttp.Render(c, http.StatusUnprocessableEntity, view.CodexResult(false, "パスを設定できませんでした。実在するファイルの絶対パスを指定してください"))
	}
}
