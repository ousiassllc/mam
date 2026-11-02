package failtext_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/failtext"
)

// The three ways a local call times out are worded apart: a job that showed no progress (the timeout can be raised), a
// call that ran past the ceiling (it cannot), and a call to a kind whose timeout bounds the call.
func TestLocalTimeoutSaysWhichLimitWasHit(t *testing.T) {
	cause := errors.New("timeout")
	stalled, ok := failtext.LocalTimeout(localcfg.MarkStall(30*time.Minute, cause))
	assert.True(t, ok)
	assert.Contains(t, stalled, "30分のあいだ進まなかった")
	assert.Contains(t, stalled, "タイムアウト（進みが止まってから打ち切るまでの時間）を延ばして")

	ceiling, ok := failtext.LocalTimeout(&localcfg.TimeoutError{After: localcfg.MaxTimeoutSeconds * time.Second, Err: cause})
	assert.True(t, ok)
	assert.Contains(t, ceiling, "全体の上限（24時間）")
	assert.NotContains(t, ceiling, "延ばす")

	whole, ok := failtext.LocalTimeout(&localcfg.TimeoutError{After: 15 * time.Minute, Err: cause})
	assert.True(t, ok)
	assert.Contains(t, whole, "接続先の待ち時間（15分）")
	assert.Contains(t, whole, "タイムアウトを延ばす")

	_, ok = failtext.LocalTimeout(cause)
	assert.False(t, ok)
}
