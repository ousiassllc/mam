package service

import (
	"context"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// PlanService shows and edits the Scene / Shot / Take hierarchy of a Project (F-04).
type PlanService struct{ *core }

// SceneNav is one entry of the Scene list.
type SceneNav struct {
	domain.Scene
	ShotCount int
}

// ShotView is a Shot with what hangs below it.
type ShotView struct {
	domain.Shot
	Storyboard *domain.ShotStoryboard // nil when the Shot has no storyboard frame
	Takes      []domain.Take
	Selected   bool
	// Repairable holds the Takes (by ID) whose Repair is accepted now (domain.CheckRepair).
	Repairable map[string]bool
}

// SceneView is the hierarchy page: the Project's Scenes, the selected Scene with its Shots and Takes.
type SceneView struct {
	Project domain.Project
	Scenes  []SceneNav
	Scene   *domain.Scene // nil when the Project has no Scene
	Shots   []ShotView
}

// Hierarchy returns the view with the Scene selected. An empty sceneID selects the first Scene.
// shotID marks one Shot of it as selected (ignored when it is not in the Scene).
func (s *PlanService) Hierarchy(ctx context.Context, projectID, sceneID, shotID string) (SceneView, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return SceneView{}, err
	}
	scenes, err := s.plans.ListScenes(ctx, projectID)
	if err != nil {
		return SceneView{}, err
	}
	counts, err := s.plans.ShotCounts(ctx, projectID)
	if err != nil {
		return SceneView{}, err
	}
	view := SceneView{Project: project, Scenes: make([]SceneNav, len(scenes))}
	for i, sc := range scenes {
		view.Scenes[i] = SceneNav{Scene: sc, ShotCount: counts[sc.ID]}
	}
	if len(scenes) == 0 {
		if sceneID != "" {
			return SceneView{}, ErrNotFound
		}
		return view, nil
	}

	idx := 0
	if sceneID != "" {
		if idx = slices.IndexFunc(scenes, func(sc domain.Scene) bool { return sc.ID == sceneID }); idx < 0 {
			return SceneView{}, ErrNotFound
		}
	}
	view.Scene = &scenes[idx]

	shots, err := s.plans.ListShots(ctx, view.Scene.ID)
	if err != nil {
		return SceneView{}, err
	}
	takes, err := s.plans.ListTakes(ctx, view.Scene.ID)
	if err != nil {
		return SceneView{}, err
	}
	for _, sh := range shots {
		v := ShotView{Shot: sh, Takes: takes[sh.ID], Selected: sh.ID == shotID}
		if frame, ok := sh.Storyboard(); ok {
			v.Storyboard = &frame
		}
		view.Shots = append(view.Shots, v)
	}
	views := make([]*ShotView, len(view.Shots))
	for i := range view.Shots {
		views[i] = &view.Shots[i]
	}
	if err := s.repairable.mark(ctx, projectID, views); err != nil {
		return SceneView{}, err
	}
	return view, nil
}

// SceneForm is the input of a new Scene (POST /projects/:id/scenes).
type SceneForm struct {
	Title, Location, Time, Objective string
	// CurrentSceneID is the Scene shown when the input is rejected ("" = the first Scene).
	CurrentSceneID string
}

const (
	sceneTitleMaxRunes = 100
	sceneMetaMaxRunes  = 100
	textMaxRunes       = 300
	maxShotSeconds     = 60
)

// CreateScene appends a Scene to the Project and shows it selected. The change is recorded
// in the thread. On a ValidationError the returned view is the unchanged hierarchy.
func (s *PlanService) CreateScene(ctx context.Context, projectID string, form SceneForm) (SceneView, error) {
	title := strings.TrimSpace(form.Title)
	location, when, objective := strings.TrimSpace(form.Location), strings.TrimSpace(form.Time), strings.TrimSpace(form.Objective)
	err := validateScene(title, location, when, objective)
	if err != nil {
		view, verr := s.Hierarchy(ctx, projectID, form.CurrentSceneID, "")
		if verr != nil {
			return SceneView{}, verr
		}
		return view, err
	}
	scene, err := s.plans.CreateScene(ctx, domain.Scene{ProjectID: projectID, Title: title, Location: location, Time: when, Objective: objective})
	if err != nil {
		return SceneView{}, notFound(err)
	}
	s.announce(ctx, projectID, "Scene「"+title+"」を追加しました。")
	return s.Hierarchy(ctx, projectID, scene.ID, "")
}

func validateScene(title, location, when, objective string) error {
	switch {
	case title == "":
		return invalid("title", "Scene の見出しを入力してください")
	case utf8.RuneCountInString(title) > sceneTitleMaxRunes:
		return invalid("title", "%d 文字以内で入力してください", sceneTitleMaxRunes)
	case utf8.RuneCountInString(location) > sceneMetaMaxRunes:
		return invalid("location", "%d 文字以内で入力してください", sceneMetaMaxRunes)
	case utf8.RuneCountInString(when) > sceneMetaMaxRunes:
		return invalid("time", "%d 文字以内で入力してください", sceneMetaMaxRunes)
	case utf8.RuneCountInString(objective) > textMaxRunes:
		return invalid("objective", "%d 文字以内で入力してください", textMaxRunes)
	}
	return nil
}

// ShotForm is the input of a new Shot (POST /scenes/:id/shots). DurationSeconds is the raw text of the field.
type ShotForm struct {
	Objective       string
	DurationSeconds string
}

// CreateShot appends a Shot to the Scene and shows the Scene with the new Shot selected. The
// change is recorded in the thread. On a ValidationError the returned view is the unchanged Scene.
func (s *PlanService) CreateShot(ctx context.Context, sceneID string, form ShotForm) (SceneView, error) {
	scene, err := s.plans.GetSceneByID(ctx, sceneID)
	if err != nil {
		return SceneView{}, notFound(err)
	}
	objective := strings.TrimSpace(form.Objective)
	seconds, err := parseShotInput(objective, form.DurationSeconds)
	if err != nil {
		view, verr := s.Hierarchy(ctx, scene.ProjectID, scene.ID, "")
		if verr != nil {
			return SceneView{}, verr
		}
		return view, err
	}
	shot, err := s.plans.CreateShot(ctx, domain.Shot{SceneID: scene.ID, Objective: objective, Duration: seconds})
	if err != nil {
		return SceneView{}, notFound(err)
	}
	s.announce(ctx, scene.ProjectID, "Scene「"+scene.Title+"」に Shot を追加しました: "+objective)
	return s.Hierarchy(ctx, scene.ProjectID, scene.ID, shot.ID)
}

func parseShotInput(objective, duration string) (float64, error) {
	switch {
	case objective == "":
		return 0, invalid("objective", "Shot の狙いを入力してください")
	case utf8.RuneCountInString(objective) > textMaxRunes:
		return 0, invalid("objective", "%d 文字以内で入力してください", textMaxRunes)
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(duration), 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > maxShotSeconds {
		return 0, invalid("duration_seconds", "0 より大きく %d 以下の秒数を入力してください", maxShotSeconds)
	}
	return seconds, nil
}

// announce records a change in the thread. A failure is logged: the change itself is stored.
func (s *PlanService) announce(ctx context.Context, projectID, text string) {
	if err := (&ChatService{s.core}).Announce(ctx, projectID, assistantText(text)); err != nil {
		s.logger.Error("record scene change in the thread failed", "project_id", projectID, "err", err)
	}
}
