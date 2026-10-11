package paths_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestProject_EscapesEverySegmentSoAnIDCannotChangeTheRoute(t *testing.T) {
	assert.Equal(t, "/projects/p1", paths.Project("p1"))
	assert.Equal(t, "/projects/p1/scenes/sc1", paths.Project("p1", "scenes", "sc1"))
	assert.Equal(t, "/projects/a%3Fb%23c%25d/assets/x%2Fy", paths.Project("a?b#c%d", "assets", "x/y"))
	assert.Equal(t, "/projects/p%20q", paths.Project("p q"))
}

func TestProjectEvents_EscapesTheIDSoTheSocketReachesTheProject(t *testing.T) {
	assert.Equal(t, "/ws/projects/p1", paths.ProjectEvents("p1"))
	assert.Equal(t, "/ws/projects/a%3Fb%23c%25d%20e", paths.ProjectEvents("a?b#c%d e"))
}

func TestShotTakeBranchSceneAPIAndSocketPaths_EscapeEverySegment(t *testing.T) {
	id := "a?b#c%d e/f"
	esc := "a%3Fb%23c%25d%20e%2Ff"
	assert.Equal(t, "/shots/"+esc, paths.Shot(id))
	assert.Equal(t, "/shots/"+esc+"/takes/partial", paths.Shot(id, "takes", "partial"))
	assert.Equal(t, "/takes/"+esc+"/select", paths.Take(id, "select"))
	assert.Equal(t, "/branches/"+esc+"/world-state", paths.Branch(id, "world-state"))
	assert.Equal(t, "/scenes/"+esc+"/shots", paths.Scene(id, "shots"))
	assert.Equal(t, "/api/v1/shots/"+esc+"/takes", paths.APIShot(id, "takes"))
	assert.Equal(t, "/api/v1/takes/"+esc+"/judge-result", paths.APITake(id, "judge-result"))
	assert.Equal(t, "/ws/shots/"+esc, paths.ShotProgress(id))
	assert.Equal(t, "/shots/plain", paths.Shot("plain"))
}

func TestNoteBrandConstraintEditTimeline_EscapeEverySegment(t *testing.T) {
	id := "a?b#c%d e/f"
	esc := "a%3Fb%23c%25d%20e%2Ff"
	assert.Equal(t, "/notes/"+esc, paths.Note(id))
	assert.Equal(t, "/notes/"+esc+"/versions/2/restore", paths.Note(id, "versions", "2", "restore"))
	assert.Equal(t, "/brand-constraints/"+esc, paths.BrandConstraint(id))
	assert.Equal(t, "/edit-timelines/"+esc, paths.EditTimeline(id))
	assert.Equal(t, "/edit-timelines/"+esc+"/request-additional-shot", paths.EditTimeline(id, "request-additional-shot"))
	assert.Equal(t, "/edit-timelines/e1/preview", paths.EditTimeline("e1", "preview"))
}

func TestSeriesReviewCredential_EscapeEverySegment(t *testing.T) {
	id := "a?b#c%d e/f"
	esc := "a%3Fb%23c%25d%20e%2Ff"
	assert.Equal(t, "/series/"+esc, paths.Series(id))
	assert.Equal(t, "/series/"+esc+"/characters/"+esc, paths.Series(id, "characters", id))
	assert.Equal(t, "/series/s1/episodes", paths.Series("s1", "episodes"))
	assert.Equal(t, "/reviews/"+esc, paths.Review(id))
	assert.Equal(t, "/reviews/"+esc+"/comments/"+esc, paths.Review(id, "comments", id))
	assert.Equal(t, "/reviews/r1/video", paths.Review("r1", "video"))
	assert.Equal(t, "/credentials/"+esc, paths.Credential(id))
	assert.Equal(t, "/credentials/openai", paths.Credential("openai"))
}
