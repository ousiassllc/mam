package shotpick_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/shotpick"
)

func ref(id string, scene, shot int) repository.ShotRef {
	return repository.ShotRef{Shot: domain.Shot{ID: id, OrderIndex: shot}, Scene: domain.Scene{OrderIndex: scene}}
}

func TestNamed(t *testing.T) {
	shots := []repository.ShotRef{ref("s1-1", 0, 0), ref("s1-2", 0, 1), ref("s2-1", 1, 0)}
	branchTarget := ref("branch-s1-2", 0, 1) // a Branch's Scene 1 / Shot 2: the same numbers as the trunk's s1-2

	tests := []struct {
		name         string
		target       *repository.ShotRef
		scene, shot  int
		wantID       string // "" = nil
		wantIsTarget bool
	}{
		{name: "the numbers find the Shot", scene: 2, shot: 1, wantID: "s2-1"},
		{name: "the numbers are 1-based", scene: 1, shot: 1, wantID: "s1-1"},
		{name: "the target wins when it has the numbers", target: &branchTarget, scene: 1, shot: 2, wantID: "branch-s1-2", wantIsTarget: true},
		{name: "another target does not hide the Shot named", target: &branchTarget, scene: 2, shot: 1, wantID: "s2-1"},
		{name: "no Scene number names nothing", scene: 0, shot: 1},
		{name: "no Shot number names nothing", scene: 1, shot: 0},
		{name: "a negative number names nothing", scene: -1, shot: -1},
		{name: "numbers no Shot has name nothing", scene: 3, shot: 1},
		{name: "a Shot number the Scene lacks names nothing", scene: 2, shot: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shotpick.Named(shots, tt.target, tt.scene, tt.shot)

			if tt.wantID == "" {
				assert.Nil(t, got)
				return
			}
			if assert.NotNil(t, got) {
				assert.Equal(t, tt.wantID, got.Shot.ID)
				assert.Equal(t, tt.wantIsTarget, got == tt.target, "the target itself is returned, not a copy")
			}
		})
	}
}
