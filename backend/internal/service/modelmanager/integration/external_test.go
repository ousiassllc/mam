package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// An external ComfyUI is read, never written: its model folders are compared with the catalog and a model that is
// not there is "missing". No request of the inventory changes the server.
func TestExternalInventoryListsInstalledModelsAndShowsTheMissingOnes(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/system_stats":
			_, _ = w.Write([]byte(`{"system":{"comfyui_version":"0.42.1"}}`))
		case "/models/checkpoints":
			_ = json.NewEncoder(w).Encode([]string{"a.safetensors", "other.ckpt"})
		case "/models/vae":
			_ = json.NewEncoder(w).Encode([]string{"shared.safetensors"})
		case "/models/diffusion_models":
			_ = json.NewEncoder(w).Encode([]string{"b.safetensors"})
		default:
			http.NotFound(w, r) // text_encoders: the folder does not exist there
		}
	}))
	defer srv.Close()
	r := newRig(t)
	require.NoError(t, r.models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "tailnet", Kind: localcfg.KindComfyUI, BaseURL: srv.URL}))

	inv, err := r.svc.ExternalInventory(ctx(), "tailnet")
	require.NoError(t, err)
	assert.Equal(t, "0.42.1", inv.Version)
	assert.Equal(t, []string{"a.safetensors", "other.ckpt"}, inv.Folders["checkpoints"])
	assert.Empty(t, inv.Folders["text_encoders"])
	byID := map[string]modelmanager.ExternalModel{}
	for _, m := range inv.Models {
		byID[m.ID] = m
	}
	assert.True(t, byID["m-a"].Complete)
	assert.True(t, byID["m-b"].Complete)
	assert.False(t, byID["m-c"].Complete)
	assert.Equal(t, []string{"text_encoders/c.safetensors"}, byID["m-c"].Missing)
	assert.Equal(t, []string{"vae/shared.safetensors"}, byID["m-c"].Present)
	mu.Lock()
	for _, m := range methods {
		assert.Contains(t, m, "GET ", "the inventory only reads")
	}
	mu.Unlock()

	_, err = r.svc.ExternalInventory(ctx(), "nope")
	assert.ErrorIs(t, err, modelmanager.ErrNotFound)
	require.NoError(t, r.models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "compat", Kind: localcfg.KindOpenAICompat, BaseURL: srv.URL}))
	var invalid *modelmanager.InvalidError
	_, err = r.svc.ExternalInventory(ctx(), "compat")
	assert.ErrorAs(t, err, &invalid, "only a ComfyUI endpoint has model folders")
}

func TestExternalInventoryReportsAnUnreachableServer(t *testing.T) {
	r := newRig(t)
	require.NoError(t, r.models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "gone", Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}))
	_, err := r.svc.ExternalInventory(ctx(), "gone")
	assert.Error(t, err)
}

// The external inventory follows the age gate like the catalog: an adult model is not listed until the confirmation is on
// record. The loras folder is read, so that the LoRA of a model can be there.
func TestExternalInventoryHidesAdultModelsUntilTheAgeIsConfirmedAndReadsLoras(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/system_stats":
			_, _ = w.Write([]byte(`{"system":{}}`))
		case "/models/checkpoints":
			_ = json.NewEncoder(w).Encode([]string{ckptA.name})
		case "/models/loras":
			_ = json.NewEncoder(w).Encode([]string{loraD.name})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	r := newRig(t, withAdultModel())
	require.NoError(t, r.models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "tailnet", Kind: localcfg.KindComfyUI, BaseURL: srv.URL}))

	inv, err := r.svc.ExternalInventory(ctx(), "tailnet")
	require.NoError(t, err)
	for _, m := range inv.Models {
		assert.NotEqual(t, "m-d", m.ID, "the gate is closed")
	}
	assert.Equal(t, []string{loraD.name}, inv.Folders["loras"], "the loras folder is read")

	r.confirmAdult()
	inv, err = r.svc.ExternalInventory(ctx(), "tailnet")
	require.NoError(t, err)
	var d *modelmanager.ExternalModel
	for i := range inv.Models {
		if inv.Models[i].ID == "m-d" {
			d = &inv.Models[i]
		}
	}
	require.NotNil(t, d)
	assert.True(t, d.Complete)
	assert.Contains(t, d.Present, "loras/"+loraD.name)
}
