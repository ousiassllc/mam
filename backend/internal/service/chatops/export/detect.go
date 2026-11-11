package export

import (
	"regexp"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

var (
	exportWord = regexp.MustCompile(`(?i)(書き出|書出|エクスポート|export)`)
	// mentionedOnly is an export or download word that places another instruction in time ("書き出す前に…", "書き出したら…", "before export") or says what the export will do ("書き出すと…",
	// "書き出しは最後に"). It is no request, so the word is dropped before the message is read.
	mentionedOnly = regexp.MustCompile(`(?i)((書き出|書出|エクスポート|ダウンロード)(する|した|しない|せる|できる|す|し|しする)?(の?(前|まえ|直前|直後|手前|より前|後|あと|時|とき|際|場合|までに|まで|ので|ため|ように)|たら|れば|なら)` +
		`|(書き出|書出|エクスポート)(する|す)と` + // "書き出すと EXPORTED になる": what the export does
		`|(書き出|書出|エクスポート)(し)?は(最後|最終的|後|あと|まだ|別|不要|いらない|しない|しません|しなくて|今は)` + // "書き出しは最後に"; "書き出しは 1080p で" is a request
		// A question about the state of an export or a download ("書き出し終わった？", "書き出しが進んでる？", "書き出しの完了を教えて", "ダウンロードは終わった？"). The words end where the
		// state is told, so "書き出しは済ませて" and "書き出しは完了させて" (to do it) stay requests.
		`|(書き出|書出|エクスポート|ダウンロード)(し)?(の|は|が|も|、)?(終わった|終わってる|終わってい|終わりました|終わる|完了した|完了してる|完了してい|完了しました|完了を|済んだ|済んで|済み|できた|できて|進んで|進み|進行|どう|どこまで)` +
		`|(書き出|書出|エクスポート|ダウンロード)(し)?(は|が)できる` + // "書き出しはできる？": whether it can be done now
		`|(書き出|書出|エクスポート|ダウンロード)(さな|さず|せず|しない|しなく|しなかった|しません)` + // "書き出さないで", "エクスポートせずに": not asking for it
		`|\b(before|after|until|once|when|while|prior to)\s+(\w+\s+){0,2}?(export|download)\w*|\b(export|download)(ing|ed|s)\b)`)
	pastExport = regexp.MustCompile(`書き出した([^い]|$)`) // 「書き出した動画」 tells of an export, 「書き出したい」 asks for one
	// exportWant, exportTe and exportSuru are the verbs of the export that can ask for it: 「書き出したい」「書き出します」, 「書き出して」, 「書き出す」「エクスポートする」. Beside 「書き出した動画」 (which tells of an export) such a verb makes the
	// message a request ("さっき書き出した動画をもう一度書き出して", "…を 720p で書き出したい"), unless the narration forms below follow it. 「書き出した」 and 「書き出していた」 are no verb of this kind.
	exportWant = regexp.MustCompile(`(?:書き出|書出|エクスポート)(?:したい|します)`)
	exportTe   = regexp.MustCompile(`(?:書き出|書出|エクスポート)して`)
	exportSuru = regexp.MustCompile(`(?:書き出|書出|エクスポート)(?:する|す)`)
	// narratedTe is what follows 「して」 when the export is told of or the sentence goes on with something else: "書き出してます／てる／てた／てない／ません", "してみた", "してあげた", "してあるはず",
	// "して終わり／終わった／完了／済んだ／失敗／満足／正解／問題なかった", "してよかった", "してもらった／もらいたくない", "してくれた／くれて助かった／くれたら良かった", "しておいた", "してしまった",
	// "して確認したら／見たけど", "して初めて", "して意味ある？／どう／何", "してはいけない／はだめ", "しても音がない／もだめ", "してて気づいた". None of them asks. Anything else after 「して」 (a space,
	// a symbol, "確認して", "お願い", "くれ", "くれたら嬉しい", "みて") is the request.
	narratedTe = regexp.MustCompile(`^(?:い|る|た|な(?:い|かっ|く|けれ)|ま(?:す|し|せん)|み(?:た(?:[^い]|$)|まし)|あげ(?:た|まし)|あ(?:る|っ|り)|終わ(?:っ|り|る|りまし)|終了|完了(?:し|だ|で|[。、！!？?\s]|$)|済(?:ん|み)|失敗|満足|正解|問題(?:な|が|は|も|あ)|` +
		`(?:よ|良)かっ|もらっ(?:た|ち)|もらいまし|もらいたくな|くれ(?:た(?:[^らり]|$)|たら(?:よ|良)かっ|て|なかっ|まし|ませんでし)|しまっ|おいた(?:[^らり]|$)|おきまし|` +
		`(?:確認し|再生し|チェックし|見|聞い|試し)た|初めて|意味|どう|何|は(?:いけ|だめ|ダメ|ならな)|も(?:だめ|ダメ|音|ない|同じ|変わら)|て)`)
	// askedTe is what follows 「して」 and still asks ("書き出していただけますか", "書き出していい？"; 「い」 alone is 「書き出していた」): it keeps the narration list from taking "していい" for "している".
	askedTe = regexp.MustCompile(`^い(?:ただ|い)`)
	// narratedSuru is what follows 「する」「す」 when it is no ask but a plan, a reason, a noun it qualifies or a statement ("書き出すつもり", "書き出すのを忘れた", "書き出す方法／手順／設定／場所／人",
	// "書き出す必要がある", "書き出すな", "書き出すより", "書き出すしかない", "書き出すんだよね", "書き出すよ", "書き出すってこと", "書き出す気がない").
	narratedSuru = regexp.MustCompile(`^(?:の|と|か|が|は|を|も|に|で|けど|けれど|つもり|はず|こと|たび|のに|べき|でき|だけ|ほど|予定|方法|手順|設定|場所|人|必要|ほか|より|しかない|なんて|ってこと|って|んだ|ん|気が|` +
		`[なよ]あ?(?:[。、！!？?\s]|$))`)
	// explicitAsk is a form that asks in a question too ("書き出していただけますか", "書き出してくれる？", "書き出していい？", "書き出したい？"); "書き出す？" and "書き出しますか" are no ask but a question.
	explicitAsk = regexp.MustCompile(`(?:書き出|書出|エクスポート)(?:したい|して(?:いただ|もらえ|もらいたい|くれる|くれます|くれない|くれませんか|くださ|下さい|ほしい|欲しい|ちょうだい|も?(?:いい|よい|良い)))`)
	// asked ends a question: "…？", "…か", "…かな", "…っけ", "…の".
	asked = regexp.MustCompile(`(?:[？?]|か|かな|かい|っけ|の)[？?\s]*$`)
	// exportThenDownload is one request for both: "動画を書き出してダウンロードしたい", "export the video and download it". The word of the export is a verb that asks for it ("書き出して"),
	// not the noun of an export that was made ("さっきの書き出しをダウンロード", "書き出した動画をダウンロード").
	exportThenDownload = regexp.MustCompile(`(?i)((書き出|書出|エクスポート)(し)?(て|たい|てから|つつ|た上で)[^。！？!?\n]*ダウンロード|\bexport\b[^.!?\n]*\band\b[^.!?\n]*\bdownload)`)
	downloadWord       = regexp.MustCompile(`(?i)(ダウンロード|download)`)
	// videoWord names the finished video; bundleWord the Project as a ZIP. A message about the Project as a whole ("プロジェクトを書き出して") is the bundle too: the app
	// calls it "プロジェクトの書き出し（バンドル）".
	videoWord    = regexp.MustCompile(`(?i)(動画|ビデオ|映像|完成|mp4|webm|video|最終版)`)
	bundleWord   = regexp.MustCompile(`(?i)(バンドル|bundle|zip|ジップ|バックアップ)`)
	wholeWord    = regexp.MustCompile(`(?i)(まるごと|丸ごと|プロジェクトごと|プロジェクト全体|project\s*ごと)(の?)`)
	exportNoun   = regexp.MustCompile(`(?i)^(書き出|書出|エクスポート|ダウンロード|バックアップ|バンドル|zip|ジップ)`)
	projectWord  = regexp.MustCompile(`(?i)(プロジェクト|project)`)
	demonstrated = regexp.MustCompile(`(これ|この|それ|その|いま|今の)`)
	// otherThing is what the words of this operation also mean elsewhere: installing a model ("動画モデルをダウンロード") is the install operation's.
	otherThing = regexp.MustCompile(`(?i)(モデル|model|lora|ollama|comfy|ランタイム|runtime|インストール|install|導入|重み|ノード|ワークフロー|workflow)`)
	// person is the "モデル" of a fashion model or of the cast ("プロのモデル", "モデルさん"): it names no model of the catalog, so it is no install (the same words as chatops/install).
	person   = regexp.MustCompile(`(?:プロ|出演者?|ファッション|専属|人物|女性|男性|外国人|日本人|ヘア|ハンド)の?モデル|モデル(?:さん|ちゃん|君|くん|たち|達)`)
	pastFile = regexp.MustCompile(`(さっき|最新|前の|先ほど|直前|ファイル|mp4|書き出し|書出し|エクスポート|export)`)

	ratioWord   = regexp.MustCompile(`(16\s*[:：]\s*9|9\s*[:：]\s*16|1\s*[:：]\s*1|4\s*[:：]\s*5)`)
	resolution  = regexp.MustCompile(`(?i)(720|1080|1440|2160)\s*p`)
	codecH264   = regexp.MustCompile(`(?i)(h\.?264|avc)`)
	codecH265   = regexp.MustCompile(`(?i)(h\.?265|hevc)`)
	codecVP9    = regexp.MustCompile(`(?i)(vp9|webm)`)
	fourK       = regexp.MustCompile(`(?i)(4k|2160)`)
	fullHD      = regexp.MustCompile(`(?i)(フル\s*hd|full\s*hd)`)
	portraitWd  = regexp.MustCompile(`(縦動画|縦長|縦型|縦向き)`)
	landscapeWd = regexp.MustCompile(`(横動画|横長|横型|横向き)`)
	squareWd    = regexp.MustCompile(`(正方形|スクエア)`)
)

type kind int

const (
	kindNone kind = iota
	kindRender
	kindDownload
	kindBundle
)

// classify says which of the three requests the message is, if it is one: a word of exporting or downloading beside what is exported (the video, the Project as a bundle).
func classify(message string) kind {
	if otherThing.MatchString(person.ReplaceAllString(message, "")) {
		return kindNone
	}
	message = mentionedOnly.ReplaceAllString(message, " ")
	exports, downloads := exportWord.MatchString(message), downloadWord.MatchString(message)
	if !exports && !downloads {
		return kindNone
	}
	video := videoWord.MatchString(message)
	switch {
	case bundleWord.MatchString(message) || (!video && (namesWholeProject(message) || projectWord.MatchString(message))): // a word of the video beside "プロジェクト全体を…" is the video's
		return kindBundle
	case exportThenDownload.MatchString(message) && (video || demonstrated.MatchString(message)): // the render's own reply gives the download
		return kindRender
	case downloads:
		if video || pastFile.MatchString(message) {
			return kindDownload
		}
	case exports && (!pastExport.MatchString(message) || asksExportAgain(message)):
		if video || demonstrated.MatchString(message) {
			return kindRender
		}
	}
	return kindNone
}

// asksExportAgain reports whether the message holds a verb that asks for an export (exportWant, 「書き出して」, 「書き出す」) that no narration follows (narratedTe, narratedSuru), and that is not asked about (asked) without an explicit ask (explicitAsk).
func asksExportAgain(message string) bool {
	if asked.MatchString(message) && !explicitAsk.MatchString(message) { // a question about the export, not a request for it
		return false
	}
	if exportWant.MatchString(message) {
		return true
	}
	for _, m := range exportTe.FindAllStringIndex(message, -1) {
		if rest := message[m[1]:]; askedTe.MatchString(rest) || !narratedTe.MatchString(rest) {
			return true
		}
	}
	for _, m := range exportSuru.FindAllStringIndex(message, -1) {
		if !narratedSuru.MatchString(message[m[1]:]) {
			return true
		}
	}
	return false
}

// namesWholeProject reports whether the message says "the whole Project" as what is exported ("プロジェクト全体を書き出して", "まるごとダウンロード", "プロジェクト全体の書き出し"). "プロジェクト全体の雰囲気を…" says
// the whole Project as the thing a direction is about, so a 「の」 after it counts only when an export noun follows.
func namesWholeProject(message string) bool {
	for _, m := range wholeWord.FindAllStringSubmatchIndex(message, -1) {
		if m[5] == m[4] || exportNoun.MatchString(message[m[1]:]) {
			return true
		}
	}
	return false
}

// AsksRender reports whether the message asks to render the video: it names the export and the video (or "これ"). A message that tells of an export ("書き出した動画") is not.
func AsksRender(message string) bool { return classify(message) == kindRender }

// AsksDownload reports whether the message asks for the link of a video that was exported.
func AsksDownload(message string) bool { return classify(message) == kindDownload }

// AsksBundle reports whether the message asks for the Project as a ZIP bundle.
func AsksBundle(message string) bool { return classify(message) == kindBundle }

func detectRender(message string) []commandinterpreter.Operation {
	if !AsksRender(message) {
		return nil
	}
	op, err := commandinterpreter.NewOperation(OpExportVideo, Args{AspectRatio: aspectIn(message), Resolution: resolutionIn(message), Codec: codecIn(message)})
	if err != nil {
		return nil
	}
	return []commandinterpreter.Operation{op}
}

func detectDownload(message string) []commandinterpreter.Operation {
	if !AsksDownload(message) {
		return nil
	}
	return []commandinterpreter.Operation{{Type: OpDownloadExport}}
}

func detectBundle(message string) []commandinterpreter.Operation {
	if !AsksBundle(message) {
		return nil
	}
	return []commandinterpreter.Operation{{Type: OpExportBundle}}
}

func aspectIn(message string) string {
	switch {
	case ratioWord.MatchString(message):
		return aspectOf(ratioWord.FindString(message))
	case portraitWd.MatchString(message):
		return "9:16"
	case landscapeWd.MatchString(message):
		return "16:9"
	case squareWd.MatchString(message):
		return "1:1"
	}
	return ""
}

func resolutionIn(message string) string {
	switch {
	case resolution.MatchString(message):
		return strings.ToLower(strings.Join(strings.Fields(resolution.FindString(message)), ""))
	case fourK.MatchString(message):
		return "2160p"
	case fullHD.MatchString(message):
		return "1080p"
	}
	return ""
}

func codecIn(message string) string {
	switch {
	case codecH265.MatchString(message):
		return domain.CodecH265
	case codecH264.MatchString(message):
		return domain.CodecH264
	case codecVP9.MatchString(message):
		return domain.CodecVP9
	}
	return ""
}
