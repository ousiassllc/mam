package track_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
)

func TestSetGetFinishAndSubscribe(t *testing.T) {
	tr := track.New()
	var got []track.Entry
	cancel := tr.Subscribe(func(e track.Entry) { got = append(got, e) })

	_, ok := tr.Get("j")
	assert.False(t, ok)
	tr.Set("j", "model:x", track.Update{Percent: 150, Phase: "download", DoneBytes: 5, TotalBytes: 10})
	e, ok := tr.Get("j")
	assert.True(t, ok)
	assert.Equal(t, 100, e.Update.Percent, "clamped")
	assert.Equal(t, "model:x", e.Target)
	assert.False(t, e.Done)

	tr.Set("j", "model:x", track.Update{Percent: -3})
	e, _ = tr.Get("j")
	assert.Equal(t, 0, e.Update.Percent)
	tr.Finish("j")
	e, _ = tr.Get("j")
	assert.True(t, e.Done, "the last progress stays readable")
	tr.Finish("unknown") // no panic, no event

	cancel()
	tr.Set("j", "model:x", track.Update{Percent: 1})
	assert.Len(t, got, 3, "no event after the subscription ended")
}

func TestOldFinishedEntriesAreDropped(t *testing.T) {
	tr := track.New()
	for i := range 200 {
		id := strconv.Itoa(i)
		tr.Set(id, "gentest:m", track.Update{})
		tr.Finish(id)
	}
	tr.Set("running", "engine", track.Update{})
	_, old := tr.Get("0")
	assert.False(t, old)
	_, recent := tr.Get("199")
	assert.True(t, recent)
	_, running := tr.Get("running")
	assert.True(t, running)
}
