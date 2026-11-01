package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func TestPipeline_LLMFailureIsReportedInTheChatAndResumesFromTheFailedStage(t *testing.T) {
	f := newPipelineFixture(t)
	f.llm.setFailure(llm.AgentDirector, &llm.APIError{Provider: llm.ProviderAnthropic, Status: 503, Message: "secret-detail"})
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	_, err := f.svc.Projects.SetConfirmationLevel(context.Background(), p.ID, domain.ConfirmFullAuto, nil)
	require.NoError(t, err)
	f.completeBrief(t, p.ID)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		msgs := f.thread(c, p.ID)
		assert.Contains(c, lastMessage(c, msgs).Content, "制作を続けられませんでした")
	}, 5*time.Second, 10*time.Millisecond)
	f.waitIdle(t, p.ID)

	msgs := f.thread(t, p.ID)
	notice := lastMessage(t, msgs).Content
	assert.NotContains(t, notice, "secret-detail", "provider details stay out of the chat")
	assert.Equal(t, domain.StateVisualDesign, f.state(t, p.ID), "the script stage was completed and kept")
	assert.Empty(t, f.hierarchy(t, p.ID).Scenes)
	require.Equal(t, 1, f.llm.count(llm.AgentProducer))
	f.llm.setFailure(llm.AgentDirector, nil)
	_, err = f.svc.Chat.PostMessage(context.Background(), p.ID, service.PostMessageInput{Content: "続けてください"})
	require.NoError(t, err)
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Len(c, f.hierarchy(c, p.ID).Scenes, 2) }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, f.llm.count(llm.AgentProducer), "the finished stages are not run again")
	assert.Equal(t, 1, f.llm.count(llm.AgentWriter))
}

func TestPipeline_MissingAPIKeyIsExplained(t *testing.T) {
	f := newPipelineFixture(t)
	f.llm.setFailure(llm.AgentProducer, llm.ErrNoAPIKey)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	f.completeBrief(t, p.ID)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Contains(c, lastMessage(c, f.thread(c, p.ID)).Content, "API キー")
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, domain.StatePlanning, f.state(t, p.ID))
}

func TestPipeline_ProgressCardTracksThePlanJob(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	f.completeBrief(t, p.ID)
	f.waitState(t, p.ID, domain.StateAwaitingApproval)
	f.waitIdle(t, p.ID)

	var progress []domain.ThreadMessage
	for _, m := range f.thread(t, p.ID) {
		if m.Kind == domain.KindProgress {
			progress = append(progress, m)
		}
	}
	require.Len(t, progress, 1)
	require.NotNil(t, progress[0].Job)
	assert.Equal(t, domain.JobSucceeded, progress[0].Job.Status)
	assert.Equal(t, domain.JobPlan, progress[0].Job.Kind)
}

func TestHierarchy_CreateSceneAndShotByHand(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	ctx := context.Background()
	empty, err := f.svc.Plans.Hierarchy(ctx, p.ID, "", "")
	require.NoError(t, err)
	assert.Empty(t, empty.Scenes)
	assert.Nil(t, empty.Scene)

	v, err := f.svc.Plans.CreateScene(ctx, p.ID, service.SceneForm{Title: " 導入 ", Location: "スタジオ", Objective: "つかむ"})
	require.NoError(t, err)
	require.Len(t, v.Scenes, 1)
	require.NotNil(t, v.Scene)
	assert.Equal(t, "導入", v.Scene.Title)
	assert.Equal(t, "スタジオ", v.Scene.Location)
	sceneID := v.Scene.ID

	v, err = f.svc.Plans.CreateScene(ctx, p.ID, service.SceneForm{Title: "本編"})
	require.NoError(t, err)
	require.Len(t, v.Scenes, 2)
	assert.Equal(t, 1, v.Scene.OrderIndex, "a new Scene goes to the end and is selected")
	v, err = f.svc.Plans.CreateShot(ctx, sceneID, service.ShotForm{Objective: "商品を見せる", DurationSeconds: "3.5"})
	require.NoError(t, err)
	assert.Equal(t, sceneID, v.Scene.ID)
	require.Len(t, v.Shots, 1)
	assert.Equal(t, 3.5, v.Shots[0].Duration)
	assert.True(t, v.Shots[0].Selected)
	assert.Nil(t, v.Shots[0].Storyboard)
	assert.Equal(t, 1, v.Scenes[0].ShotCount)

	v, err = f.svc.Plans.CreateShot(ctx, sceneID, service.ShotForm{Objective: "笑顔", DurationSeconds: "2"})
	require.NoError(t, err)
	require.Len(t, v.Shots, 2)
	assert.Equal(t, 1, v.Shots[1].OrderIndex)

	// Selecting: the Scene by ID, the Shot by query.
	v, err = f.svc.Plans.Hierarchy(ctx, p.ID, sceneID, v.Shots[0].ID)
	require.NoError(t, err)
	assert.True(t, v.Shots[0].Selected)
	assert.False(t, v.Shots[1].Selected)
	notes := 0
	for _, m := range f.thread(t, p.ID) {
		if strings.Contains(m.Content, "追加しました") {
			notes++
		}
	}
	assert.Equal(t, 4, notes, "every addition is recorded in the thread")
}

func TestHierarchy_RejectsBadInputAndUnknownTargets(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	ctx := context.Background()
	v, err := f.svc.Plans.CreateScene(ctx, p.ID, service.SceneForm{Title: "S"})
	require.NoError(t, err)

	_, err = f.svc.Plans.CreateScene(ctx, p.ID, service.SceneForm{Title: "  "})
	var verr *service.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "title", verr.Field)
	_, err = f.svc.Plans.CreateScene(ctx, p.ID, service.SceneForm{Title: strings.Repeat("あ", 101)})
	assert.ErrorAs(t, err, &verr)

	for name, form := range map[string]service.ShotForm{
		"no objective":  {DurationSeconds: "3"},
		"no duration":   {Objective: "x"},
		"zero":          {Objective: "x", DurationSeconds: "0"},
		"negative":      {Objective: "x", DurationSeconds: "-1"},
		"not a number":  {Objective: "x", DurationSeconds: "abc"},
		"too long":      {Objective: "x", DurationSeconds: "61"},
		"nan":           {Objective: "x", DurationSeconds: "NaN"},
		"infinite":      {Objective: "x", DurationSeconds: "Inf"},
		"long question": {Objective: strings.Repeat("あ", 301), DurationSeconds: "3"},
	} {
		view, err := f.svc.Plans.CreateShot(ctx, v.Scene.ID, form)
		assert.ErrorAs(t, err, &verr, name)
		assert.Empty(t, view.Shots, name+": nothing was stored")
	}

	_, err = f.svc.Plans.CreateShot(ctx, "nope", service.ShotForm{Objective: "x", DurationSeconds: "3"})
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = f.svc.Plans.CreateScene(ctx, "nope", service.SceneForm{Title: "S"})
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = f.svc.Plans.Hierarchy(ctx, p.ID, "nope", "")
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = f.svc.Plans.Hierarchy(ctx, "nope", "", "")
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestHierarchy_SceneOfAnotherProjectIsNotFound(t *testing.T) {
	f := newFixture(t)
	a := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	b := f.createProject(t, "香水の縦型15秒CMを作りたい")
	ctx := context.Background()
	v, err := f.svc.Plans.CreateScene(ctx, a.ID, service.SceneForm{Title: "A の Scene"})
	require.NoError(t, err)

	_, err = f.svc.Plans.Hierarchy(ctx, b.ID, v.Scene.ID, "")
	assert.ErrorIs(t, err, service.ErrNotFound)
}
