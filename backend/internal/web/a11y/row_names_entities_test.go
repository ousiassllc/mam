package a11y_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/reviewlist"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/seriesview"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/shotinspector"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/worldstate"
)

// The buttons that repeat once per entity of a screen (a Take, a comment, a Shot, a World State entity or
// slot, a Series Character) name their row too (#799, #804). Each test renders two rows.

func TestShotInspector_TraceButtonsAreNamedAfterTheirTake(t *testing.T) {
	html := render(t, shotinspector.View(shotinspector.Data{
		Project: domain.Project{ID: "p"},
		Shot:    domain.Shot{ID: "s1", SceneID: "sc"},
		Takes: []domain.Take{
			{ID: "t1", ShotID: "s1", ModelName: "kling", Status: domain.TakeCandidate},
			{ID: "t2", ShotID: "s1", ModelName: "veo", Status: domain.TakeCandidate},
		},
	}))

	assert.Equal(t, []string{"生成の経緯: Take 1 · kling", "生成の経緯: Take 2 · veo"}, buttonNames(html, "生成の経緯"))
}

func TestReviewList_ResolveButtonsAreNamedAfterTheirComment(t *testing.T) {
	ms := int64(12000)
	html := render(t, reviewlist.Card("p1", reviewlist.Item{Item: domain.ReviewItem{
		Request: domain.ReviewRequest{ID: "rr1", CreatedAt: time.Now()},
		Responses: []domain.ReviewResponseRecord{{ID: "r1", ReviewerName: "Ren", Comments: []domain.ReviewComment{
			{ID: "c1", TimecodeMs: &ms, Body: "色が暗い"},
			{ID: "c2", Body: "全体に長い", Resolved: true},
		}}},
	}}))

	assert.Equal(t, []string{"解決済みにする: 0:12 色が暗い"}, buttonNames(html, "解決済みにする"))
	assert.Equal(t, []string{"未解決に戻す: 全体 全体に長い"}, buttonNames(html, "未解決に戻す"))
}

func TestShotCard_ActionsAreNamedAfterTheirShot(t *testing.T) {
	var html string
	for n, objective := range []string{"街の全景", "主人公の顔"} {
		html += render(t, molecules.ShotCard(molecules.ShotCardData{
			ProjectID: "p1",
			Shot:      domain.Shot{ID: "s", SceneID: "sc", OrderIndex: n, Objective: objective, State: domain.ShotPlanning},
			Takes:     []domain.Take{{ID: "t", ShotID: "s", Status: domain.TakeCandidate}},
			Selected:  true,
		}))
	}

	for _, label := range []string{"生成", "Reference Pack", "続きを作る"} {
		assert.Equal(t, []string{label + ": Shot 1: 街の全景", label + ": Shot 2: 主人公の顔"}, buttonNames(html, label))
	}
	assert.Contains(t, html, ">追加の指示: Shot 1: 街の全景</label>")
	assert.Contains(t, html, ">追加の指示: Shot 2: 主人公の顔</label>")
}

func worldStateData() worldstate.Data {
	return worldstate.Data{
		Project: domain.Project{ID: "p1"},
		World: domain.WorldContext{
			Characters: []domain.WorldCharacter{{ID: "c1", Name: "Mio"}, {ID: "c2", Name: "Ren"}},
			Products:   []domain.WorldProduct{{ID: "pr1", Name: "ボトル"}},
		},
	}
}

func TestWorldState_SaveAndAssignButtonsAreNamedAfterTheirEntity(t *testing.T) {
	html := render(t, worldstate.View(worldStateData()))

	assert.Equal(t, []string{"保存: Mio", "保存: Ren", "保存: ボトル"}, buttonNames(html, "保存"))
	assigned := buttonNames(html, "割り当て")
	assert.Contains(t, assigned, "割り当て: Mio の 顔 (face)")
	assert.Contains(t, assigned, "割り当て: Ren の 全身 (body)")
	assert.Equal(t, len(assigned), countDistinct(assigned), "every slot button has its own name")
}

func TestWorldState_LockSwitchAnswersASaveButtonNamedAfterItsEntity(t *testing.T) {
	html := render(t, worldstate.LockSwitched(worldStateData(), domain.TargetCharacter, "c2", "wardrobe"))

	assert.Equal(t, []string{"保存: Ren"}, buttonNames(html, "保存"))
}

func TestMoleculesWorldStateSave_IsNamedAfterItsSubject(t *testing.T) {
	fields := []molecules.WorldStateFieldData{{Name: "wardrobe"}}
	html := render(t, molecules.WorldStateSave("ws-save-x", "Mio", fields, false))

	assert.Equal(t, []string{"保存: Mio"}, buttonNames(html, "保存"))
}

func TestSeriesView_CharacterSaveButtonsAreNamedAfterTheirCharacter(t *testing.T) {
	html := render(t, seriesview.View(seriesview.Data{
		Series: domain.Series{ID: "s1", Name: "Mio", Version: 1},
		Canon:  domain.SeriesCanon{Characters: []domain.WorldCharacter{{ID: "c1", Name: "Mio"}, {ID: "c2", Name: "Ren"}}},
	}))

	names := buttonNames(html, "保存")
	assert.Contains(t, names, "保存: Mio")
	assert.Contains(t, names, "保存: Ren")
}

func countDistinct(names []string) int {
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
	}
	return len(seen)
}
