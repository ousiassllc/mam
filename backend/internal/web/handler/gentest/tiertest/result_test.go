package tiertest_test

import (
	"context"
	"database/sql"
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// made runs a request on the rig and returns the id of its first result.
func (s *screen) made(req gentest.Request) string {
	s.t.Helper()
	ctx := s.t.Context()
	est, err := s.rig.Svc.Estimate(ctx, req)
	require.NoError(s.t, err)
	ref, err := s.rig.Svc.Run(ctx, req, gentest.Confirmation{EstimateID: est.ID, TotalUSD: est.TotalUSD})
	require.NoError(s.t, err)
	var st gentest.BatchStatus
	require.Eventually(s.t, func() bool {
		st, err = s.rig.Svc.Batch(ctx, ref.BatchID, true)
		return err == nil && st.Finished
	}, 30*time.Second, 20*time.Millisecond, "the batch did not end")
	require.Len(s.t, st.Results, 1)
	require.Equal(s.t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	return st.Results[0].ID
}

// A result made with a fixed seed, 30 steps and a sampler of the user's, in a tall picture.
func sdxlRequest() gentest.Request {
	seed, steps := int64(42), 30
	return gentest.Request{
		Media: gentest.MediaImage, Models: []string{string(gentesttest.SamplingModel)}, Prompts: []gentest.PromptInput{{Prompt: "a red fox", Negative: "blurry"}},
		AspectRatio: "9:16", Seed: &seed, Sampling: sampling.Settings{Steps: &steps, Sampler: "euler"}, Count: 1,
	}
}

// 結果の下に 5 つのボタンが並び、「使った設定」が折りたたみで出る（モデル・steps・cfg・サンプラー・スケジューラー・seed・LoRA・サイズ）。
func TestAnImageResultOffersTheButtonsAndTheSettingsItWasMadeWith(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels())
	id := s.made(sdxlRequest())
	body := s.page(paths.GenerationTestPart("results", id))

	row := testid(body, "gt-next")
	require.NotEmpty(t, row)
	for _, want := range []string{"一部だけ描き直す", "大きくする", "似た感じでもう1枚", "この顔で作る", "この設定でもう一度"} {
		assert.Contains(t, row, want)
	}
	used := testid(body, "gt-used")
	require.NotEmpty(t, used)
	assert.Contains(t, used, "<summary")
	assert.NotContains(t, body[strings.LastIndex(body[:strings.Index(body, `data-testid="gt-used"`)], "<details"):strings.Index(body, `data-testid="gt-used"`)], " open", "folded")
	for _, want := range []string{"bundled-comfyui/sdxl", ">30<", ">7<", "euler", "karras", ">42<", "なし", "縦（9:16）"} {
		assert.Contains(t, used, want)
	}
}

// 設定の記録が無い結果（#973 より前の履歴）は、モデルが受け取る項目でも「受け取りません」と言わず、記録が無いと言う。
func TestAResultWithoutRecordedSamplingSaysItIsOldWhenTheModelTakesIt(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels())
	id := s.made(sdxlRequest())
	require.NoError(t, s.rig.DB.Write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE gentest_result SET params = json_remove(params, '$.sampling') WHERE id = ?`, id)
		return err
	}))

	used := testid(s.page(paths.GenerationTestPart("results", id)), "gt-used")

	assert.Contains(t, used, "この結果には記録がありません（古い結果）")
	assert.NotContains(t, used, "このモデルは受け取りません")
}

// inputTag is the first <input> of the page with the name.
func inputTag(body, name string) string {
	return regexp.MustCompile(`<input[^>]*name="` + name + `"[^>]*>`).FindString(body)
}

// 「この設定でもう一度」: 同じ設定・同じシード。「似た感じでもう1枚」: 同じ設定でシードだけ空（ランダム）。
func TestReplayKeepsTheSeedAndSimilarLeavesItToChance(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels())
	id := s.made(sdxlRequest())

	same := s.page(paths.GenerationTest() + "?" + url.Values{"tier": {"detail"}, "again": {id}, "same_seed": {"1"}}.Encode())
	assert.Contains(t, inputTag(same, "seed"), `value="42"`)
	assert.Contains(t, inputTag(same, "steps"), `value="30"`, "the steps it ran with")
	assert.Contains(t, same, `<option value="euler" selected>`, "the sampler it ran with")
	assert.Contains(t, inputTag(same, "aspect_ratio"), `value="9:16"`)
	assert.Contains(t, same, `>a red fox</textarea>`)
	assert.Contains(t, same, `>blurry</textarea>`)
	assert.Contains(t, same, `name="model" value="`+string(gentesttest.SamplingModel)+`" checked`)

	similar := s.page(paths.GenerationTest() + "?" + url.Values{"tier": {"detail"}, "again": {id}}.Encode())
	assert.NotContains(t, inputTag(similar, "seed"), `value="42"`, "a new seed")
	assert.Contains(t, inputTag(similar, "steps"), `value="30"`)
}

// アップロードした入力画像はバッチが残した写しから戻る（FromBatchInput）。元の履歴の画像が消えたものだけ、黙って捨てずに理由を出す。
func TestAnUploadedInputIsPutBackFromTheBatchCopyAndWhatIsGoneIsSaid(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(""))
	edit := string(gentesttest.EditModel)
	source := s.made(gentest.Request{Media: gentest.MediaImage, Models: []string{edit}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}})
	staged, _, err := s.rig.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	made := s.made(gentest.Request{
		Media: gentest.MediaImage, Models: []string{edit}, Prompts: []gentest.PromptInput{{Prompt: "the fox, at night"}},
		References: []gentest.RefInput{
			{Source: gentest.FromHistory, ID: source, Role: gentest.RoleFace},
			{Source: gentest.FromStaged, ID: staged, Role: gentest.RoleReference},
		},
	})
	again := paths.GenerationTest() + "?" + url.Values{"tier": {"detail"}, "again": {made}}.Encode()
	detail, err := s.rig.Svc.Result(t.Context(), made)
	require.NoError(t, err)
	var uploaded string
	for _, in := range detail.Inputs {
		if in.Origin.Kind == gentest.FromStaged {
			uploaded = in.ID
		}
	}
	require.NotEmpty(t, uploaded)

	whole := s.page(again)
	assert.NotContains(t, testid(whole, "gt-form"), "アップロードした入力画像", "an upload is not lost any more")
	assert.Contains(t, whole, `value="batch_input::`+uploaded+`"`, "the uploaded picture is put back from the copy of its batch")
	assert.Contains(t, whole, `src="`+paths.GenerationTestPart("inputs", uploaded, "file")+`"`, "and shown")
	assert.Contains(t, whole, `value="history::`+source+`"`, "the picture of the history is put back")

	again2 := s.made(gentest.Request{
		Media: gentest.MediaImage, Models: []string{edit}, Prompts: []gentest.PromptInput{{Prompt: "the fox, at dawn"}},
		References: []gentest.RefInput{{Source: gentest.FromBatchInput, ID: uploaded, Role: gentest.RoleReference}},
	})
	d2, err := s.rig.Svc.Result(t.Context(), again2)
	require.NoError(t, err)
	require.Len(t, d2.Inputs, 1)
	assert.Equal(t, gentest.FromBatchInput, d2.Inputs[0].Origin.Kind, "the new result says where its picture came from")
	redo := s.page(paths.GenerationTest() + "?" + url.Values{"tier": {"detail"}, "again": {again2}}.Encode())
	assert.Contains(t, redo, `value="batch_input::`+d2.Inputs[0].ID+`"`, "and can be put back again, from its own copy")

	require.NoError(t, s.rig.Svc.DeleteResult(t.Context(), source))
	gone := s.page(again)
	assert.Contains(t, testid(gone, "gt-form"), "見つからないため入れていません")
	assert.NotContains(t, gone, `value="history::`+source+`"`)
	assert.Contains(t, gone, `value="batch_input::`+uploaded+`"`, "the upload does not depend on it")

}

// 何も触らずに作った結果と、品質を決めた結果: 標準は上書きなしなので、ボタンの「使った設定」にテンプレートの値が出る。
func TestTheSettingsPanelShowsTheTemplatesOwnValuesForAnUntouchedRequest(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels())
	id := s.made(gentest.Request{Media: gentest.MediaImage, Models: []string{string(gentesttest.SamplingModel)}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}})
	used := testid(s.page(paths.GenerationTestPart("results", id)), "gt-used")
	for _, want := range []string{">25<", ">7<", "dpmpp_2m", "karras", "正方形（1:1）"} {
		assert.Contains(t, used, want)
	}
}

// 「この顔で作る」: 結果の画像を顔の役割（face）で参照に入れ、かんたんの「同じ顔で」が開く。顔を読めるモデルが選ばれる。
func TestUseThisFaceOpensTheSameFaceIntentWithTheResultAsAFaceReference(t *testing.T) {
	s := newScreen(t, installed("sdxl-base", "qwen-image-edit-2511"))
	id := s.made(gentest.Request{Media: gentest.MediaImage, Models: []string{gentesttest.InstalledID("sdxl-base")}, Prompts: []gentest.PromptInput{{Prompt: "a woman"}}})
	detail := s.page(paths.GenerationTestPart("results", id))
	link := regexp.MustCompile(`href="([^"]*)"[^>]*data-testid="gt-next-face"|data-testid="gt-next-face"[^>]*href="([^"]*)"`).FindStringSubmatch(detail)
	require.NotNil(t, link)
	href := html.UnescapeString(link[1] + link[2])

	body := s.page(href)
	assert.Contains(t, testid(body, "gt-easy-model"), "qwen-image-edit-2511", "a model that reads the face")
	assert.Contains(t, body, `value="history::`+id+`"`, "the result is an input")
	assert.Regexp(t, `<option value="face" selected>`, body, "with the role face")
	assert.Regexp(t, `checked[^>]*name="intent"[^>]*value="face"`, body)
}
