package relay_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/generation/relay"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

func savedTemplate(t *testing.T, id string) ([]byte, localcfg.WorkflowSpec) {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	w := tpl.Model.Workflow
	return w.Graph, localcfg.WorkflowSpec{Assignments: w.Assignments, OutputNode: w.OutputNode, FPS: w.FPS, FramesStep: w.FramesStep, SizeMultiple: w.SizeMultiple, Video: tpl.Model.Video}
}

// A workflow saved in mam takes the whole path of a cloud model: the Reference Pack's images and the Take's video
// come from the ComfyUI, take.model_name and the Cost Ledger carry comfy:<id>@v<N>, editing the workflow makes the
// next Take name the next version while the Takes made before keep theirs, and a Shot's Video Model lock pins a
// version: the Quality Policy's route is then not used (no "Different Model").
func TestSavedWorkflowsRunReferencePackAndTakesAndTheTakesKeepTheirVersion(t *testing.T) {
	ctx := context.Background()
	comfy := comfyuitest.New(t)
	db := testdb.Open(t)
	repo := comfywf.New(db)
	t.Cleanup(func() {
		localcfg.PublishWorkflows(nil)
		localbackends.Publish(modeladapter.DefaultConfig())
	})
	require.NoError(t, workflows.Publish(ctx, repo, nil))

	path, keys := filepath.Join(t.TempDir(), "models.json"), credentialtest.New()
	cfg, err := configcheck.Load("")
	require.NoError(t, err)
	opts := localbackends.Options{PollEvery: time.Millisecond, StagingDir: t.TempDir()}
	adapter := modeladapter.New(cfg, localbackends.Build(cfg, keys, opts))
	localbackends.Publish(cfg)
	local := localmodels.New(localmodels.Deps{Path: path, Keys: keys, Adapter: adapter, Backends: opts})
	settings := modelsettings.New(path, false)
	settings.SetOnChange(func() { require.NoError(t, local.Reload()) })
	require.NoError(t, local.CreateEndpoint(ctx, localmodels.EndpointInput{Name: workflows.BundledEndpoint, Kind: localcfg.KindComfyUI, BaseURL: comfy.URL()}))
	svc := workflows.New(workflows.Deps{Repo: repo, Models: local, Adapter: adapter, Keys: keys, StagingDir: t.TempDir(), RunsDir: t.TempDir()})

	save := func(name string, kind localcfg.Media, template string) workflows.Workflow {
		graph, spec := savedTemplate(t, template)
		w, err := svc.Create(ctx, workflows.CreateInput{Name: name, Kind: kind, API: graph, Spec: &spec})
		require.NoError(t, err)
		require.True(t, w.Ready, "%v", w.Problems)
		return w
	}
	txt, edit := save("Stills", localcfg.MediaImage, "txt2img-sdxl"), save("Edits", localcfg.MediaImage, "img2img-sdxl")
	clip := save("Clip", localcfg.MediaVideo, "img2video-wan22-5b")

	view, err := settings.View()
	require.NoError(t, err)
	in := modelsettings.Input{Images: view.Images, Policies: view.Policies}
	still, editing := modelsettings.ImageTarget{Model: domain.ModelName(txt.ModelID)}, modelsettings.ImageTarget{Model: domain.ModelName(edit.ModelID)}
	in.Images[modelsettings.PurposeNewReference] = modelsettings.ImageRoute{Primary: still, Fallback: still}
	in.Images[modelsettings.PurposeEditReference] = modelsettings.ImageRoute{Primary: editing, Fallback: editing}
	target := modelsettings.VideoTarget{Model: domain.ModelName(clip.ModelID), Resolution: "480p"}
	in.Policies[domain.QualityBalanced] = modelsettings.PolicyRoute{Primary: target, Fallback: target, Takes: 1}
	require.NoError(t, settings.Save(in), "a saved workflow is a choice for image purposes and Quality Policies")

	projects, plans, gens := repository.NewProjectRepository(db), repository.NewPlanRepository(db), repository.NewGenerationRepository(db)
	projectID := domain.NewID()
	require.NoError(t, projects.Create(ctx, repository.NewProject{
		Project: domain.Project{ID: projectID, State: domain.StatePlanning, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmKeyCheck,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced},
		Brief:    domain.Brief{Subject: "美容液"},
		Messages: []domain.ChatMessage{{ProjectID: projectID, Role: domain.RoleUser, Kind: domain.KindText, Content: "美容液のCMを作りたい"}},
	}))
	require.NoError(t, plans.SavePlan(ctx, projectID,
		domain.ScenePlan{Scenes: []domain.PlannedScene{{Title: "Street", Shots: []domain.PlannedShot{{Objective: "主人公が走り出す", DurationSeconds: 4}}}}},
		domain.Storyboard{Shots: [][]domain.ShotStoryboard{{{Description: "x"}}}}))
	scenes, err := plans.ListScenes(ctx, projectID)
	require.NoError(t, err)
	shots, err := plans.ListShots(ctx, scenes[0].ID)
	require.NoError(t, err)
	shotID := shots[0].ID

	log, queue := &progressLog{}, &jobs{}
	progress := relay.New(log.add)
	orch := generation.New(generation.Deps{
		Generations: gens, World: repository.NewWorldRepository(db), Projects: projects, Policies: repository.NewPolicyRepository(db),
		Assets: repository.NewAssetRepository(db), Repairs: repository.NewRepairRepository(db), Chat: chat{}, Jobs: queue, LLM: reasoner{},
		Adapter: adapter, AssetsDir: t.TempDir(), FreeBytes: func(string) (uint64, error) { return 1 << 40, nil }, Notify: progress.Notify,
	})
	handle := progress.Wrap(orch.Handle)
	run := func(start func() (domain.Job, error)) {
		job, err := start()
		require.NoError(t, err)
		require.NoError(t, handle(ctx, job))
	}
	models := func() []string {
		rows, err := db.Reader().Query(`SELECT model_name FROM take ORDER BY created_at, rowid`)
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var m string
			require.NoError(t, rows.Scan(&m))
			out = append(out, m)
		}
		return out
	}

	// Version 1 makes the Reference Pack and the first Take.
	run(func() (domain.Job, error) { return orch.StartReferencePack(ctx, shotID) })
	assert.Equal(t, 1, countRows(t, db, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = ? AND shot_id = ?`, txt.ModelID, shotID))
	assert.Equal(t, 1, countRows(t, db, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = ? AND shot_id = ?`, edit.ModelID, shotID))
	run(func() (domain.Job, error) { return orch.Start(ctx, shotID, "") })
	assert.Equal(t, []string{clip.ModelID}, models(), "take.model_name names the workflow and its version")
	assert.Equal(t, 1, countRows(t, db, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = ? AND estimated_cost = 0`, clip.ModelID), "a free run still gets its ledger row")

	// A Shot pinned to version 1 (the Video Model lock) keeps it when the workflow is edited into version 2: the older
	// version stays a Video Model for the Shot, and runs from its snapshot though the policy now routes to version 2.
	require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode, value) VALUES ('pin', ?, 'shot', ?, 'video_model', 'lock', ?)`,
			projectID, shotID, `"`+clip.ModelID+`"`)
		return err
	}))
	_, spec := savedTemplate(t, "img2video-wan22-5b")
	spec.SizeMultiple += 16
	clip2, err := svc.Update(ctx, clip.ID, workflows.UpdateInput{Spec: &spec})
	require.NoError(t, err)
	require.Equal(t, 2, clip2.Version)
	assert.NotEqual(t, clip.ModelID, clip2.ModelID)
	run(func() (domain.Job, error) { return orch.Start(ctx, shotID, "") })
	assert.Equal(t, []string{clip.ModelID, clip.ModelID}, models(), "a pinned older version runs from its snapshot")

	// Unpinned, the route follows the edit and the earlier Takes keep version 1.
	require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE lock SET mode = 'auto' WHERE id = 'pin'`)
		return err
	}))
	run(func() (domain.Job, error) { return orch.Start(ctx, shotID, "") })
	assert.Equal(t, []string{clip.ModelID, clip.ModelID, clip2.ModelID}, models(), "the older Takes are unchanged")
}
