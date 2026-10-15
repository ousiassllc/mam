// Package sceneshot is the Scene / Shot / Take hierarchy screen (F-04): the Scene list, the selected
// Scene with its Shots, and the forms that add a Scene or a Shot.
package sceneshot

import (
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Item is one entry of the Scene list.
type Item struct {
	Scene     domain.Scene
	ShotCount int
}

// Data is the state of the Scene / Shot hierarchy view. The error maps are keyed by form field
// name; the Input fields hold the user's rejected input.
type Data struct {
	Project domain.Project
	Scenes  []Item
	Scene   *domain.Scene // nil when the Project has no Scene
	Shots   []molecules.ShotCardData

	SceneErrors map[string]string
	SceneInput  SceneInput
	ShotErrors  map[string]string
	ShotInput   ShotInput
}

// SceneInput is the Scene form's rejected input.
type SceneInput struct{ Title, Location, Time, Objective string }

// ShotInput is the Shot form's rejected input.
type ShotInput struct{ Objective, DurationSeconds string }

// currentSceneID is the Scene the view shows ("" when the Project has none).
func (d Data) currentSceneID() string {
	if d.Scene == nil {
		return ""
	}
	return d.Scene.ID
}

func sceneURL(projectID, sceneID string) string {
	return paths.Project(projectID, "scenes", sceneID)
}

func sceneLabel(s domain.Scene) string {
	return fmt.Sprintf("Scene %d: %s", s.OrderIndex+1, s.Title)
}

func shotCountText(n int) string { return fmt.Sprintf("%d Shot", n) }

func shotCountID(sceneID string) string { return "scene-shot-count-" + sceneID }

func sceneSummary(s domain.Scene) string {
	text := ""
	for _, part := range []string{s.Location, s.Time} {
		if part == "" {
			continue
		}
		if text != "" {
			text += " / "
		}
		text += part
	}
	return text
}

// shotCountOf is the Shot count the Scene list shows for a Scene.
func (d Data) shotCountOf(sceneID string) int {
	for _, item := range d.Scenes {
		if item.Scene.ID == sceneID {
			return item.ShotCount
		}
	}
	return 0
}

// unselected is the card of a Shot that is listed but not the one selected.
func unselected(c molecules.ShotCardData) molecules.ShotCardData {
	c.Selected = false
	return c
}

// sceneFormTarget is what the Scene form's answer replaces: the form itself, or the whole view when the
// page shows no Scene (the answer is then the first Scene with its Shot form, which the page lacks).
func sceneFormTarget(currentSceneID string) string {
	if currentSceneID == "" {
		return "#scene-shot-view"
	}
	return "this"
}
