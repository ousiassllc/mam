package projectcore_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/projectcore"
)

// #1114: get_brief says where each value came from; update_brief makes only the fields it is given the user's, and confirm accepts the rest.
func TestBrief_SourcesAreReportedAndOnlyGivenFieldsBecomeTheUsers(t *testing.T) {
	env := newEnv(t)
	p := create(t, env, map[string]any{"content": "架空の30代女性インフルエンサーを主人公にした、TikTok向けの縦型動画を作りたい。テーマは朝のルーティン"})

	b := brief(t, env, p.ID)
	assert.Equal(t, "架空の30代女性インフルエンサー", b.Fields["subject"])
	assert.Equal(t, domain.BriefSourceInferred, b.Sources["subject"])
	assert.Equal(t, domain.BriefSourceInferred, b.Sources["aspect_ratio"], "the user said vertical")
	assert.Equal(t, domain.BriefSourceRecommended, b.Sources["duration"], "a proposal from the platform")
	assert.Equal(t, "15秒", b.Fields["duration"])
	assert.Equal(t, domain.BriefSourceInferred, b.Sources["additional_instruction"])
	assert.NotContains(t, b.Sources, "target", "no value, no source")

	var out projectcore.BriefOutput
	env.CallOK("update_brief", map[string]any{"project_id": p.ID, "mood": "ゆるい"}, &out)
	assert.Equal(t, domain.BriefSourceUser, out.Sources["mood"], "the field that was given")
	assert.Equal(t, domain.BriefSourceRecommended, out.Sources["duration"], "a field that was not given keeps its source")
	assert.Equal(t, domain.BriefSourceInferred, out.Sources["subject"])

	env.CallOK("update_brief", map[string]any{"project_id": p.ID, "duration": "30秒"}, &out)
	assert.Equal(t, domain.BriefSourceUser, out.Sources["duration"], "an explicit value wins over the recommendation")

	env.CallOK("update_brief", map[string]any{"project_id": p.ID, "confirm": true}, &out)
	for field, source := range out.Sources {
		assert.Equal(t, domain.BriefSourceUser, source, field)
	}
	assert.Equal(t, "架空の30代女性インフルエンサー", out.Fields["subject"])
}

// update_brief with confirm=true makes the extractor's theme and constraint lines the user's, but they never stand in for the Subject (#1114).
func TestBrief_ConfirmingAnInferredInstructionLeavesTheSubjectMissing(t *testing.T) {
	env := newEnv(t)
	p := create(t, env, map[string]any{"content": "成人で架空の設定。テーマは朝の散歩"})
	require.Contains(t, brief(t, env, p.ID).Missing, "subject")

	var out projectcore.BriefOutput
	env.CallOK("update_brief", map[string]any{"project_id": p.ID, "confirm": true}, &out)

	assert.Equal(t, domain.BriefSourceUser, out.Sources["additional_instruction"])
	assert.Equal(t, "テーマ: 朝の散歩\n制約: 成人・架空", out.AdditionalInstruction)
	assert.Contains(t, out.Missing, "subject")
}
