package localmodels_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

func TestAPIKeyIsStoredPerEndpointAndNeverReturned(t *testing.T) {
	r := newRig(t, false, "")
	ctx := context.Background()
	require.NoError(t, r.svc.CreateEndpoint(ctx, comfy("http://127.0.0.1:8188")))
	assert.Equal(t, localmodels.KeyStatus{}, mustKey(t, r.svc, "box"))

	require.NoError(t, r.svc.SetEndpointKey("box", "  sk-secret-0000009876  "))
	got, err := r.keys.Get("endpoint:box")
	require.NoError(t, err)
	assert.Equal(t, "sk-secret-0000009876", got)
	assert.Equal(t, localmodels.KeyStatus{Configured: true, LastChars: "9876"}, mustKey(t, r.svc, "box"))
	v, _ := r.svc.View()
	assert.Equal(t, "9876", v.Endpoints[0].Key.LastChars)
	data, _ := os.ReadFile(r.path)
	assert.NotContains(t, string(data), "sk-secret", "the key is not in the file")

	var invalid *localmodels.InvalidError
	require.ErrorAs(t, r.svc.SetEndpointKey("box", ""), &invalid)
	require.ErrorAs(t, r.svc.SetEndpointKey("box", "a b"), &invalid)
	assert.ErrorIs(t, r.svc.SetEndpointKey("nope", "k"), localmodels.ErrNotFound)

	require.NoError(t, r.svc.ClearEndpointKey("box"))
	assert.Equal(t, localmodels.KeyStatus{}, mustKey(t, r.svc, "box"))
	require.NoError(t, r.svc.ClearEndpointKey("box"), "clearing a missing key is not an error")

	require.NoError(t, r.svc.SetEndpointKey("box", "again"))
	require.NoError(t, r.svc.DeleteEndpoint(ctx, "box"))
	ok, _ := r.keys.Has("endpoint:box")
	assert.False(t, ok, "deleting the endpoint deletes its key")
}
