package chat_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/chat"
)

func renderResult(t *testing.T, r chat.Result) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, chat.ResultBlock(r).Render(context.Background(), &buf))
	return buf.String()
}

// The thread is a polite live region that announces additions, and a running result block replaces itself (outerHTML) every 2 seconds, so each swap is an
// addition. Only the running block is turned off; the one that comes back finished is read once.
func TestResultBlock_ARunningBlockIsNotAnnouncedEveryTimeItIsSwapped(t *testing.T) {
	running := renderResult(t, chat.Result{BatchID: "b1", State: "running", StateTxt: "実行中", Tone: atoms.BadgeOK, Done: 1, Total: 3, Counts: "成功 1・失敗 0・取り消し 0"})
	assert.Contains(t, running, `aria-live="off"`)
	assert.Contains(t, running, `hx-trigger=`, "it is the block that polls")

	finished := renderResult(t, chat.Result{BatchID: "b1", State: "succeeded", StateTxt: "完了", Tone: atoms.BadgeOK, Done: 3, Total: 3, Finished: true, Counts: "成功 3・失敗 0・取り消し 0"})
	assert.NotContains(t, finished, `aria-live`, "the finished block is announced once, through the polite thread")
	assert.NotContains(t, finished, `hx-trigger=`)

	var thread bytes.Buffer
	require.NoError(t, chat.Chat(chat.Panel{SessionID: "0123456789abcdef0123456789abcdef"}).Render(context.Background(), &thread))
	assert.Contains(t, thread.String(), `role="log"`)
	assert.Contains(t, thread.String(), `aria-live="polite"`)
}
