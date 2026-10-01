// Package paths builds the URL paths of the pages and actions of a Project. The IDs of a Project
// come from outside (a bundle import keeps the ID it finds, and allows characters that mean
// something in a URL), so every segment is path-escaped here and nowhere else builds
// "/projects/…" by hand.
package paths

import (
	"net/url"
	"strings"
)

// Project is "/projects/<id>" followed by the segments, each path-escaped:
// Project("p1", "scenes", sc) is "/projects/p1/scenes/<sc>".
func Project(id string, segments ...string) string {
	var sb strings.Builder
	sb.WriteString("/projects/")
	sb.WriteString(url.PathEscape(id))
	for _, s := range segments {
		sb.WriteByte('/')
		sb.WriteString(url.PathEscape(s))
	}
	return sb.String()
}

// ProjectEvents is the WebSocket path of a Project's chat events, "/ws/projects/<id>".
func ProjectEvents(id string) string {
	return "/ws/projects/" + url.PathEscape(id)
}
