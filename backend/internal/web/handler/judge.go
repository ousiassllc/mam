package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

// JudgeTake runs the AI Video Judge on a Take again (POST /takes/:id/judge). It answers 202 with
// the Shot card in the JUDGING state; the progress arrives on /ws/shots/:id.
func JudgeTake(svc *service.JudgeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		frag, err := svc.Judge(c.Request.Context(), c.Param("id"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusAccepted, molecules.ShotCard(takeActionCard(c, frag)))
	}
}

// RepairTake has the Repair Agent repair a Take from its Judge result (POST /takes/:id/repair). It
// answers 202 with the Shot card in the REPAIRING state; the progress arrives on /ws/shots/:id.
func RepairTake(svc *service.RepairService) gin.HandlerFunc {
	return func(c *gin.Context) {
		frag, err := svc.Repair(c.Request.Context(), c.Param("id"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusAccepted, molecules.ShotCard(takeActionCard(c, frag)))
	}
}
