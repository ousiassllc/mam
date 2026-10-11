package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// ProposeContinue analyses a completed Shot and posts the NEXT candidates as a Question card
// (POST /shots/:id/continue). It answers 201 with the card, ready to append to the thread, and sends
// HTMX to the chat where the card waits for the choice.
func ProposeContinue(svc *service.ContinueService) gin.HandlerFunc {
	return func(c *gin.Context) {
		project, card, err := svc.Propose(c.Request.Context(), c.Param("id"))
		if err != nil {
			toast(c, err)
			return
		}
		c.Header("HX-Redirect", paths.Project(project.ID)) // the card is answered in the chat
		render(c, http.StatusCreated, organisms.MessageFragment(project, []domain.ThreadMessage{card}, nil))
	}
}

// ConfirmContinue makes the next Shot after the chosen candidate (POST /shots/:id/continue/confirm).
// Body: instruction (the candidate or the user's direction) or delegate=true (AIに任せる). It answers
// 202 with the new Shot card in the GENERATING state; the thread follows through chat:refresh.
func ConfirmContinue(svc *service.ContinueService) gin.HandlerFunc {
	return func(c *gin.Context) {
		frag, err := svc.Confirm(c.Request.Context(), c.Param("id"), service.ConfirmInput{
			Instruction: c.PostForm("instruction"),
			Delegate:    c.PostForm("delegate") == "true",
		})
		if err != nil {
			toast(c, err)
			return
		}
		c.Header("HX-Trigger", refreshTrigger)
		render(c, http.StatusAccepted, molecules.ShotCard(shotCardData(frag)))
	}
}
