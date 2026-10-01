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
