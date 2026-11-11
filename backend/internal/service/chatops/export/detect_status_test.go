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
	for _, message := range []string{"さっき書き出した動画をもう一度書き出して", "書き出した動画を 720p で書き出したい", "さっき書き出した動画をもう一度書き出していただけますか", "書き出した動画をもう一度書き出していい？", "書き出した動画をもう一度エクスポートする", "書き出した動画をもう一度書き出してください", "書き出した動画をもう一度書き出してから確認して",
		"さっき書き出した動画をもう一度書き出して確認して", "さっき書き出した動画をもう一度書き出して再生して", "さっき書き出した動画をもう一度書き出して共有して", "さっき書き出した動画をもう一度書き出して mp4 にして",
		"さっき書き出した動画をもう一度書き出して 720p にして", "さっき書き出した動画をもう一度書き出して mp4 で", "さっき書き出した動画をもう一度書き出してくれ", "さっき書き出した動画をもう一度書き出してくれ。",
		"さっき書き出した動画をもう一度書き出してくれたら嬉しい", "さっき書き出した動画をもう一度書き出してくれれば", "さっき書き出した動画をもう一度書き出してお願い", "さっき書き出した動画をもう一度書き出してみて",
		"さっき書き出した動画をもう一度書き出してみてください", "さっき書き出した動画をもう一度書き出してあげて", "さっき書き出した動画をもう一度書き出してくださる？", "さっき書き出した動画をもう一度書き出して　お願い",
		"さっき書き出した動画をもう一度書き出して…", "さっき書き出した動画をもう一度書き出して〜", "さっき書き出した動画をもう一度書き出してー"} {
		got := detect(t, message)
		require.Len(t, got, 1, message)
		assert.Contains(t, got, export.OpExportVideo, message)
	}
	for _, message := range []string{"さっき書き出した動画を確認して", "書き出した動画は良かった", "書き出した動画をエクスポートしてた？", "書き出した動画と書き出してない動画", "書き出した動画を書き出していた", "書き出した動画を書き出していった", "書き出した動画を書き出していっている", "書き出した動画はもう書き出してます", "さっき書き出した動画を書き出してよかった？", "書き出した動画をエクスポートしてみたけど音がない", "書き出した動画を書き出して終わった", "書き出した動画をエクスポートしてあるはず",
		"書き出した動画を書き出してる", "書き出した動画を書き出してた", "書き出した動画を書き出してません", "書き出した動画を書き出してみた", "書き出した動画を書き出してあげた", "書き出した動画を書き出してもらった",
		"書き出した動画を書き出してくれた", "書き出した動画を書き出してくれなかった", "書き出した動画を書き出しておいた", "書き出した動画を書き出してしまった", "書き出した動画を書き出すつもり"} {
		assert.Empty(t, detect(t, message), message)
	}
}

// A noun the verb qualifies, a plan and a narration in a form that cannot ask ("書き出してます", "書き出して終わった") are no request, though 「書き出した動画」 and a verb stand in the message.
// The export needs a confirmation, so a doubtful sentence ("書き出して終わり", "書き出して確認したら音がない") is read as a request and costs one card, and is not listed here.
func TestDetect_NarrationsBesideThePastFormAreNotRequests(t *testing.T) {
	for _, message := range []string{
		"書き出した動画を書き出す方法を教えて", "書き出した動画を書き出す手順は？", "書き出した動画を書き出す設定は？", "書き出した動画を書き出す場所はどこ？", "書き出した動画を書き出す人は誰？",
		"書き出した動画を書き出す必要がある", "書き出した動画を書き出すな", "書き出した動画を書き出すより、こっちがいい", "書き出した動画を書き出す気がない", "書き出した動画を書き出すしかないか",
		"書き出した動画を書き出すんだよね", "書き出した動画を書き出すよ", "書き出した動画を書き出すってこと？", "書き出した動画を書き出すんだっけ",
		"書き出した動画を書き出して終了した", "書き出した動画を書き出して完了した", "書き出した動画を書き出して失敗した", "書き出した動画を書き出して満足した",
		"書き出した動画を書き出して確認した", "書き出した動画を書き出して見た", "書き出した動画を書き出してくれた？", "書き出した動画を書き出してもらった？",
		"書き出した動画を書き出してみました", "書き出した動画を書き出してあげました", "書き出した動画を書き出しておきました", "書き出した動画を書き出して見た", "書き出した動画を書き出して聞いた",
		"書き出した動画を書き出し直した", "書き出した動画を書き出し直しました", "書き出した動画を書き出し直す必要がある", "書き出した動画を書き出し直す方法を教えて", "書き出した動画を書き出し直してた",
		"書き出した動画を書き出してまして", "書き出した動画を書き出してみまして", "書き出した動画を書き出してあげまして", "書き出した動画を書き出しておきまして",
		"書き出した動画を書き出して再生した", "書き出した動画を書き出してチェックした",
	} {
		assert.Empty(t, detect(t, message), message)
		assert.False(t, export.AsksRender(message), message)
	}
}

// A request that goes on after 「書き出して」, or a question that holds a request, is read: a doubtful sentence costs one confirmation card, a dropped request costs the answer.
func TestDetect_ARequestThatGoesOnIsStillARequest(t *testing.T) {
	for _, message := range []string{
		"さっき書き出した動画をもう一度書き出して確認したい", "さっき書き出した動画をもう一度書き出して再生したい", "さっき書き出した動画をもう一度書き出してチェックしたい",
		"さっき書き出した動画をもう一度書き出して見たい", "さっき書き出した動画をもう一度書き出して試したい", "さっき書き出した動画をもう一度書き出して確認したいので、お願いします",
		"さっき書き出した動画をもう一度書き出して何分かかるか教えて", "さっき書き出した動画をもう一度書き出して問題ないか確認して", "さっき書き出した動画をもう一度書き出して完了しておいて",
		"さっき書き出した動画をもう一度書き出して終了したら知らせて", "さっき書き出した動画をもう一度書き出して失敗したらやり直して", "さっき書き出した動画をもう一度書き出して満足するまで調整して",
		"さっき書き出した動画をもう一度書き出してもダメなら教えて", "さっき書き出した動画をもう一度書き出して終わりにして", "さっき書き出した動画をもう一度書き出してくれて、リンクも教えて",
		"さっき書き出した動画をもう一度書き出して。できますか？", "さっき書き出した動画をもう一度書き出して確認してくれる？", "さっき書き出した動画をもう一度書き出して頂けますか",
		"さっき書き出した動画をもう一度書き出してもらってもいいですか", "さっき書き出した動画をもう一度書き出してくれません？", "さっき書き出した動画をもう一度書き出してお願いできますか",
		"さっき書き出した動画をもう一度書き出しておいてもらえますか", "さっき書き出した動画をもう一度書き出して？", "さっき書き出した動画をもう一度書き出すんで、お願い",
		"さっき書き出した動画をもう一度書き出してしまって", "さっき書き出した動画をもう一度書き出してよかったら送って",
		// 「ましょう」 is no 「ました」, 「見た目」「聞いた感じ」 are no 「見た」「聞いた」, and the verb forms 「し直し／しましょう」 ask
		"さっき書き出した動画をもう一度書き出してみましょう", "さっき書き出した動画をもう一度書き出しておきましょう", "さっき書き出した動画をもう一度書き出してあげましょうか",
		"さっき書き出した動画をもう一度書き出して見た目を確認して", "さっき書き出した動画をもう一度書き出して聞いた感じを教えて", "さっき書き出した動画をもう一度書き出して見た目を見たい",
		"さっき書き出した動画をもう一度書き出し直して", "さっき書き出した動画をもう一度書き出しなおして", "さっき書き出した動画をもう一度書き出し直したい",
		"さっき書き出した動画をもう一度書き出しましょう", "さっき書き出した動画をもう一度書き出しましょうか", "さっき書き出した動画をもう一度エクスポートし直して",
		"書き出した動画を書き出し直す", "書き出した動画を書き出し直します", "書き出した動画を書き出し直しましょう", "書き出した動画をエクスポートし直す", // 「し直す」 asks too (exportSuru)
		"さっき書き出した動画をもう一度書き出してなくてごめん", "さっき書き出した動画をもう一度書き出してたら教えて", "さっき書き出した動画をもう一度書き出してて",
	} {
		got := detect(t, message)
		require.Len(t, got, 1, message)
		assert.Contains(t, got, export.OpExportVideo, message)
	}
}
