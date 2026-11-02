// Package sidenav is the fixed menu on the left of every screen (spec/components/side-menu.md): the screens of the app and, on a
// Project's screens, the views of that Project.
package sidenav

import (
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Key names one entry of the menu; the entry of the screen that is shown is the current one (aria-current="page").
type Key string

// The screens of the app.
const (
	None           Key = ""
	Projects       Key = "projects"
	GenerationTest Key = "generation-test"
	ModelManager   Key = "model-manager"
	Workflows      Key = "workflows"
	Nodes          Key = "nodes"
	Reviews        Key = "reviews"
	Analytics      Key = "analytics"
	Settings       Key = "settings"
)

// The views of a Project, on the Project's screens.
const (
	Chat             Key = "chat"
	Brief            Key = "brief"
	SceneShot        Key = "scene-shot"
	Edit             Key = "edit"
	ProjectSettings  Key = "project-settings"
	ProjectAnalytics Key = "project-analytics"
	Branch           Key = "branch"
	WorldState       Key = "world-state"
	References       Key = "references"
	Notes            Key = "notes"
	ProjectReviews   Key = "project-reviews"
	Series           Key = "series"
)

// State is what a page tells the menu: which entry is current and, on a Project's screens, the Project.
type State struct {
	Current Key
	Project *domain.Project
	// Hidden draws no menu at all (the guide of the first start).
	Hidden bool
}

// At is the state of a screen of the app.
func At(current Key) State { return State{Current: current} }

// InProject is the state of a view of p.
func InProject(current Key, p domain.Project) State { return State{Current: current, Project: &p} }

// Off is the state of a page that has no menu.
func Off() State { return State{Hidden: true} }

// Item is one entry: a link with an icon and a short label.
type Item struct {
	Key   Key
	Label string
	Href  string
	Icon  atoms.IconName
}

// Screens are the screens of the app, in the order of the menu. Every Href is a route of the router (the test of the menu
// fetches them all).
func Screens() []Item {
	return []Item{
		{Projects, "プロジェクト", paths.Home(), atoms.IconProjects},
		{GenerationTest, "生成テスト", paths.GenerationTest(), atoms.IconFlask},
		{ModelManager, "モデルマネージャー", paths.ModelManager(), atoms.IconPackage},
		{Workflows, "ワークフロー", paths.Workflows(), atoms.IconWorkflow},
		{Nodes, "ノード", paths.Nodes(), atoms.IconNetwork},
		{Reviews, "レビュー", paths.Reviews(), atoms.IconReview},
		{Analytics, "制作指標", paths.Analytics(), atoms.IconChart},
		{Settings, "設定", paths.Settings(), atoms.IconSettings},
	}
}

// Views are the views of p, in the order of the menu (what the chat's "補助ビュー" menu listed); the Series page only for an
// Episode.
func Views(p domain.Project) []Item {
	items := []Item{
		{Chat, "チャット", paths.Project(p.ID), atoms.IconChat},
		{Brief, "Brief", paths.Project(p.ID, "brief"), atoms.IconBrief},
		{SceneShot, "Scene / Shot", paths.Project(p.ID, "scenes"), atoms.IconMedia},
		{Edit, "編集 / 書き出し", paths.Project(p.ID, "edit"), atoms.IconEdit},
		{ProjectSettings, "設定 / コスト", paths.Project(p.ID, "settings"), atoms.IconCost},
		{ProjectAnalytics, "制作指標", paths.Project(p.ID, "analytics"), atoms.IconChart},
		{Branch, "Branch", paths.Project(p.ID, "branches"), atoms.IconBranch},
		{WorldState, "World State", paths.Project(p.ID, "world-state"), atoms.IconWorld},
		{References, "Reference Library", paths.Project(p.ID, "references"), atoms.IconLibrary},
		{Notes, "Notes", paths.Project(p.ID, "notes"), atoms.IconNotes},
		{ProjectReviews, "レビュー", paths.Project(p.ID, "reviews"), atoms.IconReview},
	}
	if p.SeriesID != "" {
		items = append(items, Item{Series, "Series", paths.Series(p.SeriesID), atoms.IconSeries})
	}
	return items
}
