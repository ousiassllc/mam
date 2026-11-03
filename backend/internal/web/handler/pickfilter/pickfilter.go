// Package pickfilter is the handler side of the model filter (#1011): it reads the condition of a request (the query string of the bar, else what the screen
// remembered, else the screen's starting point), remembers it per screen, and builds the molecules/modelpick.Filter the bar, the panel and the chips are drawn
// from. The list is narrowed by modelfilter (the screen's service calls it: mmview.Where, gentest.ModelsWhere, llmmodels.Candidates, ...): nothing here
// narrows anything, and the script does not either.
//
// What a screen remembers is its condition as a query string in a cookie of its own (mam_mf_<key>), written when a request states the condition (the bar's
// requests carry the marker modelpick.KeyMarker) and read when the screen is opened with none. A cookie rather than localStorage, for the reason of the side
// menu (navstate): the desktop app loads the loopback URL on a port that may change, a cookie is shared across ports, localStorage is not; and the server
// can draw the first page already narrowed, with no flash of the full list.
package pickfilter

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
)

// CookiePrefix is the start of the name of the cookie a screen remembers its condition in; the screen's Key follows.
const CookiePrefix = "mam_mf_"

// maxAge is how long a choice is kept: a year (as the side menu's).
const maxAge = 365 * 24 * 60 * 60

// Screen is one list of models that narrows by a condition.
type Screen struct {
	// Key names the screen in what it remembers ("gentest-image", "model-manager"): one cookie per screen.
	Key string
	// BarID is the id of the bar element.
	BarID string
	// Action is the address that answers the results (a request of the bar, a chip or a button), Target the selector of the element it replaces and Include the
	// selectors of the fields of the screen that travel with the request (the models that are ticked).
	Action, Target, Include string
	// Start is the condition the screen starts at: modelfilter.Default() for a list that offers models to choose from (what cannot be chosen now is hidden),
	// the zero condition for the model manager's catalog.
	Start  modelfilter.Condition
	Facets modelpick.Facets
}

// Read is the condition of the request and, when it could not be read, why. A request that states the condition (it has the marker or a key of the condition) is
// taken as it is; one that does not is the screen opened: what the screen remembered, else its starting point. A condition that does not read falls back to the
// start (the bar shows the reason).
func (s Screen) Read(c *gin.Context) (cond modelfilter.Condition, problem string) {
	q := c.Request.URL.Query()
	if _, stated := q[modelpick.KeyMarker]; stated {
		cond, err := modelfilter.ParseQuery(q)
		if err != nil {
			return s.Start.Normalize(), err.Error()
		}
		return cond, ""
	}
	if cond, err := modelfilter.ParseQueryOr(q, s.remembered(c)); err == nil {
		return cond, ""
	} else {
		return s.Start.Normalize(), err.Error()
	}
}

// remembered is the condition the screen stored, or its start.
func (s Screen) remembered(c *gin.Context) modelfilter.Condition {
	raw, err := c.Cookie(CookiePrefix + s.Key)
	if err != nil {
		return s.Start
	}
	v, err := url.ParseQuery(raw)
	if err != nil {
		return s.Start
	}
	cond, err := modelfilter.ParseQueryOr(v, s.Start)
	if err != nil {
		return s.Start
	}
	return cond
}

// Remember stores the condition of a request that stated it (the marker): the next time the screen is opened, it starts there. The commercial part stays as stated
// ("" follows the setting 「商用OKのモデルだけを使う」). A request that did not state it stores nothing.
func (s Screen) Remember(c *gin.Context, cond modelfilter.Condition) {
	if _, stated := c.Request.URL.Query()[modelpick.KeyMarker]; !stated {
		return
	}
	value := cond.Query(s.Start).Encode()
	if value == "" {
		value = "mf=1" // a condition at its start, but a stated one: the cookie says so (an empty value would delete it)
	}
	//nolint:gosec // Secure cannot be set: the server is plain HTTP on the loopback interface (as the CSRF cookie)
	http.SetCookie(c.Writer, &http.Cookie{
		Name: CookiePrefix + s.Key, Value: url.QueryEscape(value), Path: "/", MaxAge: maxAge, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// Resolve is the condition the list is narrowed by: the commercial part that the setting decides when the condition leaves it empty (the setting 「商用OKのモデルだけを
// 使う」, modellicense.DefaultFilter).
func Resolve(cond modelfilter.Condition) modelfilter.Condition {
	if cond.Commercial == "" {
		cond.Commercial = string(modellicense.DefaultFilter())
	}
	return cond.Normalize()
}

// Filter is what the bar, the panel and the chips are drawn from: the condition (resolved), how many models the list has, how many pass it and how many only
// "選べるものだけ" hides.
func (s Screen) Filter(cond modelfilter.Condition, problem string, counts Counts) modelpick.Filter {
	return modelpick.Filter{
		ID: s.BarID, Key: s.Key, Action: s.Action, Target: s.Target, Include: s.Include,
		Cond: Resolve(cond), Start: s.Start.Normalize(), DefaultCommercial: modellicense.DefaultFilter(), Facets: s.Facets,
		Sync: false, Error: problem, Hidden: counts.Hidden, Total: counts.Total, Shown: counts.Shown,
	}
}

// Sync marks the filter as the answer to a request of a chip or a button: the panel is drawn again out of band.
func Sync(f modelpick.Filter, c *gin.Context) modelpick.Filter {
	_, f.Sync = c.Request.URL.Query()[modelpick.KeySync]
	return f
}

// Counts are the numbers of the line above a list.
type Counts struct{ Total, Shown, Hidden int }

// CountOf counts the list: its models, those that pass the condition and, of those that pass every part but "選べるものだけ", how many cannot be chosen now.
func CountOf[T any](cond modelfilter.Condition, list []T, item func(T) modelfilter.Item) Counts {
	cond = Resolve(cond)
	all := cond
	all.AvailableOnly = false
	passing := modelfilter.Filter(all, list, item)
	n := Counts{Total: len(list), Shown: len(passing)}
	for _, e := range passing {
		if !item(e).Available {
			n.Hidden++
		}
	}
	if cond.AvailableOnly {
		n.Shown -= n.Hidden
	}
	return n
}
