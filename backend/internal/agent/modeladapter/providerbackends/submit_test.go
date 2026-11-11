package providerbackends

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
)

// A body that has no JSON (a NaN) is an error of the call, not an empty body sent to the aggregator: the zero API would fail on any request, so the error that
// is read here can only come from the encoding.
func TestSubmitRefusesABodyThatCannotBeEncoded(t *testing.T) {
	err := submit(context.Background(), modeladapter.API{}, "/jobs", nil, map[string]any{"duration": math.NaN()}, nil)

	require.ErrorContains(t, err, "encode the request to /jobs")
}
