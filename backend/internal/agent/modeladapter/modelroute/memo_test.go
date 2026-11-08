package modelroute_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// counted is the Env of the observed configuration whose availability check counts its questions per model: it reads credentials and the disk in
// production, so a decision asks it once per model however many routes it chooses.
func counted(t *testing.T) (modelroute.Env, map[domain.ModelName]int) {
	t.Helper()
	env := observed(t)
	asked, check := map[domain.ModelName]int{}, env.Check
	env.Check = func(m domain.ModelName) modelfilter.Availability {
		asked[m]++
		return check(m)
	}
	return env, asked
}

func requireOnce(t *testing.T, asked map[domain.ModelName]int) {
	t.Helper()
	require.NotEmpty(t, asked)
	for m, n := range asked {
		assert.Equal(t, 1, n, "the availability of %s was asked %d times", m, n)
	}
}

// One Image call asks each model once, and so does one Video call, however many models it looks over.
func TestImageAndVideo_AskTheAvailabilityOfAModelOncePerCall(t *testing.T) {
	env, asked := counted(t)
	_, err := env.Image(modeladapter.PurposeNewReference)
	require.NoError(t, err)
	requireOnce(t, asked)

	env, asked = counted(t)
	_, err = env.Video(domain.QualityBalanced, modelroute.VideoNeed{Keyframe: true})
	require.NoError(t, err)
	requireOnce(t, asked)
}

// A decision that chooses several routes (a Reference Pack's two image purposes) shares the answers when the Env is memoised first.
func TestMemoised_RoutesOfOneDecisionShareTheAnswers(t *testing.T) {
	env, asked := counted(t)
	env = env.Memoised()

	for _, p := range []modeladapter.ImagePurpose{modeladapter.PurposeNewReference, modeladapter.PurposeEditReference} {
		_, err := env.Image(p)
		require.NoError(t, err)
	}
	_, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Keyframe: true})
	require.NoError(t, err)

	requireOnce(t, asked)
}
