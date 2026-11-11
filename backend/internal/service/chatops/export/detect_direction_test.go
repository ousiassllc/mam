package export_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/export"
)

// An export word that only places another instruction in time ("書き出す前に…") or says what the export will do is not a request to export.
func TestDetect_AnOrdinaryDirectionThatMentionsTheExportIsNotAnExport(t *testing.T) {
	for _, message := range []string{
		"書き出す前に動画の雰囲気を整えて",
		"動画を書き出す前にテンポを速くして",
		"書き出しの前にこの動画の長さを確認したい",
		"動画を書き出したらすぐ確認するので、字幕を大きくして",
		"書き出しは最後にするので、この動画のカットを見直して",
		"プロジェクト全体の雰囲気を暗くして。書き出しは最後にする",
		"プロジェクト全体のトーンを揃えてから書き出す前に確認させて",
		"動画を書き出すと EXPORTED になるの？",
		"動画を書き出す直前に Final QC を通して",
		"動画を書き出さないで、まず確認したい",
		"この動画をエクスポートせずに確認したい",
		"動画を書き出す手前で音量を揃えて",
		"この動画の書き出しは終わった？",
		"動画のエクスポートは完了した？",
		"動画を書き出しする前に確認したい",
		"Before export, make this video shorter",
		"Change the mood of the video after exporting",
	} {
		got := detect(t, message)
		assert.NotContains(t, got, export.OpExportVideo, message)
		assert.NotContains(t, got, export.OpExportBundle, message)
		assert.NotContains(t, got, export.OpDownloadExport, message)
		assert.False(t, export.AsksRender(message) || export.AsksBundle(message) || export.AsksDownload(message), message)
	}
}

// The request still reads when an ordinary direction sits beside it, and "プロジェクト全体の書き出し" is the bundle.
func TestDetect_ARequestBesideADirectionStillReads(t *testing.T) {
	for message, want := range map[string]commandinterpreter.OpType{
		"この動画の雰囲気を整えたら書き出して":       export.OpExportVideo,
		"プロジェクト全体の書き出しをお願い":        export.OpExportBundle,
		"プロジェクト全体を書き出したい":          export.OpExportBundle,
		"この動画を書き出して。書き出す前に確認はいらない": export.OpExportVideo,
		"プロジェクト全体を明るくして、動画を書き出して":  export.OpExportVideo,
		"動画をまるごと書き出して":             export.OpExportVideo,
		"この動画の書き出しは最終版でお願い":        export.OpExportVideo,
		"この動画をもう一度書き出しなおして":        export.OpExportVideo,
		"この動画の書き出しは 1080p でお願い":    export.OpExportVideo,
	} {
		got := detect(t, message)
		require.Len(t, got, 1, message)
		assert.Contains(t, got, want, message)
	}
}
