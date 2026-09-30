package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// refreshTrigger is the HX-Trigger event that makes the chat fetch the messages it does
// not show yet. Action responses send it so the thread is complete even without the WebSocket.
const refreshTrigger = "chat:refresh"

// ChatWorkspace renders the Chat Workspace (GET /projects/:id): the full page, or the
// workspace fragment for HTMX. The history is the same content as GET /projects/:id/messages.
func ChatWorkspace(svc *service.ChatService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ws, err := svc.Workspace(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.ChatWorkspace(ws.Project, ws.Messages))
			return
		}
		render(c, http.StatusOK, pages.ChatWorkspacePage(middleware.CSRFToken(c), ws.Project, ws.Messages))
	}
}

// ChatMessages returns the thread fragment (GET /projects/:id/messages?after=<messageId>):
// every message, or only those after the given one.
func ChatMessages(svc *service.ChatService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ws, err := svc.Messages(c.Request.Context(), c.Param("id"), c.Query("after"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.MessageFragment(ws.Project, ws.Messages, ws.Updated))
	}
}

// ChatCard returns one card as it is now (GET /projects/:id/messages/:messageId), used to
// update a progress card in place.
func ChatCard(svc *service.ChatService) gin.HandlerFunc {
	return func(c *gin.Context) {
		project, m, err := svc.Card(c.Request.Context(), c.Param("id"), c.Param("messageId"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.CardFragment(project, m))
	}
}

// PostMessage appends the user's message and the AI's answer (POST /projects/:id/messages)
// and returns both as fragments for the thread.
func PostMessage(svc *service.ChatService) gin.HandlerFunc {
	return func(c *gin.Context) {
		in, err := readPostMessage(c)
		if err != nil {
			toast(c, err)
			return
		}
		ws, err := svc.PostMessage(c.Request.Context(), c.Param("id"), in)
		if err != nil {
			toast(c, err)
			return
		}
		// A reply to a Continue card starts a generation whose cards are posted after this answer.
		c.Header("HX-Trigger", refreshTrigger)
		render(c, http.StatusOK, organisms.MessageFragment(ws.Project, ws.Messages, ws.Updated))
	}
}

func readPostMessage(c *gin.Context) (service.PostMessageInput, error) {
	var in service.PostMessageInput
	if c.ContentType() == "multipart/form-data" {
		content, uploads, err := readMessageForm(c)
		if err != nil {
			return in, err
		}
		in.Content, in.Uploads = content, uploads
	} else {
		in.Content = c.PostForm("content")
	}
	in.ReplyToMessageID = c.PostForm("reply_to_message_id")
	in.AttachmentAssetIDs = c.PostFormArray("attachment_asset_ids")
	return in, nil
}

// RespondApproval answers an Approval card (POST /projects/:id/approvals/:messageId).
// It returns the card in its answered state; the follow-up messages arrive through chat:refresh.
func RespondApproval(svc *service.ChatService) gin.HandlerFunc {
	return func(c *gin.Context) {
		project, card, err := svc.RespondApproval(c.Request.Context(), c.Param("id"), c.Param("messageId"),
			service.Decision(c.PostForm("decision")), c.PostForm("comment"))
		if err != nil {
			toast(c, err)
			return
		}
		c.Header("HX-Trigger", refreshTrigger)
		render(c, http.StatusOK, organisms.CardFragment(project, card))
	}
}
