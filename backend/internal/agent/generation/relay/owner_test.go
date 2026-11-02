package relay_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/generation/relay"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The progress of a Shot's ComfyUI calls is found by the Shot's ID (get_job): the handler Wrap returns runs with the Shot as owner.
func TestWrapMakesTheShotTheOwnerOfTheModelCalls(t *testing.T) {
	var owner string
	wrapped := relay.New(func(generation.Progress) {}).Wrap(func(ctx context.Context, _ domain.Job) error {
		owner = live.OwnerOf(ctx)
		return nil
	})

	require.NoError(t, wrapped(context.Background(), domain.Job{ID: "job-1", Payload: []byte(`{"shot_id":"shot-7","action":"generate"}`)}))
	assert.Equal(t, "shot-7", owner)

	require.NoError(t, wrapped(context.Background(), domain.Job{ID: "job-2", Payload: []byte(`{"batch_id":"b"}`)}))
	assert.Empty(t, owner, "a job that is not a Shot's has no owner")
}
