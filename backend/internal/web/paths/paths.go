// Package paths builds the URL paths of the pages and actions of a Project, Scene, Shot, Take,
// Branch, Note, Brand Constraint, Edit Timeline, Series, Review and Credential. Their IDs come from
// outside; domain.ValidBundleID limits them to [A-Za-z0-9_-] (domain/bundle_test.go is the source
// of truth), which needs no escaping. Every segment is path-escaped here all the same, so a URL
// stays intact were that limit ever relaxed, and nowhere else builds "/projects/…",
// "/scenes/…", "/shots/…", "/takes/…", "/branches/…", "/notes/…", "/brand-constraints/…",
// "/edit-timelines/…", "/series/…", "/reviews/…", "/credentials/…" or "/api/v1/…" by hand. The routes
// of the collections (Projects, Reviews, StaticApp …) are defined here too, for the router and the pages alike. The same goes for
// the Take video and thumbnail URLs of domain (domain cannot import this package; paths_test pins
// them to Take(id, "video") and Take(id, "thumbnail")).
package paths

import (
	"net/url"
	"strings"
)

// join is prefix followed by the segments, each path-escaped.
func join(prefix string, segments ...string) string {
	var sb strings.Builder
	sb.WriteString(prefix)
	for _, s := range segments {
		sb.WriteByte('/')
		sb.WriteString(url.PathEscape(s))
	}
	return sb.String()
}

// Project is "/projects/<id>" followed by the segments, each path-escaped:
// Project("p1", "scenes", sc) is "/projects/p1/scenes/<sc>".
func Project(id string, segments ...string) string {
	return join(Projects(), append([]string{id}, segments...)...)
}

// Scene is "/scenes/<id>" followed by the segments: Scene(id, "shots") is "/scenes/<id>/shots".
func Scene(id string, segments ...string) string {
	return join("/scenes", append([]string{id}, segments...)...)
}

// Shot is "/shots/<id>" followed by the segments: Shot(id, "takes", "partial").
func Shot(id string, segments ...string) string {
	return join("/shots", append([]string{id}, segments...)...)
}

// Take is "/takes/<id>" followed by the segments: Take(id, "select").
func Take(id string, segments ...string) string {
	return join("/takes", append([]string{id}, segments...)...)
}

// Branch is "/branches/<id>" followed by the segments: Branch(id, "world-state").
func Branch(id string, segments ...string) string {
	return join("/branches", append([]string{id}, segments...)...)
}

// Note is "/notes/<id>" followed by the segments: Note(id, "versions", "2", "restore").
func Note(id string, segments ...string) string {
	return join("/notes", append([]string{id}, segments...)...)
}

// BrandConstraint is "/brand-constraints/<id>" followed by the segments.
func BrandConstraint(id string, segments ...string) string {
	return join("/brand-constraints", append([]string{id}, segments...)...)
}

// EditTimeline is "/edit-timelines/<id>" followed by the segments: EditTimeline(id, "judge").
func EditTimeline(id string, segments ...string) string {
	return join("/edit-timelines", append([]string{id}, segments...)...)
}

// APIPrefix is the base path of the JSON API used by Lit components; the router mounts the API
// under it and every API URL handed to a component starts with it.
const APIPrefix = "/api/v1"

// APIProject is the JSON API path "/api/v1/projects/<id>" followed by the segments:
// APIProject(id, "edit-timeline").
func APIProject(id string, segments ...string) string {
	return join(APIPrefix+"/projects", append([]string{id}, segments...)...)
}

// APIShot is the JSON API path "/api/v1/shots/<id>" followed by the segments.
func APIShot(id string, segments ...string) string {
	return join(APIPrefix+"/shots", append([]string{id}, segments...)...)
}

// APITake is the JSON API path "/api/v1/takes/<id>" followed by the segments.
func APITake(id string, segments ...string) string {
	return join(APIPrefix+"/takes", append([]string{id}, segments...)...)
}

// ProjectEvents is the WebSocket path of a Project's chat events, "/ws/projects/<id>".
func ProjectEvents(id string) string { return join("/ws/projects", id) }

// ShotProgress is the WebSocket path of the progress of a Shot, "/ws/shots/<id>".
func ShotProgress(id string) string { return join("/ws/shots", id) }

// Series is "/series/<id>" followed by the segments: Series(id, "characters", cid).
func Series(id string, segments ...string) string {
	return join(SeriesList(), append([]string{id}, segments...)...)
}

// Review is "/reviews/<id>" (an imported Review Package) followed by the segments:
// Review(id, "comments", cid).
func Review(id string, segments ...string) string {
	return join(Reviews(), append([]string{id}, segments...)...)
}

// Routes of the collections, without an ID: the router registers them and the pages link to and
// post to them through the same functions, as APIPrefix does for the API.

// Projects is the Project list and where a new Project is posted, "/projects".
func Projects() string { return "/projects" }

// ProjectImport is where a Project bundle is posted, "/projects/import".
func ProjectImport() string { return Projects() + "/import" }

// SeriesList is where a Series is created, "/series".
func SeriesList() string { return "/series" }

// Reviews is the inbox of imported Review Packages, "/reviews".
func Reviews() string { return "/reviews" }

// ReviewImport is where a Review Package is posted, "/reviews/import".
func ReviewImport() string { return Reviews() + "/import" }

// StaticPrefix is the base path of the front-end assets; the router serves them under it.
const StaticPrefix = "/static"

// The two files the layout loads (the CSP of middleware.SecurityHeaders allows only this script).
const (
	StaticCSS = StaticPrefix + "/app.css"
	StaticApp = StaticPrefix + "/app.js"
)

// Credential is "/credentials/<provider>", the API key of a provider.
func Credential(provider string) string { return join("/credentials", provider) }
