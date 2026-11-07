// Package modelpicker answers the model picker (#1011): the dialog a field of a model opens, with the filter bar (search, the funnel and its panel, the chips) and the
// models as cards, the same on every screen that chooses one model. A screen's handler draws a field (Field) that names a Source; GET /model-picker/<source>
// answers the dialog's body, and GET /model-picker/<source>?part=results the cards alone for a changed filter. The list is narrowed by the server's
// modelfilter (a Source calls it on its own service); each source remembers its condition (pickfilter) under its own key, so a screen opens as it was left.
//
// The sources are the screens that choose one model: the prompt match and the partial redraw of the generation test, a Shot's video model, the image and video assignment of the settings and the LLM assignment. The generation test's form, which chooses several, and the model manager's
// catalog, which lists, draw the same bar and panel in their own pages (handler/gentest, handler/modelmgr) and are not sources.
package modelpicker

import (
	"context"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/handler/pickfilter"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The sources: one for each kind of field. The key is also what the screen remembers its condition under.
const (
	SourceMatch         = "match"          // the model of the prompt match
	SourceInpaint       = "inpaint"        // the model of the partial redraw
	SourceShotVideo     = "shot-video"     // the video model a Shot keeps with MANUAL or LOCK
	SourceSettingsImage = "settings-image" // an image model of an image purpose of the settings
	SourceSettingsVideo = "settings-video" // a video model (at a resolution) of a Quality Policy of the settings
	SourceLLM           = "settings-llm"   // the provider and the model of an Agent's LLM
	SourceUpscale       = "upscale"        // the upscale workflow of "make it larger"
)

// Services are what the sources list from; one that is nil leaves its sources out (a router built without it).
type Services struct {
	Gentest      *gentest.Service
	Settings     *modelsettings.Service
	LLM          *llmmodels.Service
	ModelManager *modelmanager.Service
}

// source is one list a field chooses from.
type source struct {
	title, empty string
	// facets are the sections of the panel; list the cards of the models that pass the condition, with the numbers of the line above them.
	facets func(ctx context.Context, svc Services) modelpick.Facets
	list   func(ctx context.Context, svc Services, q url.Values, cond modelfilter.Condition) ([]modelcard.Card, pickfilter.Counts, error)
}

// sources are the sources by key.
var sources = map[string]source{}

// Register adds the route of the picker.
func Register(engine *gin.Engine, svc Services) {
	engine.GET(paths.ModelPickerSource(":source"), Handler(svc))
}

// Handler answers GET /model-picker/:source. An unknown source is 404.
func Handler(svc Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.Param("source")
		src, ok := sources[key]
		if !ok {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		ctx := c.Request.Context()
		screen := pickfilter.Screen{
			Key: key, BarID: "mam-model-picker-bar", Action: actionOf(key, c.Request.URL.Query()), Target: "#" + resultsID, Start: modelfilter.Default(),
			Facets: src.facets(ctx, svc),
		}
		cond, problem := screen.Read(c)
		screen.Remember(c, cond)
		cards, counts, err := src.list(ctx, svc, c.Request.URL.Query(), pickfilter.Resolve(cond))
		if err != nil { // a condition the source cannot take: the start
			problem, cond = err.Error(), screen.Start
			cards, counts, _ = src.list(ctx, svc, c.Request.URL.Query(), pickfilter.Resolve(cond))
		}
		p := modelpick.Picker{Title: src.title, Bar: pickfilter.Sync(screen.Filter(cond, problem, counts), c), ResultsID: resultsID, Cards: cards, Empty: src.empty}
		c.Header("Cache-Control", "no-store")
		if c.Query("part") == "results" {
			gtshare.Render(c, http.StatusOK, modelpick.PickerAnswer(p))
			return
		}
		gtshare.Render(c, http.StatusOK, modelpick.PickerBody(p))
	}
}

// resultsID is the id of the cards of the picker (one picker is open at a time).
const resultsID = "mam-model-picker-results"

// actionOf is the address the bar asks for the cards: the picker's own with "part=results" and what the field carried besides the condition (which purpose, which Agent).
func actionOf(key string, q url.Values) string {
	extra := url.Values{"part": {"results"}}
	for k, v := range q {
		if isField(k) {
			extra[k] = v
		}
	}
	return paths.ModelPickerSource(key) + "?" + extra.Encode()
}

// isField says the query key is what a field carries (its context), not part of the condition or a marker of the bar.
func isField(k string) bool {
	switch k {
	case "part", modelpick.KeyMarker, modelpick.KeySync:
		return false
	}
	for _, key := range modelfilter.Keys() {
		if k == key {
			return false
		}
	}
	return true
}

// FieldFor is the field that chooses one model from a source: its id and the name its button is spoken as, the hidden inputs the choice sets, what it shows now
// (current: the card of the model chosen, or an empty card), and extra, the context the picker needs (which purpose, which Agent), as a query.
func FieldFor(key, id, label string, extra url.Values, inputs []modelpick.Input, current modelcard.Card) modelpick.FieldData {
	u := paths.ModelPickerSource(key)
	if len(extra) > 0 {
		u += "?" + extra.Encode()
	}
	return modelpick.FieldData{ID: id, Label: label, Inputs: inputs, Current: current, URL: u}
}
