package export_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/service/chatops/export"
)

// The verb forms that urge an export ("書き出そう", "書き出せ", "エクスポートしよう") and "書き出してますぐ確認して" (「書き出して」＋「すぐ」) are requests, also beside a past export that would make the
// message a story ("書き出した動画を…").
func TestDetect_UrgingFormsAndImmediatelyAreRequests(t *testing.T) {
	for _, message := range []string{
		"動画を書き出そう",
		"この動画を書き出そう！",
		"動画を書き出そうよ",
		"動画を書き出そうか？",
		"動画を書き出せ",
		"この動画を書き出せ。",
		"動画を書き出せよ",
		"動画を書き出し直そう",
		"動画を書き出し直せ",
		"動画をエクスポートしよう",
		"動画をエクスポートしろ",
		"動画をエクスポートせよ",
		"動画を書き出してますぐ確認して",
		"書き出した動画を書き出そう",
		"さっき書き出した動画をもう一度書き出そう",
		"書き出した動画を書き出せ",
		"書き出した動画を書き出し直せ",
		"書き出した動画をエクスポートしよう",
		"書き出した動画をエクスポートしろ",
		"書き出した動画を書き出してますぐ確認して",
		"書き出した動画をエクスポートしてますぐ確認して",
		"書き出した動画を書き出してますぐ",
	} {
		assert.Contains(t, detect(t, message), export.OpExportVideo, message)
		assert.True(t, export.AsksRender(message), message)
	}
}

// What only tells of an export, or asks whether it can be done, is no request beside a past export: it posts no card. (Without a past export in the sentence the word and the video are
// enough, as before.)
func TestDetect_NarrationAndConfirmationBesideAPastExportAreNotRequests(t *testing.T) {
	for _, message := range []string{
		"書き出した動画を書き出してよかった？",
		"書き出した動画をエクスポートしてみたけど音がない",
		"書き出した動画はもう書き出してます",
		"書き出した動画を書き出してました",
		"書き出した動画を書き出してる",
		"書き出した動画を書き出せる？",
		"書き出した動画を書き出せない",
		"書き出した動画を書き出せなかった",
		"書き出した動画を書き出せて良かった",
		"書き出した動画を書き出せば",
		"書き出した動画を書き出そうと思ったけど",
		"書き出した動画を書き出そうとしたら止まった",
		"書き出した動画を書き出そうかな",
		"書き出した動画をエクスポートしようとしたけど",
		"書き出した動画をエクスポートしようかな",
		"書き出した動画をエクスポートしろと言われた",
	} {
		assert.Empty(t, detect(t, message), message)
		assert.False(t, export.AsksRender(message), message)
	}
}
