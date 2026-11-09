package loratools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr/loratools"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile/lorafiletest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

func TestMain(m *testing.M) { mmtest.Main(m) }

func newEnv(t *testing.T) (*mcptest.Env, *mmtest.Rig) {
	t.Helper()
	var rig *mmtest.Rig
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		rig = mmtest.New(t, mmtest.WithDB(env.DB), mmtest.WithLogger(env.Logger))
		env.Services.ModelManager = rig.Svc
		loratools.Register(r, env.Services)
	})
	return env, rig
}

// upload posts a file to the ticket of the purpose, as a client does with the URL create_upload_ticket gives.
func upload(t *testing.T, env *mcptest.Env, params map[string]string, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	ticket, err := env.Registrar.Files.UploadTicketFor(context.Background(), loratools.Purpose, params)
	require.NoError(t, err)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", name)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, ticket.URL, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	env.Registrar.Files.Handler().ServeHTTP(rec, req)
	return rec
}

func lorasDir(rig *mmtest.Rig) string { return filepath.Join(rig.Layout.DataDir(), "models", "loras") }

type uploaded struct {
	Result loratools.LoRA `json:"result"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) uploaded {
	t.Helper()
	var u uploaded
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &u), rec.Body.String())
	return u
}

// The upload purpose takes a LoRA in and records only what the call gave: the metadata of the file is taken in only on request, the strength and the
// license never, and a license that is not given is unknown (never commercial use OK).
func TestUploadAddsALoRAAndListsIt(t *testing.T) {
	env, rig := newEnv(t)
	file := lorafiletest.LoRA(map[string]string{"ss_base_model_version": "sdxl_base_v1-0", "modelspec.trigger_phrase": "fromfile", "modelspec.license": "MIT"})

	rec := upload(t, env, map[string]string{"name": "my lora.safetensors", "strength": "0.6"}, "orig.safetensors", file)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got := decode(t, rec).Result
	assert.Equal(t, "models/loras/my_lora.safetensors", got.Path)
	assert.Empty(t, got.BaseModels, "what the file says is not taken in unless asked")
	assert.Empty(t, got.TriggerWords)
	require.NotNil(t, got.Strength)
	assert.InDelta(t, 0.6, *got.Strength, 1e-9)
	assert.Equal(t, "unknown", string(got.License.Commercial))
	onDisk, err := os.ReadFile(filepath.Join(lorasDir(rig), "my_lora.safetensors"))
	require.NoError(t, err)
	assert.Equal(t, file, onDisk)

	rec = upload(t, env, map[string]string{"adopt_metadata": "true", "base_models": "", "license_name": "Mine", "commercial": "conditional"}, "second.safetensors", file)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	second := decode(t, rec).Result
	assert.Equal(t, []string{"sdxl"}, second.BaseModels, "adopt_metadata takes the base model the file names")
	assert.Equal(t, []string{"fromfile"}, second.TriggerWords)
	assert.Nil(t, second.Strength)
	assert.Equal(t, "conditional", string(second.License.Commercial))

	var list loratools.ListOutput
	env.CallOK("list_user_loras", map[string]any{}, &list)
	require.Len(t, list.LoRAs, 2)
	for _, l := range list.LoRAs {
		assert.True(t, l.Present)
		assert.NotEqual(t, "yes", string(l.License.Commercial))
	}
}

// A file that is not a LoRA .safetensors, a bad name or a bad value is refused with 422 and nothing is placed; a taken name is refused and the old file stays.
func TestUploadRefusesWhatTheScreenRefuses(t *testing.T) {
	env, rig := newEnv(t)
	for name, tc := range map[string]struct {
		params map[string]string
		file   string
		body   []byte
		msg    string
	}{
		"text":           {nil, "a.safetensors", lorafiletest.Garbage(), ".safetensors の形式ではありません"},
		"a checkpoint":   {nil, "a.safetensors", lorafiletest.Checkpoint(), "LoRA ではなく"},
		"a pickle":       {nil, "a.ckpt", lorafiletest.LoRA(nil), "拡張子が .safetensors"},
		"a bad name":     {map[string]string{"name": "../../x.safetensors"}, "a.safetensors", lorafiletest.LoRA(nil), "使えない文字"},
		"a bad family":   {map[string]string{"base_models": "nope"}, "a.safetensors", lorafiletest.LoRA(nil), "ベースモデル"},
		"a bad strength": {map[string]string{"strength": "7"}, "a.safetensors", lorafiletest.LoRA(nil), "推奨の強さ"},
		"commercial yes": {map[string]string{"commercial": "yes"}, "a.safetensors", lorafiletest.LoRA(nil), "商用利用"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := upload(t, env, tc.params, tc.file, tc.body)
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tc.msg)
			entries, _ := os.ReadDir(lorasDir(rig))
			assert.Empty(t, entries)
		})
	}

	old := lorafiletest.LoRA(map[string]string{"modelspec.title": "old"})
	require.Equal(t, http.StatusCreated, upload(t, env, nil, "same.safetensors", old).Code)
	rec := upload(t, env, nil, "same.safetensors", lorafiletest.LoRA(map[string]string{"modelspec.title": "new"}))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "上書きしません")
	onDisk, _ := os.ReadFile(filepath.Join(lorasDir(rig), "same.safetensors"))
	assert.Equal(t, old, onDisk)
}

func TestUpdateAndDeleteAsToolCalls(t *testing.T) {
	env, rig := newEnv(t)
	require.Equal(t, http.StatusCreated, upload(t, env, nil, "a.safetensors", lorafiletest.LoRA(nil)).Code)
	path := "models/loras/a.safetensors"

	var got loratools.LoRA
	env.CallOK("update_user_lora", map[string]any{"path": path, "base_models": []string{"flux"}, "strength": 0.5, "trigger_words": []string{"zz", "yy"}, "license_name": "X", "commercial": "no"}, &got)
	assert.Equal(t, []string{"flux"}, got.BaseModels)
	assert.Equal(t, []string{"FLUX.1"}, got.Families)
	assert.Equal(t, []string{"zz", "yy"}, got.TriggerWords)
	assert.Equal(t, "no", string(got.License.Commercial))
	assert.Contains(t, env.CallError("update_user_lora", map[string]any{"path": path, "strength": 3}), "validation")
	assert.Contains(t, env.CallError("update_user_lora", map[string]any{"path": "models/loras/none.safetensors"}), "not_found")

	require.NoError(t, os.WriteFile(filepath.Join(lorasDir(rig), "hand.safetensors"), lorafiletest.LoRA(nil), 0o600))
	assert.Contains(t, env.CallError("delete_user_lora", map[string]any{"path": "models/loras/hand.safetensors"}), "not_found")
	assert.FileExists(t, filepath.Join(lorasDir(rig), "hand.safetensors"), "a file nobody recorded is never touched")

	var del loratools.Deleted
	env.CallOK("delete_user_lora", map[string]any{"path": path}, &del)
	assert.True(t, del.Deleted)
	assert.NoFileExists(t, filepath.Join(lorasDir(rig), "a.safetensors"))
}

// Without a model manager the tools say so.
func TestToolsAreUnavailableWithoutTheModelManager(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { loratools.Register(r, env.Services) })
	assert.Contains(t, env.CallError("list_user_loras", map[string]any{}), "unavailable")
}
