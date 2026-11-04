package venhancescreen

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/videoenhance"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/videoenhancebox"
)

// outputText is the video the quote would make: the size (an upscale) and the frame rate.
func outputText(est videoenhance.Estimate) string {
	if est.Op == string(venhance.OpInterpolate) {
		return fmt.Sprintf("%g fps → %g fps", est.Source.FPS, est.Output.FPS)
	}
	return fmt.Sprintf("%d × %d → %d × %d", est.Source.Width, est.Source.Height, est.Output.Width, est.Output.Height)
}

// modelLabelOf is the name of the model in the catalogue (the id when it is not there).
func modelLabelOf(c *gin.Context, svc *gentest.Service, id string) string {
	for _, m := range videoenhance.Enhancers(c.Request.Context(), svc) {
		if m.ID == id {
			return m.Label
		}
	}
	return id
}

// hiddenOf are the fields the form that runs the quote posts back: the values of the estimate as they were posted, the id of the quote and the credits of it.
func hiddenOf(c *gin.Context, est videoenhance.Estimate) []videoenhancebox.Pair {
	var out []videoenhancebox.Pair
	for _, name := range []string{
		videoenhancebox.FieldOp, videoenhancebox.FieldModel, videoenhancebox.FieldSize, videoenhancebox.FieldInterp, videoenhancebox.FieldFPS,
	} {
		if v, ok := c.GetPostForm(name); ok {
			out = append(out, videoenhancebox.Pair{Name: name, Value: v})
		}
	}
	return append(out,
		videoenhancebox.Pair{Name: videoenhancebox.FieldEstimateID, Value: est.EstimateID},
		videoenhancebox.Pair{Name: videoenhancebox.FieldCredits, Value: videoenhancebox.CreditsText(*est.Credits)})
}
