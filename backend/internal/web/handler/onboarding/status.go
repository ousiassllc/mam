package onboarding

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	svc "github.com/ousiassllc/mam/backend/internal/service/onboarding"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	chatgpthandler "github.com/ousiassllc/mam/backend/internal/web/handler/chatgptsettings"
	credentialshandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/credentials"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/onboarding"
	chatgptview "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/chatgpt"
	keysview "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/credentials"
)

// readCredentials reads what the credentials step shows, once: the API keys (a key store that cannot be read leaves the
// cards without a state and the error in the log) and, when the sign-in is there, the ChatGPT sign-in. The key store can
// be slow, so the state line and the cards are drawn from the same read.
func (h Handlers) readCredentials(c *gin.Context) (keys keysview.Data, login *chatgptview.Data) {
	keys = credentialshandler.Data(c, credentialshandler.Keys{Svc: h.Keys, Tester: h.Tester})
	if h.ChatGPT != nil {
		d := chatgpthandler.SectionData(h.ChatGPT)
		login = &d
	}
	return keys, login
}

// credentialsStatus says whether the credentials step has what it asks for: an API key, or a ChatGPT sign-in. Neither is
// a warning (generation cannot run), not an error: the step may be skipped. A key store or a sign-in that cannot be read is
// said so, rather than taken for "none".
func credentialsStatus(keys keysview.Data, login *chatgptview.Data) view.CredentialsStatus {
	if keys.Unavailable {
		return view.CredentialsStatus{Tone: atoms.BadgeWarn, Text: "API キーの状態を確認できません（キーストアを利用できません）"}
	}
	var set []string
	n := 0
	for _, p := range keys.Providers {
		if p.Configured {
			n++
		}
	}
	if n > 0 {
		set = append(set, fmt.Sprintf("API キー %d 件", n))
	}
	if login != nil && (login.Official.SignedIn || login.Image.SignedIn || login.Codex.SignedIn) {
		set = append(set, "ChatGPT ログイン済み")
	}
	switch {
	case len(set) > 0:
		return view.CredentialsStatus{Tone: atoms.BadgeOK, Text: "設定済み: " + strings.Join(set, "・")}
	case login != nil && login.Error != "":
		return view.CredentialsStatus{Tone: atoms.BadgeWarn, Text: "ChatGPT ログインの状態を確認できません（キーストアを利用できません）"}
	}
	return view.CredentialsStatus{Tone: atoms.BadgeWarn, Text: "API キーも ChatGPT ログインもまだありません。どちらか 1 つを入れると生成が使えます。あとで設定することもできます"}
}

// CredentialsStatus answers the state line of the credentials step alone (GET /onboarding/credentials/status): the step asks
// for it again when a modal closes or an API key or a sign-in is removed.
func (h Handlers) CredentialsStatus(c *gin.Context) {
	settingshttp.NoStore(c)
	settingshttp.Render(c, http.StatusOK, view.CredentialsStatusLine(credentialsStatus(h.readCredentials(c))))
}

// CodexPath sets the codex the user installed, or clears it (PUT /onboarding/codex-path, field path): the macOS step. It is
// the setting of the screen's "自分で入れた codex のパス", with the same answers (chatgpthandler.SetCodexPath). Another
// system, or no ChatGPT sign-in, has no such step: 404.
func (h Handlers) CodexPath(c *gin.Context) {
	if h.ChatGPT == nil || h.Guide.OS() != svc.OSMac {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	r := chatgpthandler.SetCodexPath(c, h.ChatGPT, c.PostForm("path"))
	settingshttp.Render(c, r.Status, view.CodexResult(r.OK(), r.Message))
}
