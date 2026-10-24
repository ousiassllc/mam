package projectcore_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/projectcore"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/stage/stagetest"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func newEnv(t *testing.T) *mcptest.Env {
	return mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { projectcore.Register(r, env.Services) })
}

func create(t *testing.T, env *mcptest.Env, args map[string]any) projectcore.Project {
	t.Helper()
	var out projectcore.ProjectOutput
	env.CallOK("create_project", args, &out)
	return out.Project
}

func brief(t *testing.T, env *mcptest.Env, id string) projectcore.BriefOutput {
	t.Helper()
	var out projectcore.BriefOutput
	env.CallOK("get_brief", map[string]any{"project_id": id}, &out)
	return out
}

func TestCreateProject_StartsFromTheFirstMessage(t *testing.T) {
	env := newEnv(t)
	p := create(t, env, map[string]any{"content": "美容液の縦型15秒CMを作りたい。上品な雰囲気で"})

	assert.Equal(t, "DRAFT", p.State)
	assert.Equal(t, "auto", p.Mode)
	assert.Equal(t, "key_check", p.ConfirmationLevel)
	assert.Equal(t, "美容液", p.Name)

	var got projectcore.ProjectOutput
	env.CallOK("get_project", map[string]any{"project_id": p.ID}, &got)
	assert.Equal(t, p, got.Project)

	b := brief(t, env, p.ID)
	assert.Equal(t, "美容液", b.Fields["subject"])
	assert.Equal(t, "9:16", b.Fields["aspect_ratio"])
	assert.Equal(t, "15秒", b.Fields["duration"])
	assert.Contains(t, b.Missing, "objective")
	assert.NotContains(t, b.Missing, "subject")
}

func TestCreateProject_Validation(t *testing.T) {
	env := newEnv(t)
	assert.Contains(t, env.CallError("create_project", map[string]any{}), "validation: invalid input: content")
	assert.Contains(t, env.CallError("create_project", map[string]any{"content": "CM", "brand_template_id": "nope"}), "validation:")
	assert.Contains(t, env.CallError("create_project", map[string]any{"content": "CM", "attachment_upload_ids": []string{"nope"}}), "attachment_upload_ids")
	assert.Contains(t, env.CallError("get_project", map[string]any{"project_id": "nope"}), "not_found")
}

func TestCreateProject_TakesUploadedAttachmentsIntoTheBrief(t *testing.T) {
	env := newEnv(t)
	var ids []string
	for name, data := range map[string][]byte{"product.png": stagetest.PNG, "ref.mp4": stagetest.MP4} {
		status, staged := stagetest.Upload(env, projectcore.PurposeNewProjectAttachment, nil, name, data)
		require.Equal(t, http.StatusCreated, status)
		ids = append(ids, staged["upload_id"].(string))
	}

	p := create(t, env, map[string]any{"content": "商品CMを作りたい", "attachment_upload_ids": ids})
	b := brief(t, env, p.ID)
	require.Len(t, b.Assets, 1)
	assert.Equal(t, "image", b.Assets[0].Type)
	require.Len(t, b.ReferenceAssets, 1)
	assert.Equal(t, "video", b.ReferenceAssets[0].Type)

	assert.Contains(t, env.CallError("create_project", map[string]any{"content": "もう一度", "attachment_upload_ids": ids}),
		"attachment_upload_ids", "the files were used by the first Project")
}

func TestCreateProject_AFileTheServiceRefusesCreatesNothingAndStaysUsable(t *testing.T) {
	env := newEnv(t)
	status, staged := stagetest.Upload(env, projectcore.PurposeNewProjectAttachment, nil, "fake.png", []byte("not a png"))
	require.Equal(t, http.StatusCreated, status)
	args := map[string]any{"content": "商品CM", "attachment_upload_ids": []string{staged["upload_id"].(string)}}

	first := env.CallError("create_project", args)
	assert.Contains(t, first, "拡張子とファイルの内容が一致しません")
	assert.Equal(t, first, env.CallError("create_project", args), "the staged file was put back")
	projects, err := env.Services.Projects.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, projects)
}

func TestListNewProjectTemplates_AndStartingFromOne(t *testing.T) {
	env := newEnv(t)
	var out projectcore.ListTemplatesOutput
	env.CallOK("list_new_project_templates", map[string]any{}, &out)
	assert.NotNil(t, out.Templates, "an empty list is [], not null")
	assert.Empty(t, out.Templates)

	ctx := context.Background()
	source := create(t, env, map[string]any{"content": "美容液のCM"})
	_, err := env.Services.Brand.Add(ctx, source.ID, service.BrandForm{Type: string(domain.BrandForbidden), Text: "最高"})
	require.NoError(t, err)
	_, err = env.Services.Brand.SaveTemplate(ctx, source.ID, "コスメ用")
	require.NoError(t, err)

	env.CallOK("list_new_project_templates", map[string]any{}, &out)
	require.Len(t, out.Templates, 1)
	assert.Equal(t, "コスメ用", out.Templates[0].Name)
	assert.Equal(t, 1, out.Templates[0].ConstraintCount)

	p := create(t, env, map[string]any{"content": "化粧水のCM", "brand_template_id": out.Templates[0].ID})
	view, err := env.Services.Brand.List(ctx, p.ID)
	require.NoError(t, err)
	assert.Len(t, view.Constraints, 1, "the template's constraints were copied into the new Project")
}

func TestSetConfirmationLevel_ChangesTheLevelAndTellsTheThread(t *testing.T) {
	env := newEnv(t)
	p := create(t, env, map[string]any{"content": "美容液のCM"})

	var out projectcore.ProjectOutput
	env.CallOK("set_confirmation_level", map[string]any{"project_id": p.ID, "level": "custom_check", "checkpoints": []string{"final", "rough_cut"}}, &out)
	assert.Equal(t, "custom_check", out.Project.ConfirmationLevel)
	assert.Equal(t, []string{"rough_cut", "final"}, out.Project.CustomCheckpoints, "in pipeline order")

	env.CallOK("set_confirmation_level", map[string]any{"project_id": p.ID, "level": "full_auto"}, &out)
	assert.Equal(t, "full_auto", out.Project.ConfirmationLevel)
	assert.Equal(t, []string{"rough_cut", "final"}, out.Project.CustomCheckpoints, "remembered for the next custom_check")

	ws, err := env.Services.Chat.Workspace(context.Background(), p.ID)
	require.NoError(t, err)
	assert.Contains(t, ws.Messages[len(ws.Messages)-1].Content, "FULL AUTO")
}

func TestSetConfirmationLevel_Validation(t *testing.T) {
	env := newEnv(t)
	p := create(t, env, map[string]any{"content": "美容液のCM"})
	assert.Contains(t, env.CallError("set_confirmation_level", map[string]any{"project_id": p.ID, "level": "yolo"}), "validation: invalid input: level")
	assert.Contains(t, env.CallError("set_confirmation_level", map[string]any{"project_id": p.ID, "level": "custom_check", "checkpoints": []string{"scene_complete"}}), "validation: invalid input: checkpoints", "a point that posts no card cannot be chosen")
	assert.Contains(t, env.CallError("set_confirmation_level", map[string]any{"project_id": p.ID, "level": "custom_check", "checkpoints": []string{"nope"}}), "checkpoints")
	assert.Contains(t, env.CallError("set_confirmation_level", map[string]any{"project_id": "nope", "level": "full_auto"}), "not_found")
	var got projectcore.ProjectOutput
	env.CallOK("get_project", map[string]any{"project_id": p.ID}, &got)
	assert.Equal(t, "key_check", got.Project.ConfirmationLevel, "refused changes leave the level")
}

func TestUpdateBrief_ChangesOnlyWhatIsGivenAndKeepsTheRest(t *testing.T) {
	env := newEnv(t)
	p := create(t, env, map[string]any{"content": "美容液の縦型15秒CMを作りたい。上品な雰囲気で"})
	before := brief(t, env, p.ID)

	var out projectcore.BriefOutput
	env.CallOK("update_brief", map[string]any{
		"project_id": p.ID, "objective": "Instagram広告", "additional_instruction": "ロゴを入れない",
		"references": []string{"https://example.com/ref"},
	}, &out)
	assert.Equal(t, "Instagram広告", out.Fields["objective"])
	assert.Equal(t, "ロゴを入れない", out.AdditionalInstruction)
	assert.Equal(t, []string{"https://example.com/ref"}, out.References)
	for _, f := range []string{"subject", "duration", "aspect_ratio", "mood"} {
		assert.Equal(t, before.Fields[f], out.Fields[f], f+" was not given and stays")
	}
	assert.Equal(t, out, brief(t, env, p.ID), "saved")

	env.CallOK("update_brief", map[string]any{"project_id": p.ID, "objective": ""}, &out)
	assert.Contains(t, out.Missing, "objective", "an empty string makes the item undecided again")
	assert.Equal(t, []string{"https://example.com/ref"}, out.References, "references not given stay")
}

func TestUpdateBrief_KeepsVideoReferencesWhenTheURLsAreReplaced(t *testing.T) {
	env := newEnv(t)
	status, staged := stagetest.Upload(env, projectcore.PurposeNewProjectAttachment, nil, "ref.mp4", stagetest.MP4)
	require.Equal(t, http.StatusCreated, status)
	p := create(t, env, map[string]any{"content": "商品CM", "attachment_upload_ids": []string{staged["upload_id"].(string)}})

	out := brief(t, env, p.ID)
	require.Len(t, out.ReferenceAssets, 1)
	env.CallOK("update_brief", map[string]any{"project_id": p.ID, "references": []string{"https://example.com/a"}}, &out)
	assert.Len(t, out.ReferenceAssets, 1, "the uploaded video stays a reference")
	assert.Contains(t, out.References, "https://example.com/a")
}

func TestUpdateBrief_ValidatesLikeTheEditForm(t *testing.T) {
	env := newEnv(t)
	p := create(t, env, map[string]any{"content": "美容液のCM"})
	assert.Contains(t, env.CallError("update_brief", map[string]any{"project_id": p.ID, "aspect_ratio": "4:5"}), "validation: invalid input: aspect_ratio")
	assert.Contains(t, env.CallError("update_brief", map[string]any{"project_id": p.ID, "references": []string{"ftp://x"}}), "validation: invalid input: references")
	long := make([]rune, domain.MaxMessageRunes+1)
	for i := range long {
		long[i] = 'あ'
	}
	assert.Contains(t, env.CallError("update_brief", map[string]any{"project_id": p.ID, "additional_instruction": string(long)}), "additional_instruction")
	assert.Contains(t, env.CallError("update_brief", map[string]any{"project_id": "nope", "mood": "x"}), "not_found")
	assert.Contains(t, env.CallError("get_brief", map[string]any{"project_id": "nope"}), "not_found")
	assert.Equal(t, "", brief(t, env, p.ID).Fields["aspect_ratio"], "a refused update saves nothing")
}
