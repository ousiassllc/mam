package export_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/export"
)

// A question about the state of an export or a download is no request: it posts no card and gives no link.
func TestDetect_AQuestionAboutTheStateIsNotARequest(t *testing.T) {
	for _, message := range []string{
		"動画の書き出し終わった？",
		"動画のエクスポートが終わった？",
		"この動画の書き出しが終わった？",
		"この動画の書き出しは進んでる？",
		"動画の書き出しはどう？",
		"この動画の書き出しはできる？",
		"動画の書き出しの完了を教えて",
		"動画の書き出し、終わったら教えて",
		"動画のダウンロードは終わった？",
		"動画のダウンロードが終わった？",
		"動画のダウンロード終わった？",
	} {
		assert.Empty(t, detect(t, message), message)
		assert.False(t, export.AsksRender(message) || export.AsksBundle(message) || export.AsksDownload(message), message)
	}
}

// What asks for the export or the download in words that only start like a state ("済ませて", "完了させて") stays a request, and so does one message that asks for both.
func TestDetect_ARequestIsStillReadBesideAStateWord(t *testing.T) {
	for message, want := range map[string]commandinterpreter.OpType{
		"動画の書き出しは済ませて":                     export.OpExportVideo,
		"動画の書き出しは完了させて":                    export.OpExportVideo,
		"動画の書き出しは終わらせて":                    export.OpExportVideo,
		"動画を書き出してダウンロードしたい":                export.OpExportVideo,
		"この動画をエクスポートして、ダウンロードもしたい":         export.OpExportVideo,
		"Export the video and download it": export.OpExportVideo,
		"書き出した動画をダウンロードして":                 export.OpDownloadExport,
		"さっきの書き出しをダウンロードしたい":               export.OpDownloadExport,
		"モデルさんが映る動画を書き出して":                 export.OpExportVideo,
		"プロのモデルが出る動画をエクスポートして":             export.OpExportVideo,
	} {
		t.Run(message, func(t *testing.T) {
			got := detect(t, message)
			require.Len(t, got, 1)
			assert.Contains(t, got, want)
		})
	}
}

// A model of the catalog is still the install operation's.
func TestDetect_ACatalogModelIsStillNotAnExport(t *testing.T) {
	for _, message := range []string{"動画モデルをダウンロードして", "モデルさんの動画モデルをダウンロードして"} {
		assert.Empty(t, detect(t, message), message)
	}
}

// 「書き出した動画」 tells of an export, but a verb that asks for one after it ("もう一度書き出して") makes the message a request.
func TestDetect_ARequestVerbAfterThePastFormIsARequest(t *testing.T) {
	for _, message := range []string{"さっき書き出した動画をもう一度書き出して", "書き出した動画を 720p で書き出したい"} {
		got := detect(t, message)
		require.Len(t, got, 1, message)
		assert.Contains(t, got, export.OpExportVideo, message)
	}
	for _, message := range []string{"さっき書き出した動画を確認して", "書き出した動画は良かった"} {
		assert.Empty(t, detect(t, message), message)
	}
}
