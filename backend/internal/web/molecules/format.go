// Package molecules holds the compound components of the chat: the thread cards,
// the attachment strip and the confirmation control.
package molecules

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

const replyExcerptRunes = 40

// excerpt is the one-line summary of a message used for reply previews.
func excerpt(m domain.ChatMessage) string {
	text := m.Content
	switch m.Kind {
	case domain.KindQuestion, domain.KindEscalation:
		text = domain.DecodeCardBody(m.Content).Text
	case domain.KindTake:
		text = domain.DecodeTakeCardBody(m.Content).Title
	case domain.KindProgress:
		text = "進捗: " + m.Content
	case domain.KindKeyframe:
		text = "Keyframe: " + m.Content
	}
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > replyExcerptRunes {
		return string(r[:replyExcerptRunes]) + "…"
	}
	if text == "" && len(m.AttachmentAssetIDs) > 0 {
		return "添付ファイル"
	}
	return text
}

// AssetURL is where an uploaded asset is served (GET /projects/:id/assets/:assetId).
func AssetURL(projectID, assetID string) templ.SafeURL {
	return templ.SafeURL(paths.Project(projectID, "assets", assetID))
}

func messagesURL(projectID string) string { return paths.Project(projectID, "messages") }

// cardURL is where one card is fetched as it is now (GET /projects/:id/messages/:messageId).
func cardURL(projectID, messageID string) string {
	return paths.Project(projectID, "messages", messageID)
}

// messagesAfterURL fetches the messages newer than messageID (GET /projects/:id/messages?after=<messageId>).
// The newest card of the thread carries it, so the client never builds the URL itself.
func messagesAfterURL(projectID, messageID string) string {
	return messagesURL(projectID) + "?after=" + url.QueryEscape(messageID)
}

func approvalURL(projectID, messageID string) string {
	return paths.Project(projectID, "approvals", messageID)
}

// answerAttrs makes a button post `answer` as a reply to the card. The buttons of a card
// (data-options) share one in-flight request: a second click is dropped until the first one
// answers, and the clicked button is disabled meanwhile.
func answerAttrs(m domain.ThreadMessage, answer string) templ.Attributes {
	return templ.Attributes{
		"type":            "button",
		"hx-post":         messagesURL(m.ProjectID),
		"hx-vals":         atoms.HxVals(map[string]string{"content": answer, "reply_to_message_id": m.ID}),
		"hx-target":       "#chat-thread",
		"hx-swap":         "beforeend",
		"hx-sync":         "closest [data-options]:drop",
		"hx-disabled-elt": "this",
	}
}

func replyAttrs(m domain.ThreadMessage) templ.Attributes {
	return templ.Attributes{
		"type":             "button",
		"data-reply-to":    m.ID,
		"data-reply-label": excerpt(m.ChatMessage),
	}
}

func submitAttrs(decision string) templ.Attributes {
	return templ.Attributes{"type": "submit", "name": "decision", "value": decision}
}

// approvalLabel names what the approval card asks to confirm.
func approvalLabel(m domain.ChatMessage) string {
	if m.RefType == domain.RefNotePromotion {
		return "World State への反映の提案"
	}
	if cp, ok := domain.CheckpointForRefType(m.RefType); ok {
		return "確認: " + cp.Label()
	}
	return "確認"
}

func approvalBadge(s domain.ApprovalStatus) (atoms.BadgeTone, string) {
	switch s {
	case domain.ApprovalApproved:
		return atoms.BadgeOK, "承認済み"
	case domain.ApprovalChangesRequested:
		return atoms.BadgeWarn, "修正依頼済み"
	}
	return atoms.BadgeInfo, "確認待ち"
}

// progressView is the text and tone of a progress card for the live job status.
func progressView(job *domain.Job) (atoms.BadgeTone, string, bool) {
	if job == nil {
		return atoms.BadgeNeutral, "状態不明", false
	}
	switch job.Status {
	case domain.JobQueued:
		return atoms.BadgeNeutral, "待機中", true
	case domain.JobRunning:
		return atoms.BadgeInfo, "実行中", true
	case domain.JobSucceeded:
		return atoms.BadgeOK, "完了", false
	case domain.JobFailed:
		return atoms.BadgeError, "失敗", false
	}
	return atoms.BadgeNeutral, string(job.Status), false
}

func scorePercent(score float64) string { return fmt.Sprintf("%.2f", score) }

// frameRate writes a frame rate for the fps attribute of mam-video-player ("23.976", "30").
func frameRate(fps float64) string { return strconv.FormatFloat(fps, 'f', -1, 64) }

func frameClass(m domain.ThreadMessage) string {
	base := "max-w-[85%] rounded-lg px-4 py-3 text-sm "
	if m.Role == domain.RoleUser {
		return base + "ml-auto bg-indigo-600/30 text-neutral-100"
	}
	return base + "mr-auto border border-neutral-700 bg-neutral-900 text-neutral-100"
}

type oobKey struct{}

// WithOOB makes the cards rendered with the returned context replace the card of the
// same id already on the page (out-of-band swap) instead of being appended.
func WithOOB(ctx context.Context) context.Context { return context.WithValue(ctx, oobKey{}, true) }

func isOOB(ctx context.Context) bool { v, _ := ctx.Value(oobKey{}).(bool); return v }

// refLink is the link a text notice carries: the notice names what it points at (ref_type / ref_id),
// and the URL is built here from paths, not written into the message by the service or agent.
type refLink struct {
	Label string
	Href  templ.SafeURL
	Attrs templ.Attributes
}

// refLinkOf is the link of an AI text notice whose ref_type is a destination, if it has one.
func refLinkOf(m domain.ThreadMessage) (refLink, bool) {
	if m.Role != domain.RoleAssistant || m.Kind != domain.KindText {
		return refLink{}, false
	}
	switch {
	case m.RefType == domain.RefExport && m.RefID != "":
		return refLink{
			Label: "書き出した動画をダウンロード",
			Href:  templ.SafeURL(paths.Project(m.ProjectID, "exports", m.RefID, "video")),
			Attrs: templ.Attributes{"download": true},
		}, true
	case m.RefType == domain.RefSettings:
		return refLink{Label: "設定画面を開く", Href: templ.SafeURL(paths.Project(m.ProjectID, "settings"))}, true
	case m.RefType == domain.RefGlobalSettings:
		slug, label := globalSettingsTarget(m.RefID)
		return refLink{Label: label, Href: templ.SafeURL(paths.SettingsCategory(slug))}, true
	}
	return refLink{}, false
}
