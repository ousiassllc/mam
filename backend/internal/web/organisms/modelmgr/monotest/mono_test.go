// Package monotest_test checks that the model manager says what is wrong in the quiet shape of the settings screens (Issue #1163): a cause is a
// settingsui.Banner or a settingsui.Note in the ink of the page, a state on a card is a BadgeMuted, and none of it is the yellow of text-warning,
// border-warning or bg-warning. Yellow stays only for the licence, commercial, unofficial and adult-content marks. It is a package of its own because the
// directories of the web layer are full (linterly).
package monotest_test

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/companion/facefusion"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/companion/ffmpeg"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/facecard"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/llmcard"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/loraimport"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/modeldetail"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/nodecard"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/ollamacard"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/runtimecard"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, c.Render(context.Background(), &b))
	return b.String()
}

// yes is the answer "commercial use is fine": the mark of a licence that is not clear is yellow on purpose, so the data of these tests carry none.
var yes = commercial.View{Level: modellicense.Yes}

// yellow are the classes of the warning colour: none of them is on a screen except for the marks of the licence, the commercial use, an unofficial
// build and adult content.
var yellow = []string{"text-warning", "border-warning", "bg-warning"}

func assertQuiet(t *testing.T, html string, testIDs ...string) {
	t.Helper()
	for _, id := range testIDs {
		assert.Contains(t, html, `data-testid="`+id+`"`, id)
	}
	for _, c := range yellow {
		assert.NotContains(t, html, c)
	}
}

// bannerOrNote reports whether the element with the test id is a settingsui.Banner or settingsui.Note: the thin line and the ink at the left, an icon in the muted ink.
func bannerOrNote(t *testing.T, html, testID string) {
	t.Helper()
	i := strings.Index(html, `data-testid="`+testID+`"`)
	require.GreaterOrEqual(t, i, 0, testID)
	open := strings.LastIndex(html[:i], "<")
	tag := html[open : i+len(testID)+14]
	assert.True(t, strings.Contains(tag, "border-l-2") || strings.Contains(tag, "text-ink"), "%s is a Banner or a Note: %s", testID, tag)
}

func TestTheEngineAndTheCatalogSayWhatIsWrongQuietly(t *testing.T) {
	html := render(t, view.CatalogList(view.Catalog{Blocked: "先に ComfyUI エンジンを導入してください", Models: []view.Model{{
		License: view.License{Answer: yes}, ID: "m", Name: "M", State: "incomplete", NeedsToken: true, NeedsNodes: "追加ノードが必要", PartialNote: "任意のファイルがありません",
		PlatformWarning: "この PC は対象外です", VRAMFit: "limited", LowDisk: false,
	}}}))
	assertQuiet(t, html, "mm-catalog-blocked", "mm-token-badge", "mm-nodes-badge", "mm-partial-badge", "mm-platform-warning", "mm-vram-fit")
	bannerOrNote(t, html, "mm-catalog-blocked")
	assert.Contains(t, html, "先に ComfyUI エンジンを導入してください")
	assert.Contains(t, html, "VRAM に余裕なし")

	d := render(t, modeldetail.Detail(view.Model{License: view.License{Answer: yes}, ID: "m", Name: "M", State: "incomplete", NeedsToken: true, TokenGuide: "HF のトークンを設定してください", NeedsNodes: "追加ノードが必要",
		PlatformWarning: "この PC は対象外です", PartialNote: "任意のファイルがありません", VRAMFit: "limited", VRAMFitText: "動くが制約あり"}))
	assertQuiet(t, d, "mm-token-guide", "mm-nodes-note", "mm-platform-warning-text", "mm-partial-note", "mm-vram-fit-text")
	for _, id := range []string{"mm-token-guide", "mm-nodes-note", "mm-platform-warning-text", "mm-partial-note"} {
		bannerOrNote(t, d, id)
	}
	assert.Contains(t, d, "HF のトークンを設定してください")

	assert.Equal(t, "text-ink", view.FitClass("limited"), "a GPU with no room to spare is said in the ink of the page")
	assert.Equal(t, "text-danger", view.FitClass("short"))
}

func TestTheEngineDetailSaysWhyItCannotRunInTheQuietShape(t *testing.T) {
	for name, tc := range map[string]struct {
		e  view.Engine
		id string
	}{
		"unsupported": {view.Engine{}, "mm-engine-unsupported"},
		"read-only":   {view.Engine{Supported: true, Dir: "/x", WritableError: "permission denied"}, "mm-engine-readonly"},
		"broken":      {view.Engine{Supported: true, Writable: true, Installed: true, Broken: true}, "mm-engine-broken"},
	} {
		t.Run(name, func(t *testing.T) {
			html := render(t, modeldetail.EngineDetail(tc.e))
			assertQuiet(t, html, tc.id)
			bannerOrNote(t, html, tc.id)
		})
	}
	assert.Contains(t, render(t, modeldetail.EngineDetail(view.Engine{Supported: true, Dir: "/x", WritableError: "permission denied"})), "permission denied")
}

func TestTheGPUAndTheTokenSayWhatIsWrongInTheQuietShape(t *testing.T) {
	g := render(t, view.GPUBlock(view.GPU{DetectError: "nvidia-smi がありません", Guidance: "対応する GPU が見つかりません"}))
	assertQuiet(t, g, "mm-gpu-error", "mm-gpu-guidance")
	bannerOrNote(t, g, "mm-gpu-error")
	assert.Contains(t, g, "nvidia-smi がありません")

	h := render(t, view.HFCard(view.HFToken{Unavailable: true}))
	assertQuiet(t, h, "mm-hf-unavailable")
	bannerOrNote(t, h, "mm-hf-unavailable")
}

func TestTheRuntimeSaysWhyItCannotRunInOneBannerAndKeepsTheConsentControls(t *testing.T) {
	unsupported := render(t, runtimecard.RuntimeDetail(runtimecard.Runtime{Commercial: yes, ID: "r", Name: "R", Reason: "この OS では動きません", Fallback: "クラウドを使ってください"}))
	assertQuiet(t, unsupported, "mm-runtime-unsupported", "mm-runtime-reason", "mm-runtime-fallback")
	assert.Equal(t, 1, strings.Count(unsupported, `data-testid="mm-runtime-unsupported"`), "one banner for the one cause")
	assert.Contains(t, unsupported, "この OS では動きません")

	readonly := render(t, runtimecard.RuntimeDetail(runtimecard.Runtime{Commercial: yes, ID: "r", Name: "R", Supported: true, Dir: "/x", WritableError: "denied"}))
	assertQuiet(t, readonly, "mm-runtime-readonly")
	assert.Contains(t, readonly, `role="alert"`)

	missing := render(t, runtimecard.RuntimeDetail(runtimecard.Runtime{Commercial: yes, ID: "ollama", Name: "Ollama", Supported: true, Writable: true, Installed: true, UpdateAvailable: true,
		PinnedVersion: "2", Version: "1"}))
	assertQuiet(t, missing, "mm-ollama-noauth")

	models := render(t, runtimecard.RuntimeModels(runtimecard.Runtime{Commercial: yes, ID: "r", Name: "R", Models: []runtimecard.Model{{
		ID: "m", Name: "M", VRAMFit: "足りない見込み", Cautions: []string{"本人の同意を得た音声だけ"},
		Consent: &runtimecard.ModelConsent{Version: "1", Text: "利用条件を読んでください"},
	}}}))
	assertQuiet(t, models, "mm-runtime-model-cautions", "mm-runtime-model-consent", "mm-runtime-model-agree")
	assert.Contains(t, models, "本人の同意を得た音声だけ")
	assert.Contains(t, models, "利用条件を読んでください")
	assert.Contains(t, models, `type="checkbox"`, "the consent box keeps its checkbox")
}

func TestAFileThatIsMissingIsAMutedChipOnEveryCard(t *testing.T) {
	card := render(t, runtimecard.RuntimeCard(runtimecard.Runtime{Commercial: yes, ID: "r", Name: "R", Supported: true, Writable: true, Installed: true, Broken: true}))
	assertQuiet(t, card)
	assert.Contains(t, card, "ファイルが見つかりません")

	hf := render(t, view.HFModelListView(view.HFModelList{Models: []view.HFModel{{Answer: yes, Name: "a.safetensors", Path: "p/a.safetensors", Missing: true, Gated: true}}}))
	assertQuiet(t, hf, "mm-hfi-model-state")
	assert.Contains(t, hf, "gated")

	ext := render(t, view.ExternalCard(view.External{Endpoints: []string{"http://127.0.0.1:8188"}, Result: &view.ExternalResult{Endpoint: "http://127.0.0.1:8188",
		Models: []view.ExternalModel{{Name: "m", Missing: []string{"vae.safetensors"}}}}}))
	assertQuiet(t, ext, "mm-external-model-state")
	assert.Contains(t, ext, "足りないファイルがあります")

	detail := render(t, modeldetail.ExternalDetail("http://127.0.0.1:8188", view.ExternalModel{Name: "m", Missing: []string{"vae.safetensors"}}))
	assertQuiet(t, detail, "mm-external-missing")
	bannerOrNote(t, detail, "mm-external-missing")
	assert.Contains(t, detail, "vae.safetensors")
}

func TestTheConflictOfALoRAIsOneAlertBanner(t *testing.T) {
	for _, same := range []bool{true, false} {
		html := render(t, loraimport.PanelView(loraimport.Panel{Staged: &loraimport.Staged{Token: "t", Name: "style.safetensors", Renamed: true, SavedAs: "style.safetensors",
			Conflict: &loraimport.Conflict{Name: "style.safetensors", Same: same}}}))
		assertQuiet(t, html, "mm-lora-conflict", "mm-lora-renamed")
		assert.Equal(t, 1, strings.Count(html, `data-testid="mm-lora-conflict"`))
		assert.Contains(t, html, `role="alert" data-testid="mm-lora-conflict"`)
		assert.Contains(t, html, "style.safetensors")
		assert.Contains(t, html, `data-testid="mm-lora-discard"`, "the buttons stay")
	}
}

func TestTheCompanionToolsSayWhatIsMissingWithAMutedChipAndANote(t *testing.T) {
	ff := render(t, ffmpeg.Section(ffmpeg.Data{
		FFmpeg:   ffmpeg.Tool{Label: "ffmpeg", Field: "ffmpeg", Problem: "見つかりません: PATH に ffmpeg がありません"},
		FFprobe:  ffmpeg.Tool{Label: "ffprobe", Field: "ffprobe", Problem: "見つかりません: PATH に ffprobe がありません"},
		Download: &ffmpeg.Download{Supported: true, Installed: true, Broken: true, UpdateAvailable: true, Version: "1", PinnedVersion: "2"},
	}))
	assertQuiet(t, ff, "ffmpeg-state-ffmpeg", "ffmpeg-state-ffprobe", "ffmpeg-download-broken", "ffmpeg-download-update")
	assert.Contains(t, ff, "壊れています")
	assert.Contains(t, ff, "PATH に ffmpeg がありません")
	bannerOrNote(t, ff, "ffmpeg-state-problem-ffmpeg")

	fu := render(t, facefusion.Section(facefusion.Data{
		Python: facefusion.Part{Label: "Python", Field: "python", Problem: "Python が見つかりません"},
		Path:   facefusion.Part{Label: "FaceFusion", Field: "path", Problem: "フォルダが見つかりません"}, VersionNote: "3.x の選択肢だけ確かめています",
	}))
	for _, c := range []string{"border-warning", "bg-warning"} {
		assert.NotContains(t, fu, c)
	}
	assert.Contains(t, fu, `data-testid="facefusion-notes"`)
	assert.Contains(t, fu, `data-testid="facefusion-state-python"`)
	assert.Contains(t, fu, `data-testid="mm-facefusion-version-note"`)
	assert.Contains(t, fu, "未設定")
	assert.Contains(t, fu, "Python が見つかりません")
	bannerOrNote(t, fu, "facefusion-state-problem-python")
	// the one yellow left in the section is the list of cautions about the licence, the commercial use and the adult pictures it declines
	assert.NotContains(t, regexp.MustCompile(`(?s)<ul[^>]*data-testid="facefusion-notes".*?</ul>`).ReplaceAllString(fu, ""), "text-warning")
}

func TestTheNodeOllamaAndFaceModelCardsAreQuiet(t *testing.T) {
	node := nodecard.Node{Commercial: yes, ID: "n", Name: "N", State: "outdated", Detail: "更新が必要です", SecurityNotice: "ノードはコードを動かします", Limits: "動画には向きません", Blocked: "Python が足りません"}
	assertQuiet(t, render(t, nodecard.NodeCard(node)), "mm-node-detail-note")
	assertQuiet(t, render(t, nodecard.NodeDetail(node)), "mm-node-security", "mm-node-blocked-detail", "mm-node-limits")
	assertQuiet(t, render(t, nodecard.ConsentDialog(node)), "mm-node-consent-security")
	assertQuiet(t, render(t, nodecard.NodeCard(nodecard.Node{Commercial: yes, ID: "n", Name: "N", Unavailable: "エンジンを入れてください"})), "mm-node-unavailable")

	m := ollamacard.Model{Commercial: yes, Name: "a:b", DisplayName: "A", State: ollamacard.StateChanged, NoSpace: true}
	assertQuiet(t, render(t, ollamacard.ModelCard(m)), "mm-ollama-changed", "mm-ollama-nospace", "mm-ollama-badge")
	assertQuiet(t, render(t, ollamacard.ModelDetail(ollamacard.Detail{Commercial: yes, Name: "a:b", DisplayName: "A", Check: "内容が変わっています", Changed: true})), "mm-ollama-tag-note", "mm-ollama-check")

	face := render(t, facecard.Section(facecard.Card{State: facecard.StateUnverified, OffReason: "runtime_unavailable", OffCause: "ライブラリがありません", OffFix: "入れ直してください"}))
	assertQuiet(t, face, "mm-face-models-runtime")
	assert.Contains(t, face, "ライブラリがありません")
}

func TestTheLocalLLMPinnedAndProblemLinesAreQuiet(t *testing.T) {
	html := render(t, llmcard.UseSection(llmcard.Use{Pinned: true, Problem: "読めません", State: llmcard.State{Line: "今は llama.cpp に呼び出します", Problem: "起動できません", Kind: "attention"}}))
	assertQuiet(t, html, "mm-llm-use-pinned", "mm-llm-use-state-problem")
	bannerOrNote(t, html, "mm-llm-use-pinned")
	assert.Contains(t, html, "起動できません")
	problem := render(t, llmcard.UseSection(llmcard.Use{Problem: "読めません"}))
	assertQuiet(t, problem, "mm-llm-use-problem")
	assert.Contains(t, problem, `role="alert" data-testid="mm-llm-use-problem"`)
	assert.Contains(t, problem, "読めません")
}
