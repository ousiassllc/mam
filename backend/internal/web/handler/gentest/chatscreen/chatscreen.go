// Package chatscreen holds the handlers of the chat of the single generation screen (paths.GenerationTestChat, #1138): the page, a message of the user, the
// buttons of a confirmation card and the results block. It belongs to the generation test: the batches are its batches, the files and the details are served by
// its handlers. The page is a thin layer over service/gentest/chat (a separate package of the handlers for the directory's line limit).
//
// The only route that starts a generation is the confirm button of a card. It takes the id of the card, nothing else: the request and the estimate it runs
// are the ones the service kept when it priced them (service/gentest/chat.Service.Confirm).
package chatscreen

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"

	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/chat"
)

// Handlers answer the routes of the screen.
type Handlers struct {
	Gentest *gentest.Service
	Chat    *chat.Service
}

// Register adds the routes of the screen to the engine. c is nil when the app has no LLM for the chat: the page is then a notice.
func Register(engine *gin.Engine, g *gentest.Service, c *chat.Service) {
	h := Handlers{Gentest: g, Chat: c}
	engine.GET(paths.GenerationTestChat(), h.Page)
	if c == nil { // no LLM for the chat: the page says so, and there is nothing to post to
		return
	}
	engine.POST(paths.GenerationTestChat("messages"), h.Send)
	engine.POST(paths.GenerationTestChat("proposals", ":id", "confirm"), h.Confirm)
	engine.POST(paths.GenerationTestChat("proposals", ":id", "dismiss"), h.Dismiss)
	engine.GET(paths.GenerationTestChat("batches", ":id"), h.Batch)
}

// Page answers GET /generation-test/chat with the page and a new conversation.
func (h Handlers) Page(c *gin.Context) {
	gtshare.Render(c, http.StatusOK, pages.GenerationChatPage(middleware.CSRFToken(c), view.Panel{SessionID: chat.NewSessionID(), Unavailable: h.Chat == nil}))
}

// Send answers POST /generation-test/chat/messages (session, content): the message and what the assistant answered, to append to the thread. Nothing
// is generated: an estimate comes back as a confirmation card.
func (h Handlers) Send(c *gin.Context) {
	session := c.PostForm(view.FieldSession)
	rep, err := h.Chat.Send(c.Request.Context(), session, c.PostForm(view.FieldContent))
	if errors.Is(err, chat.ErrNoSession) { // Send makes a conversation for any id of the right shape: only a malformed id is refused
		refuseWith(c, session, http.StatusGone, "この会話の ID の形が正しくありません。ページを読み込み直して、もう一度相談してください")
		return
	}
	if err != nil {
		refuse(c, session, err)
		return
	}
	h.render(c, http.StatusOK, session, rep)
}

// Confirm answers POST /generation-test/chat/proposals/:id/confirm (session): the button of a card. The batch is queued only when the service accepts the
// confirmation (the estimate is the one shown and is within the limits); the answer is the card as it is now and the block of the results, or the reason.
func (h Handlers) Confirm(c *gin.Context) {
	session := c.PostForm(view.FieldSession)
	rep, err := h.Chat.Confirm(c.Request.Context(), session, c.Param("id"))
	if err != nil {
		refuse(c, session, err)
		return
	}
	status := http.StatusOK
	if len(rep.Messages) > 0 && rep.Messages[0].Kind == chat.KindResult {
		status = http.StatusAccepted
	}
	h.render(c, status, session, rep)
}

// Dismiss answers POST /generation-test/chat/proposals/:id/dismiss (session): the card, closed, in place (out of band). A refusal is a line added to the
// thread, the card stays as it is.
func (h Handlers) Dismiss(c *gin.Context) {
	session := c.PostForm(view.FieldSession)
	rep, err := h.Chat.Dismiss(c.Request.Context(), session, c.Param("id"))
	if err != nil {
		refuse(c, session, err)
		return
	}
	h.render(c, http.StatusOK, session, rep)
}

// Batch answers GET /generation-test/chat/batches/:id, which the results block asks for every 2 seconds until its batch is over.
func (h Handlers) Batch(c *gin.Context) {
	b, err := h.Gentest.Batch(c.Request.Context(), c.Param("id"), true)
	if err != nil {
		status, text := gtshare.Refuse(c, err)
		gtshare.Render(c, status, view.Messages("", []view.Message{statusLine(resultLost + "（" + text + "）")}))
		return
	}
	gtshare.Render(c, http.StatusOK, view.ResultBlock(resultView(b)))
}

// render draws a reply: the cards that changed (out of band) and the messages to append.
func (h Handlers) render(c *gin.Context, status int, session string, rep chat.Reply) {
	changed := make([]view.Proposal, len(rep.Changed))
	for i, p := range rep.Changed {
		changed[i] = proposalView(session, p)
		changed[i].Swap = true
	}
	msgs := make([]view.Message, 0, len(rep.Messages))
	for _, m := range rep.Messages {
		msgs = append(msgs, h.messageView(c, session, m))
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := view.Messages(session, msgs).Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		return
	}
	if err := view.Swaps(changed).Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
	}
}

// messageView is a message of the service as the thread draws it.
func (h Handlers) messageView(c *gin.Context, session string, m chat.Message) view.Message {
	v := view.Message{ID: m.ID, Kind: view.Kind(m.Kind), Text: m.Text, Failed: m.Failed}
	if m.LLMKey {
		v.SettingsHref = paths.LLMSettings()
	}
	for _, o := range m.Options {
		v.Options = append(v.Options, view.Option{Label: o.Label, Detail: o.Detail, Reply: o.Reply})
	}
	if m.Proposal != nil {
		p := proposalView(session, *m.Proposal)
		v.Proposal = &p
	}
	if m.BatchID != "" {
		b, err := h.Gentest.Batch(c.Request.Context(), m.BatchID, true)
		if err != nil { // the batch was queued: say where its results are instead of drawing nothing
			h.Gentest.Logger.Warn("gentest chat: the results block of a queued batch could not be read", "batch", m.BatchID, "err", err)
			return statusLine(resultLost)
		}
		r := resultView(b)
		v.Result = &r
	}
	return v
}

// resultLost is the line that stands for a results block that cannot be drawn.
const resultLost = "結果をここに出せませんでした。「履歴」のタブで確認してください"

// statusLine is a failure line of the thread with an id of its own (a thread may hold many).
func statusLine(text string) view.Message {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("gentest chat: no random id: " + err.Error()) // crypto/rand does not fail on supported platforms
	}
	return view.Message{ID: hex.EncodeToString(b[:]), Kind: view.KindStatus, Failed: true, Text: text}
}

// refuse answers a call the service did not take with a line in the thread and the status of the cause.
func refuse(c *gin.Context, session string, err error) {
	var (
		status int
		text   string
	)
	switch {
	case errors.Is(err, chat.ErrBadMessage):
		status, text = http.StatusUnprocessableEntity, fmt.Sprintf("メッセージは 1〜%d 文字で入力してください", chat.MaxMessageRunes)
	case errors.Is(err, chat.ErrNoSession):
		status, text = http.StatusGone, "この会話はもう残っていません（アプリを再起動したか、長く使わなかったため）。ページを読み込み直して、もう一度相談してください"
	case errors.Is(err, chat.ErrNoProposal):
		status, text = http.StatusNotFound, "この見積りのカードが見つかりません。ページを読み込み直してください"
	case errors.Is(err, chat.ErrProposalClosed):
		status, text = http.StatusConflict, "この見積りはもう使えません（実行済み・やめた・新しい見積りに置き換わった）。最新のカードを使ってください"
	case c.Request.Context().Err() != nil: // the browser went away
		return
	default:
		status, text = gtshare.Refuse(c, err)
	}
	refuseWith(c, session, status, text)
}

// refuseWith answers with a failure line added to the thread.
func refuseWith(c *gin.Context, session string, status int, text string) {
	gtshare.Render(c, status, view.Messages(session, []view.Message{statusLine(text)}))
}
