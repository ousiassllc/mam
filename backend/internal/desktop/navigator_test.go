package desktop

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNavigator_NavigatesOnceBothAreReadyInEitherOrder(t *testing.T) {
	t.Run("URL first", func(t *testing.T) {
		var got []string
		n := NewNavigator(func(url string) { got = append(got, url) })
		n.SetURL("http://127.0.0.1:1")
		assert.Empty(t, got, "the window is not ready yet")
		n.WindowReady()
		assert.Equal(t, []string{"http://127.0.0.1:1"}, got)
	})

	t.Run("window first", func(t *testing.T) {
		var got []string
		n := NewNavigator(func(url string) { got = append(got, url) })
		n.WindowReady()
		assert.Empty(t, got, "the URL is not known yet")
		n.SetURL("http://127.0.0.1:2")
		assert.Equal(t, []string{"http://127.0.0.1:2"}, got)
	})
}

func TestNavigator_LaterWindowEventsDoNotNavigateAgain(t *testing.T) {
	var got []string
	n := NewNavigator(func(url string) { got = append(got, url) })
	n.SetURL("http://127.0.0.1:3")
	n.WindowReady()
	n.WindowReady() // the navigation events of the loaded app
	n.WindowReady()
	assert.Equal(t, []string{"http://127.0.0.1:3"}, got)
}
