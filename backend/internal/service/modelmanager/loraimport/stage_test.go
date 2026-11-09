package loraimport_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile/lorafiletest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
)

// gated is an upload that says it began (its first Read) and then waits until the gate opens, so that a test holds requests in the middle of the write.
type gated struct {
	began chan<- struct{}
	gate  <-chan struct{}
	body  io.Reader
	once  sync.Once
}

func (g *gated) Read(p []byte) (int, error) {
	g.once.Do(func() { g.began <- struct{}{}; <-g.gate })
	return g.body.Read(p)
}

// limit is how many files the importer lets wait at once, found by filling it.
func limit(t *testing.T) int {
	t.Helper()
	r := newRig(t, nil)
	for n := 0; n < 50; n++ {
		if _, err := r.stage("a.safetensors", lorafiletest.LoRA(nil)); err != nil {
			require.Positive(t, n)
			return n
		}
	}
	t.Fatal("no limit on the files that wait")
	return 0
}

// Files that are still being written count against the limit of files that wait: at the same time, no more than the limit are let in, however the
// requests interleave (each may be gigabytes). A refused or failed upload gives its place back.
func TestStage_TheLimitOfWaitingFilesHoldsWhileUploadsAreInFlight(t *testing.T) {
	n := limit(t)
	r := newRig(t, nil)
	body := lorafiletest.LoRA(nil)
	total := n + 3
	began, gate := make(chan struct{}, total), make(chan struct{})
	type result struct{ err error }
	done := make(chan result, total)
	for range total {
		go func() {
			_, err := r.svc.Stage(context.Background(), "a.safetensors", int64(len(body)), &gated{began: began, gate: gate, body: bytes.NewReader(body)})
			done <- result{err}
		}()
	}
	// Every request either begins its write or is refused at once. Wait (bounded) until all of them did one or the other.
	timeout := time.After(10 * time.Second)
	var writing, refused int
	for writing+refused < total {
		select {
		case <-began:
			writing++
		case res := <-done:
			require.Error(t, res.err, "nothing succeeds before the gate opens")
			assert.Contains(t, refusal(t, res.err), "確認待ち")
			refused++
		case <-timeout:
			t.Fatalf("requests did not settle: %d writing, %d refused", writing, refused)
		}
	}
	assert.Equal(t, n, writing, "no more uploads are let in than files may wait")
	close(gate)
	var ok int
	for range writing {
		select {
		case res := <-done:
			if res.err == nil {
				ok++
			}
		case <-time.After(10 * time.Second):
			t.Fatal("uploads did not finish")
		}
	}
	assert.Equal(t, n, ok)
	assert.Len(t, r.leftovers(), n)
}

// An upload that fails gives its place back: failures never use up the files that may wait.
func TestStage_AFailedUploadGivesItsPlaceBack(t *testing.T) {
	n := limit(t)
	r := newRig(t, nil)
	for range n + 2 {
		_, err := r.svc.Stage(context.Background(), "a.safetensors", 0, io.MultiReader(bytes.NewReader([]byte("x")), iotest.ErrReader(errors.New("connection reset"))))
		require.Error(t, err)
	}
	_, err := r.svc.Stage(context.Background(), "a.safetensors", 0, io.MultiReader(bytes.NewReader(lorafiletest.Garbage())))
	require.Error(t, err)
	for range n {
		r.mustStage("a.safetensors", lorafiletest.LoRA(nil))
	}
}

// The name the user's file had is kept for the record, but not without bound: what goes into state.json is cut to a sensible length.
func TestStage_TheOriginalNameIsBounded(t *testing.T) {
	r := newRig(t, nil)
	long := strings.Repeat("あ", 5000) + "x.safetensors"
	st := r.mustStage(long, lorafiletest.LoRA(nil))
	assert.LessOrEqual(t, len([]rune(st.OriginalName)), 255)
	m, err := r.svc.Commit(context.Background(), loraimport.Input{Token: st.Token})
	require.NoError(t, err)
	assert.LessOrEqual(t, len([]rune(m.OriginalName)), 255)
	assert.LessOrEqual(t, len([]rune(r.state.Get().LocalLoRAs[m.Path].OriginalName)), 255)
	assert.True(t, st.Renamed)
}
