package handler

import (
	"net/http"
	"strconv"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

// GenerateShot starts the generation of a Shot (POST /shots/:id/generate). It answers 202 with the
// Shot card in the GENERATING state; the progress arrives on /ws/shots/:id.
func GenerateShot(svc *service.GenerationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		frag, err := svc.GenerateShot(c.Request.Context(), c.Param("id"), c.PostForm("additional_instruction"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusAccepted, molecules.ShotCard(shotCardData(frag)))
	}
}

// GenerateCandidates starts Prompt Candidate Generation for a Shot (POST /shots/:id/generate/candidates).
// It answers 202 with the Shot card in the GENERATING state, like GenerateShot.
func GenerateCandidates(svc *service.GenerationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		frag, err := svc.GenerateCandidates(c.Request.Context(), c.Param("id"), c.PostForm("additional_instruction"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusAccepted, molecules.ShotCard(shotCardData(frag)))
	}
}

// BuildReferencePack starts the (re)generation of the Reference Pack of a Shot
// (POST /shots/:id/reference-pack). It answers 202 like GenerateShot.
func BuildReferencePack(svc *service.GenerationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		frag, err := svc.BuildReferencePack(c.Request.Context(), c.Param("id"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusAccepted, molecules.ShotCard(shotCardData(frag)))
	}
}

func shotCardData(f service.ShotFragment) molecules.ShotCardData {
	return molecules.ShotCardData{ProjectID: f.ProjectID, Shot: f.Shot, Storyboard: f.Storyboard, Takes: f.Takes, Selected: f.Selected, Repairable: f.Repairable, Judgeable: f.Judgeable, Candidates: f.Candidates}
}

// takeActionCard is the Shot card that answers an action of a Take button. The card replaces the one the
// button sits on, so it is drawn as selected only if that one was: the button says so in the "selected"
// value (htmx puts it in the query of a DELETE, in the body of a POST). Without it the Shot is the
// selected one, as the service reports.
func takeActionCard(c *gin.Context, f service.ShotFragment) molecules.ShotCardData {
	d := shotCardData(f)
	raw, ok := c.GetQuery(molecules.SelectedParam)
	if !ok {
		raw, ok = c.GetPostForm(molecules.SelectedParam)
	}
	if selected, err := strconv.ParseBool(raw); ok && err == nil {
		d.Selected = selected
	}
	return d
}

// ShotProgress streams the progress of a Shot (WebSocket /ws/shots/:id): queued, generating, done,
// failed. The first message is "idle" when nothing is running.
func ShotProgress(svc *service.GenerationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		events, cancel, err := svc.SubscribeShot(c.Request.Context(), c.Param("id"))
		if err != nil {
			status, _ := statusOf(c, err)
			c.AbortWithStatus(status)
			return
		}
		defer cancel()
		streamEvents(c, events)
	}
}

// streamEvents upgrades the request to a WebSocket and writes every event of the channel as JSON
// until the client goes away.
func streamEvents[T any](c *gin.Context, events <-chan T) {
	// Accept checks that Origin matches Host, so only pages served by this app can connect.
	conn, err := websocket.Accept(c.Writer, c.Request, nil)
	if err != nil {
		return // Accept has already answered the request
	}
	defer func() { _ = conn.CloseNow() }()
	ctx := conn.CloseRead(c.Request.Context()) // cancelled when the client goes away
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-events:
			if err := wsjson.Write(ctx, conn, e); err != nil {
				return
			}
		}
	}
}
