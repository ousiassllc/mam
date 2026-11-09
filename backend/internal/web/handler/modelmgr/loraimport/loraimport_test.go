package loraimport_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile/lorafiletest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

var (
	inspectURL = paths.ModelManagerPart("loras", "inspect")
	lorasURL   = paths.ModelManagerPart("loras")
)

func lorasDir(s *screentest.Screen) string {
	return filepath.Join(s.Rig.Layout.DataDir(), "models", "loras")
}

func token(t *testing.T, body string) string {
	m := regexp.MustCompile(`name="token" value="([0-9a-f]+)"`).FindStringSubmatch(body)
	require.Len(t, m, 2, body)
	return m[1]
}

// ticked says the checkbox of the base model key is checked in the form.
func ticked(form, key string) bool {
	tag := regexp.MustCompile(`<input[^>]*name="base_models"[^>]*value="` + regexp.QuoteMeta(key) + `"[^>]*>`).FindString(form)
	return strings.Contains(tag, " checked")
}

func commit(tok string, extra url.Values) url.Values {
	f := url.Values{"token": {tok}}
	for k, v := range extra {
		f[k] = v
	}
	return f
}

// The entry point is on the models page, with the button, the drop zone and the limits; nothing is generated and the list starts empty.
func TestTheModelsPageHasTheEntryPoint(t *testing.T) {
	s := screentest.New(t)
	body := s.PageOf("image-video")
	assert.Contains(t, body, `data-testid="mm-lora"`)
	assert.Contains(t, body, ">ファイルを追加<")
	assert.Contains(t, body, `data-lora-zone`)
	assert.Contains(t, body, `accept=".safetensors"`)
	assert.Contains(t, body, "まだ追加していません")
	assert.Contains(t, body, `hx-post="`+inspectURL+`"`)
}

// A valid file: checked, the form shows what the file says of itself, the user adds it, and it appears in the list — no job is started and the
// existing Hugging Face section still answers.
func TestUploadACompleteLoRAEndToEnd(t *testing.T) {
	s := screentest.New(t)
	file := lorafiletest.LoRA(map[string]string{
		"ss_base_model_version": "sdxl_base_v1-0", "modelspec.trigger_phrase": "mychar, red dress", "ss_tag_frequency": `{"a":{"solo":9,"1girl":8}}`,
		"modelspec.title": "My Char",
	})
	rec := s.Upload(inspectURL, "file", "my char.safetensors", file)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	form := rec.Body.String()
	assert.Contains(t, form, `data-testid="mm-lora-staged"`)
	assert.Contains(t, form, "my char.safetensors")
	assert.Contains(t, form, "my_char.safetensors", "the name it would get")
	assert.Contains(t, form, "sdxl_base_v1-0", "what the file says of the base model, as written")
	assert.Contains(t, form, "ファイルの情報から読み取りました")
	assert.True(t, ticked(form, "sdxl"), "the base model the file names is ticked")
	assert.False(t, ticked(form, "flux"), "the others are not")
	assert.Contains(t, form, "mychar, red dress", "the trigger words the file names fill the field")
	assert.Contains(t, form, "solo、1girl", "the tags are a hint only")
	assert.Contains(t, form, "候補です。トリガー語かどうかは分かりません")
	assert.NotContains(t, form, `data-testid="mm-lora-conflict"`)
	assert.NoFileExists(t, filepath.Join(lorasDir(s), "my_char.safetensors"), "not placed before the user confirms")

	tok := token(t, form)
	rec = s.Do(http.MethodPost, lorasURL, commit(tok, url.Values{"name": {"my_char.safetensors"}, "base_models": {"sdxl"}, "strength": {"0.7"}, "trigger_words": {"mychar"}, "license_name": {"My own"}, "commercial": {""}}))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	out := rec.Body.String()
	assert.Contains(t, out, "「my_char.safetensors」を追加しました")
	assert.Contains(t, out, "生成テストの「LoRA と強さ」で選べます")
	assert.Contains(t, out, `href="/generation-test"`)
	assert.Contains(t, out, `hx-swap-oob="true"`, "the list is drawn again")
	assert.Contains(t, out, `data-testid="mm-lora-item"`)
	got, err := os.ReadFile(filepath.Join(lorasDir(s), "my_char.safetensors"))
	require.NoError(t, err)
	assert.Equal(t, file, got)

	page := s.PageOf("image-video")
	assert.Contains(t, page, `data-path="models/loras/my_char.safetensors"`)
	assert.Regexp(t, `(?s)mm-lora-item-strength">0\.7<`, page)
	assert.Contains(t, page, "mychar")
	assert.Contains(t, s.PageOf("hf-external"), `data-testid="mm-hfi"`, "the import from Hugging Face is still there")
}

// A file with no metadata: every field of the form is empty and the page says 未設定 / 不明; nothing is guessed from the name or the tensors, and the license is never
// commercial use OK.
func TestAFileWithoutMetadataShowsUnsetAndUnknown(t *testing.T) {
	s := screentest.New(t)
	rec := s.Upload(inspectURL, "file", "plain.safetensors", lorafiletest.LoRA(nil))
	require.Equal(t, http.StatusOK, rec.Code)
	form := rec.Body.String()
	assert.Contains(t, form, "ファイルに書かれていないため、未設定です")
	for _, k := range []string{"sdxl", "sd1", "sd3", "flux", "chroma", "qwen-image", "wan"} {
		assert.False(t, ticked(form, k), "no base model is ticked: %s", k)
	}
	assert.NotContains(t, form, "mm-lora-candidates")
	tok := token(t, form)
	require.Equal(t, http.StatusCreated, s.Do(http.MethodPost, lorasURL, commit(tok, url.Values{"name": {"plain.safetensors"}})).Code)

	page := s.PageOf("image-video")
	item := regexp.MustCompile(`(?s)<li[^>]*data-testid="mm-lora-item".*?</li>`).FindString(page)
	require.NotEmpty(t, item)
	for _, id := range []string{"mm-lora-item-base", "mm-lora-item-strength", "mm-lora-item-triggers"} {
		assert.Regexp(t, `data-testid="`+id+`">未設定<`, item)
	}
	assert.Regexp(t, `data-testid="mm-lora-item-license">不明<`, item)
	assert.Contains(t, item, `data-commercial="unknown"`)
	assert.NotContains(t, item, `data-commercial="yes"`)
}

// What is not a LoRA .safetensors is refused with the reason in the panel, and nothing is placed.
func TestAnInvalidFileIsRefusedWithTheReason(t *testing.T) {
	s := screentest.New(t)
	for name, tc := range map[string]struct {
		file string
		body []byte
		msg  string
	}{
		"text renamed":   {"x.safetensors", lorafiletest.Garbage(), ".safetensors の形式ではありません"},
		"truncated":      {"x.safetensors", lorafiletest.Truncated(), ".safetensors の形式ではありません"},
		"a checkpoint":   {"x.safetensors", lorafiletest.Checkpoint(), "LoRA ではなく"},
		"a pickle":       {"x.ckpt", lorafiletest.LoRA(nil), "pickle"},
		"path traversal": {"../../x.safetensors", lorafiletest.LoRA(nil), "使えない文字"},
		"empty":          {"x.safetensors", []byte{}, "空のファイル"},
		"not a LoRA":     {"x.safetensors", lorafiletest.File([]string{"foo"}, nil), "LoRA のファイルとして扱えません"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := s.Upload(inspectURL, "file", tc.file, tc.body)
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			assert.Contains(t, rec.Body.String(), `data-testid="mm-lora-error"`)
			assert.Contains(t, rec.Body.String(), tc.msg)
			assert.Contains(t, rec.Body.String(), `data-testid="mm-lora-pick"`, "the choose-a-file form is back")
			entries, _ := os.ReadDir(lorasDir(s))
			assert.Empty(t, entries)
		})
	}
}

// A name that is taken: the form says so, suggests another name and does not overwrite; committing the taken name is refused with a message above the fields,
// and with the suggested name the file is added next to the old one.
func TestASameNameFileIsReportedAndNeverOverwritten(t *testing.T) {
	s := screentest.New(t)
	old := lorafiletest.LoRA(map[string]string{"modelspec.title": "old"})
	require.NoError(t, os.MkdirAll(lorasDir(s), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(lorasDir(s), "style.safetensors"), old, 0o600))

	rec := s.Upload(inspectURL, "file", "style.safetensors", lorafiletest.LoRA(map[string]string{"modelspec.title": "new"}))
	require.Equal(t, http.StatusOK, rec.Code)
	form := rec.Body.String()
	assert.Contains(t, form, `data-testid="mm-lora-conflict"`)
	assert.Contains(t, form, "上書きしません")
	suggest := regexp.MustCompile(`data-testid="mm-lora-name"[^>]*value="([^"]+)"|value="([^"]+)"[^>]*data-testid="mm-lora-name"`).FindStringSubmatch(form)
	require.NotNil(t, suggest, form)
	name := suggest[1] + suggest[2]
	assert.Regexp(t, `^style-[0-9a-f]{8}\.safetensors$`, name, "a free name is already in the field")

	tok := token(t, form)
	rec = s.Do(http.MethodPost, lorasURL, commit(tok, url.Values{"name": {"style.safetensors"}}))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "#mm-lora-msg", rec.Header().Get("HX-Retarget"))
	assert.Contains(t, rec.Body.String(), "上書きしません")
	got, _ := os.ReadFile(filepath.Join(lorasDir(s), "style.safetensors"))
	assert.Equal(t, old, got)

	rec = s.Do(http.MethodPost, lorasURL, commit(tok, url.Values{"name": {name}}))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.FileExists(t, filepath.Join(lorasDir(s), name))
	got, _ = os.ReadFile(filepath.Join(lorasDir(s), "style.safetensors"))
	assert.Equal(t, old, got)
}

// The same content under the same name needs no second copy: the form says so and offers no add button.
func TestTheSameFileAgainOffersNoAdd(t *testing.T) {
	s := screentest.New(t)
	file := lorafiletest.LoRA(nil)
	require.NoError(t, os.MkdirAll(lorasDir(s), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(lorasDir(s), "same.safetensors"), file, 0o600))
	form := s.Upload(inspectURL, "file", "same.safetensors", file).Body.String()
	assert.Contains(t, form, "同じ内容のファイルが、すでに")
	assert.NotContains(t, form, `data-testid="mm-lora-commit"`)
	assert.Contains(t, form, `data-testid="mm-lora-discard"`)
}

// A refused commit keeps the file waiting (the form can be corrected); "取り消す" throws it away and nothing is placed.
func TestDiscardThrowsTheStagedFileAway(t *testing.T) {
	s := screentest.New(t)
	tok := token(t, s.Upload(inspectURL, "file", "a.safetensors", lorafiletest.LoRA(nil)).Body.String())
	rec := s.Do(http.MethodPost, lorasURL, commit(tok, url.Values{"name": {"a.safetensors"}, "strength": {"9"}}))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "推奨の強さ")
	assert.Equal(t, http.StatusOK, s.Do(http.MethodPost, paths.ModelManagerPart("loras", "discard"), url.Values{"token": {tok}}).Code)
	rec = s.Do(http.MethodPost, lorasURL, commit(tok, url.Values{"name": {"a.safetensors"}}))
	assert.Contains(t, rec.Body.String(), "もう一度ファイルを選んでください")
	assert.NoFileExists(t, filepath.Join(lorasDir(s), "a.safetensors"))
}

// The list's edit form replaces what was confirmed (and an empty strength is "not set"); delete removes the file and the record.
func TestEditAndDeleteAnAddedLoRA(t *testing.T) {
	s := screentest.New(t)
	tok := token(t, s.Upload(inspectURL, "file", "a.safetensors", lorafiletest.LoRA(nil)).Body.String())
	require.Equal(t, http.StatusCreated, s.Do(http.MethodPost, lorasURL, commit(tok, url.Values{"name": {"a.safetensors"}})).Code)
	q := "?path=" + url.QueryEscape("models/loras/a.safetensors")

	rec := s.Do(http.MethodPut, lorasURL+q, url.Values{"base_models": {"flux", "sdxl"}, "strength": {"0.6"}, "trigger_words": {"abc"}, "license_name": {"X"}, "commercial": {"conditional"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "SDXL")
	assert.Contains(t, rec.Body.String(), "FLUX.1")
	assert.Contains(t, rec.Body.String(), `data-commercial="conditional"`)
	assert.Equal(t, http.StatusUnprocessableEntity, s.Do(http.MethodPut, lorasURL+q, url.Values{"strength": {"abc"}}).Code)
	assert.Equal(t, http.StatusNotFound, s.Do(http.MethodPut, lorasURL+"?path=models/loras/none.safetensors", nil).Code)

	rec = s.Do(http.MethodDelete, lorasURL+q, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "まだ追加していません")
	assert.NoFileExists(t, filepath.Join(lorasDir(s), "a.safetensors"))
}

// Adding a LoRA starts nothing: no job is created in the queue.
func TestImportStartsNoJob(t *testing.T) {
	s := screentest.New(t)
	tok := token(t, s.Upload(inspectURL, "file", "a.safetensors", lorafiletest.LoRA(nil)).Body.String())
	require.Equal(t, http.StatusCreated, s.Do(http.MethodPost, lorasURL, commit(tok, url.Values{"name": {"a.safetensors"}})).Code)
	assert.Empty(t, s.Rig.Svc.ActiveHFJobs())
	body := s.PageOf("image-video")
	assert.False(t, strings.Contains(body, `data-testid="mm-progress"`), "no install progress")
}
