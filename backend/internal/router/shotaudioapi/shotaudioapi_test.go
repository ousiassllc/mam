package shotaudioapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/router/shotaudioapi"
	"github.com/ousiassllc/mam/backend/internal/service/shotaudiosvc"
	"github.com/ousiassllc/mam/backend/internal/service/shotaudiosvc/shotaudiotest"
)

func TestShotAudioAPI_GetPutAndTheStatusOfEveryRefusal(t *testing.T) {
	f := shotaudiotest.New(t)
	f.Asset("p1", "voice", domain.AssetAudio, shotaudiotest.WAV(3))
	f.Asset("p1", "pic", domain.AssetImage, []byte("png"))
	f.Asset("p2", "foreign", domain.AssetAudio, shotaudiotest.WAV(3))
	_, api := humatest.New(t)
	shotaudioapi.Register(api, f.Svc)

	resp := api.Get("/api/v1/shots/s1/audio")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var got shotaudiosvc.Report
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	assert.Nil(t, got.Audio)
	require.Len(t, got.Choices, 1)
	assert.Equal(t, "voice", got.Choices[0].AssetID)
	assert.Equal(t, "p1", got.ProjectID)
	assert.Contains(t, resp.Body.String(), `"audio":null`, "the key is there, with null")

	resp = api.Put("/api/v1/shots/s1/audio", map[string]any{"asset_id": "voice"})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Contains(t, resp.Body.String(), `"audio":{"asset_id":"voice"`)
	assert.Contains(t, resp.Body.String(), `"usable":true`)

	assert.Equal(t, http.StatusNotFound, api.Get("/api/v1/shots/none/audio").Code)
	assert.Equal(t, http.StatusNotFound, api.Put("/api/v1/shots/s1/audio", map[string]any{"asset_id": "foreign"}).Code, "an asset of another Project")
	assert.Equal(t, http.StatusUnprocessableEntity, api.Put("/api/v1/shots/s1/audio", map[string]any{"asset_id": "pic"}).Code)

	f.Exec(`UPDATE shot SET state = 'GENERATING' WHERE id = 's1'`)
	assert.Equal(t, http.StatusConflict, api.Put("/api/v1/shots/s1/audio", map[string]any{"asset_id": ""}).Code)
	f.Exec(`UPDATE shot SET state = 'PLANNING' WHERE id = 's1'`)

	f.Exec(`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('l', 'p1', 'shot', 's1', 'prompt', 'lock')`)
	locked := api.Put("/api/v1/shots/s1/audio", map[string]any{"asset_id": ""})
	assert.Equal(t, http.StatusLocked, locked.Code)
	assert.Contains(t, locked.Body.String(), "LOCK")
	f.Exec(`DELETE FROM lock`)

	resp = api.Put("/api/v1/shots/s1/audio", map[string]any{"asset_id": ""})
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"audio":null`)
}
