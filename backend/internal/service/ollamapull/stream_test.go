package ollamapull

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func pulling(digest string, total, completed int64) line {
	return line{Status: "pulling " + digest[:12], Digest: digest, Total: total, Completed: completed}
}

const (
	layerA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	layerB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// The total of a layer and its completed bytes begin again at 0 with every layer, and the stream has no total of the pull: the progress is the finished layers and the part of the one in progress, over the
// total of the record, and it never goes back when the next layer begins.
func TestProgressIsTheFinishedLayersAndTheCurrentOneOverTheTotalOfTheRecord(t *testing.T) {
	p := newProgress(1000)
	steps := []struct {
		l    line
		want int
	}{
		{line{Status: "pulling manifest"}, 0},
		{pulling(layerA, 600, 0), 0},
		{pulling(layerA, 600, 300), 30},
		{pulling(layerA, 600, 600), 60}, // the layer is finished
		{pulling(layerB, 300, 0), 60},   // the next layer begins at 0: the progress does not go back
		{pulling(layerB, 300, 150), 75},
		{pulling(layerB, 300, 300), 90},
		{line{Status: "verifying sha256 digest"}, 90},
		{line{Status: "writing manifest"}, 90},
	}
	for _, s := range steps {
		u, done := p.add(s.l)
		assert.False(t, done)
		assert.Equal(t, s.want, u.Percent, "%+v", s.l)
	}
	u, done := p.add(line{Status: "success"})
	assert.True(t, done)
	assert.Equal(t, 100, u.Percent)
	assert.Equal(t, "done", u.Phase)
}

// A line that arrives late or twice, a layer that reports less than before and a total that is larger than the record never make the progress go back or pass the end before the success.
func TestProgressNeverGoesBackAndNeverReachesOneHundredBeforeSuccess(t *testing.T) {
	p := newProgress(1000)
	var last int
	for _, l := range []line{
		pulling(layerA, 800, 700), pulling(layerA, 800, 200), pulling(layerB, 800, 800), pulling(layerA, 800, 800), pulling(layerB, 5000, 5000),
	} {
		u, _ := p.add(l)
		assert.GreaterOrEqual(t, u.Percent, last)
		assert.LessOrEqual(t, u.Percent, 99)
		last = u.Percent
	}
	assert.Equal(t, 99, last)
}

func TestProgressNamesTheStepsInJapanese(t *testing.T) {
	p := newProgress(1000)
	for status, want := range map[string]string{
		"pulling manifest":        "manifest を取得しています",
		"verifying sha256 digest": "検証しています",
		"writing manifest":        "manifest を書いています",
	} {
		u, _ := p.add(line{Status: status})
		assert.Equal(t, want, u.Message)
	}
	u, _ := p.add(pulling(layerA, 500, 250))
	assert.Contains(t, u.Message, "データを取得しています（25%")
	assert.Equal(t, "pulling", u.Phase)
	assert.Equal(t, int64(250), u.DoneBytes)
	assert.Equal(t, int64(1000), u.TotalBytes)
}

func TestExplainErrorSaysWhatWentWrongBeforeWhatOllamaSaid(t *testing.T) {
	for msg, want := range map[string]string{
		"write /models/blobs: no space left on device":                        "ディスクの空きが足りません",
		"pull model manifest: file does not exist":                            "Ollama のレジストリにこの名前のモデルがありません",
		"max retries exceeded: Get https://x: dial tcp: i/o timeout":          "ネットワークに接続できないか、途中で切れました",
		"digest mismatch, file must be downloaded again: want sha256:a got b": "ダウンロードしたデータの検証に失敗しました",
		"something unexpected":                                                "Ollama が取得に失敗しました",
	} {
		got := explainError(msg)
		assert.Contains(t, got, want, msg)
		assert.Contains(t, got, msg, "what Ollama said is kept")
		assert.Contains(t, got, "もう一度取得すると")
	}
}
