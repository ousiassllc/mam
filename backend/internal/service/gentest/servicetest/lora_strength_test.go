package servicetest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

// localcfg treats a LoRA strength of 0 as "not given" (the engine then runs the default 0.4): a 0 the user chose must reach the
// model as the least strength while the record keeps the 0.
func TestRunSendsALoRAStrengthOfZeroAsTheLeastNotTheDefault(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""))
	req := imageReq(string(gentesttest.EditModel))
	req.LoRA = &gentest.LoRAInput{Name: "face.safetensors", Strength: f64(0)}

	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)

	sent := r.Fake.CallsOf(gentesttest.EditModel)[0].Image.Options.LoRA
	require.NotNil(t, sent)
	assert.Greater(t, sent.Strength, 0.0, "0 would be the default 0.4")
	assert.Less(t, sent.Strength, 0.1)
	require.NotNil(t, st.Results[0].LoRA)
	assert.Zero(t, st.Results[0].LoRA.Strength, "the record keeps what the user chose")
}

func TestLoRAsDoesNotAskTheEngineForVideo(t *testing.T) {
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		_, _ = w.Write([]byte(loraInfo))
	}))
	t.Cleanup(srv.Close)
	r := gentesttest.New(t, gentesttest.WithReferenceModels(srv.URL))

	assert.Empty(t, r.Svc.LoRAs(context.Background(), gentest.MediaVideo))
	assert.Zero(t, asked.Load(), "a LoRA is for images only")
	assert.Len(t, r.Svc.LoRAs(context.Background(), gentest.MediaImage), 2)
	assert.Len(t, r.Svc.LoRAs(context.Background(), ""), 2, "both media")
	assert.EqualValues(t, 1, asked.Load())
}
