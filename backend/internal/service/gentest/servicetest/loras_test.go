package servicetest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

const loraInfo = `{"LoraLoaderModelOnly":{"input":{"required":{"model":["MODEL"],"lora_name":[["zeta.safetensors","alpha.safetensors","alpha.safetensors"],{}],"strength_model":["FLOAT",{}]}}}}`

func TestLoRAsListsTheFilesOfTheBundledEngineSorted(t *testing.T) {
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		asked.Add(1)
		if req.URL.Path != "/object_info/LoraLoaderModelOnly" {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write([]byte(loraInfo))
	}))
	t.Cleanup(srv.Close)
	r := gentesttest.New(t, gentesttest.WithReferenceModels(srv.URL))

	got := r.Svc.LoRAs(context.Background())
	assert.Equal(t, []string{"alpha.safetensors", "zeta.safetensors"}, got)
	got[0] = "changed"
	assert.Equal(t, []string{"alpha.safetensors", "zeta.safetensors"}, r.Svc.LoRAs(context.Background()), "the answer is kept; the caller's slice is its own")
	assert.EqualValues(t, 1, asked.Load(), "kept for a few seconds")
}

func TestLoRAsAcceptsTheNewComboShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"LoraLoaderModelOnly":{"input":{"required":{"lora_name":["COMBO",{"options":["b.safetensors","a.safetensors"]}]}}}}`))
	}))
	t.Cleanup(srv.Close)
	r := gentesttest.New(t, gentesttest.WithReferenceModels(srv.URL))
	assert.Equal(t, []string{"a.safetensors", "b.safetensors"}, r.Svc.LoRAs(context.Background()))
}

func TestLoRAsIsEmptyWhenTheEngineIsNotReachableOrHasNone(t *testing.T) {
	down := gentesttest.New(t, gentesttest.WithReferenceModels(""))
	assert.Empty(t, down.Svc.LoRAs(context.Background()), "a stopped engine")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"LoraLoaderModelOnly":{"input":{"required":{"lora_name":[[],{}]}}}}`))
	}))
	t.Cleanup(srv.Close)
	none := gentesttest.New(t, gentesttest.WithReferenceModels(srv.URL))
	assert.Empty(t, none.Svc.LoRAs(context.Background()))

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "nope", http.StatusInternalServerError) }))
	t.Cleanup(bad.Close)
	broken := gentesttest.New(t, gentesttest.WithReferenceModels(bad.URL))
	assert.Empty(t, broken.Svc.LoRAs(context.Background()))

	plain := gentesttest.New(t)
	assert.Empty(t, plain.Svc.LoRAs(context.Background()), "no bundled endpoint registered")
}
