// Package paths builds the URL paths of the pages and actions of a Project, Scene, Shot, Take,
// Branch, Note, Brand Constraint, Edit Timeline, Series, Review and Credential. Their IDs come from
// outside (a bundle import keeps the ID of every row it finds, and allows characters that mean
// something in a URL), so every segment is path-escaped here and nowhere else builds "/projects/…",
// "/scenes/…", "/shots/…", "/takes/…", "/branches/…", "/notes/…", "/brand-constraints/…",
// "/edit-timelines/…", "/series/…", "/reviews/…" or "/credentials/…" by hand.
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
	return join("/projects", append([]string{id}, segments...)...)
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

// APIShot is the JSON API path "/api/v1/shots/<id>" followed by the segments.
func APIShot(id string, segments ...string) string {
	return join("/api/v1/shots", append([]string{id}, segments...)...)
}

// APITake is the JSON API path "/api/v1/takes/<id>" followed by the segments.
func APITake(id string, segments ...string) string {
	return join("/api/v1/takes", append([]string{id}, segments...)...)
}

// ProjectEvents is the WebSocket path of a Project's chat events, "/ws/projects/<id>".
func ProjectEvents(id string) string { return join("/ws/projects", id) }

// ShotProgress is the WebSocket path of the progress of a Shot, "/ws/shots/<id>".
func ShotProgress(id string) string { return join("/ws/shots", id) }

// Series is "/series/<id>" followed by the segments: Series(id, "characters", cid).
func Series(id string, segments ...string) string {
	return join("/series", append([]string{id}, segments...)...)
}

// Review is "/reviews/<id>" (an imported Review Package) followed by the segments:
// Review(id, "comments", cid).
func Review(id string, segments ...string) string {
	return join("/reviews", append([]string{id}, segments...)...)
}

// Credential is "/credentials/<provider>", the API key of a provider.
func Credential(provider string) string { return join("/credentials", provider) }
