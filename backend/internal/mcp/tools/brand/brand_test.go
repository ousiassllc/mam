package brand_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/brand"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func newEnv(t *testing.T) (*mcptest.Env, string) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { brand.Register(r, env.Services) })
	return env, newProject(t, env, "春の新作スニーカーの 15 秒 PV")
}

func newProject(t *testing.T, env *mcptest.Env, content string) string {
	t.Helper()
	p, err := env.Services.Projects.Create(context.Background(), service.CreateProjectInput{Content: content})
	require.NoError(t, err)
	return p.ID
}

// add calls add_brand_constraint once: the constraints on success, the error text on failure.
func add(env *mcptest.Env, id string, args map[string]any) (out brand.ConstraintsOutput, errText string) {
	env.T.Helper()
	args["project_id"] = id
	res := env.Call("add_brand_constraint", args)
	if res.IsError {
		return out, mcptest.Text(res)
	}
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(env.T, err)
	require.NoError(env.T, json.Unmarshal(raw, &out))
	return out, ""
}

func TestBrandConstraints_AddListDeleteWithTheScreensValidation(t *testing.T) {
	env, id := newEnv(t)

	var out brand.ConstraintsOutput
	env.CallOK("list_brand_constraints", map[string]any{"project_id": id}, &out)
	assert.NotNil(t, out.Constraints)
	assert.Empty(t, out.Constraints)

	out, errText := add(env, id, map[string]any{"type": "forbidden_expression", "text": "世界一\n業界No.1"})
	require.Empty(t, errText)
	require.Len(t, out.Constraints, 1)
	assert.Equal(t, "lock", out.Constraints[0].Level, "an omitted level is the strict one, as on the screen")
	assert.Equal(t, "forbidden_expression", out.Constraints[0].Type)

	out, errText = add(env, id, map[string]any{"type": "required_disclaimer", "level": "advisory", "text": "効果には個人差があります"})
	require.Empty(t, errText)
	require.Len(t, out.Constraints, 2)

	for name, args := range map[string]map[string]any{
		"unknown type":  {"type": "nonsense", "text": "x"},
		"unknown level": {"type": "brand_guideline", "level": "strict", "text": "x"},
		"blank text":    {"type": "brand_guideline", "text": "   "},
		"too long":      {"type": "brand_guideline", "text": strings.Repeat("あ", 1001)},
	} {
		_, errText := add(env, id, args)
		assert.True(t, strings.HasPrefix(errText, "validation:"), "%s: %s", name, errText)
	}
	_, errText = add(env, "missing", map[string]any{"type": "brand_guideline", "text": "x"})
	assert.True(t, strings.HasPrefix(errText, "not_found:"), errText)

	env.CallOK("delete_brand_constraint", map[string]any{"constraint_id": out.Constraints[0].ID}, &out)
	require.Len(t, out.Constraints, 1)
	assert.Equal(t, "required_disclaimer", out.Constraints[0].Type)
	assert.Contains(t, env.CallError("delete_brand_constraint", map[string]any{"constraint_id": "missing"}), "not_found:")
	assert.Contains(t, env.CallError("list_brand_constraints", map[string]any{"project_id": "missing"}), "not_found:")
}

func TestBrandTemplates_SaveApplyDelete(t *testing.T) {
	env, id := newEnv(t)
	other := newProject(t, env, "カフェの開店告知ショート動画")

	assert.Contains(t, env.CallError("save_brand_template", map[string]any{"project_id": id, "name": "empty"}), "validation:",
		"a Project without constraints cannot be saved as a template")

	_, errText := add(env, id, map[string]any{"type": "forbidden_expression", "text": "世界一"})
	require.Empty(t, errText)
	_, errText = add(env, id, map[string]any{"type": "platform_rule", "level": "advisory", "text": "9:16 で作る"})
	require.Empty(t, errText)

	var tpls brand.TemplatesOutput
	env.CallOK("save_brand_template", map[string]any{"project_id": id, "name": "  食品  ブランド "}, &tpls)
	require.Len(t, tpls.Templates, 1)
	assert.Equal(t, "食品 ブランド", tpls.Templates[0].Name, "white space is collapsed as on the screen")
	assert.Len(t, tpls.Templates[0].Constraints, 2)
	tplID := tpls.Templates[0].ID

	assert.Contains(t, env.CallError("save_brand_template", map[string]any{"project_id": id, "name": "食品 ブランド"}), "validation:", "a taken name")
	assert.Contains(t, env.CallError("save_brand_template", map[string]any{"project_id": id, "name": strings.Repeat("a", 101)}), "validation:")

	var applied brand.ConstraintsOutput
	env.CallOK("apply_brand_template", map[string]any{"project_id": other, "template_id": tplID}, &applied)
	assert.Equal(t, other, applied.ProjectID)
	require.Len(t, applied.Constraints, 2)
	env.CallOK("apply_brand_template", map[string]any{"project_id": other, "template_id": tplID}, &applied)
	assert.Len(t, applied.Constraints, 2, "applying again adds only what the Project lacks")
	assert.Contains(t, env.CallError("apply_brand_template", map[string]any{"project_id": other, "template_id": "missing"}), "not_found:")
	assert.Contains(t, env.CallError("apply_brand_template", map[string]any{"project_id": "missing", "template_id": tplID}), "not_found:")

	env.CallOK("list_brand_templates", map[string]any{"project_id": other}, &tpls)
	require.Len(t, tpls.Templates, 1)

	env.CallOK("delete_brand_template", map[string]any{"project_id": id, "template_id": tplID}, &tpls)
	assert.NotNil(t, tpls.Templates)
	assert.Empty(t, tpls.Templates)
	env.CallOK("list_brand_constraints", map[string]any{"project_id": other}, &applied)
	assert.Len(t, applied.Constraints, 2, "Projects that used the template keep their constraints")
	assert.Contains(t, env.CallError("delete_brand_template", map[string]any{"project_id": id, "template_id": tplID}), "not_found:")
}
