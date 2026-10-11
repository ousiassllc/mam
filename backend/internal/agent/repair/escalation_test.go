package repair_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// escalationChat fails the first failures calls of RaiseEscalation.
type escalationChat struct {
	repair.Chat
	failures int
	calls    int
}

func (c *escalationChat) RaiseEscalation(context.Context, string, string, []string, string) error {
	c.calls++
	if c.calls <= c.failures {
		return errors.New("chat is down")
	}
	return nil
}

func TestWithinPolicy_AnEscalationThatFailedToPostIsAskedAgainByTheNextAutomaticRun(t *testing.T) {
	chat := &escalationChat{failures: 1}
	a := repair.New(repair.Deps{Chat: chat})
	res := domain.JudgeResult{ID: "r1"}
	eff := domain.EffectivePolicy{MaxRetries: 1} // retries >= MaxRetries: the policy stops the loop
	ask := func(auto bool) (bool, error) {
		return a.WithinPolicy(context.Background(), repository.ShotRef{}, eff, auto, 1, res, "take1")
	}

	ok, err := ask(true)
	require.Error(t, err)
	assert.False(t, ok)

	ok, err = ask(true)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, 2, chat.calls, "the question never reached the user, so the second run asks again")

	ok, err = ask(true)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, 2, chat.calls, "a posted question is not repeated by the automatic loop")

	_, err = ask(false)
	require.NoError(t, err)
	assert.Equal(t, 3, chat.calls, "a manual Repair always asks")
}
