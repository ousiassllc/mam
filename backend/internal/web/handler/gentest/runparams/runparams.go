// Package runparams says which settings one run of the generation test was started with, so that the blocks of several runs on
// the screen can be told apart and each one matched to its entry of the history (#1109). It is a package of its own because
// handler/gentest is at its line limit.
package runparams

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/settingstext"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

// Started is when the run was started, to the second (two runs of the same minute are told apart by it and by the id).
func Started(t time.Time) string { return t.Local().Format("2006-01-02 15:04:05") }

// Of are the settings of the run as lines of its block: what the confirmed request set besides the models and the prompts (the
// grid names those). Settings the request left to the models' defaults are not listed.
func Of(st gentest.BatchStatus) []view.KV {
	if st.Estimate == nil {
		return nil
	}
	r := st.Estimate.Request
	var kv []view.KV
	add := func(key, value string) { kv = append(kv, view.KV{Key: key, Value: value}) }
	add("1 組あたりの枚数", strconv.Itoa(max(r.Count, 1)))
	if r.AspectRatio != "" {
		add("縦横比", r.AspectRatio)
	}
	if r.Resolution != "" {
		add("解像度", r.Resolution)
	}
	if r.DurationSeconds > 0 {
		add("長さ", strconv.FormatFloat(r.DurationSeconds, 'f', -1, 64)+" 秒")
	}
	if r.Seed != nil {
		add("シード", strconv.FormatInt(*r.Seed, 10))
	}
	if r.Quality != "" {
		add("品質", string(r.Quality))
	}
	if r.SpeedMode != "" {
		add("速さのモード", r.SpeedMode)
	}
	if !r.Sampling.IsZero() {
		add("サンプリング", settingstext.SamplingText(record.ResultView{Sampling: &r.Sampling}))
	}
	if r.LoRA != nil {
		add("LoRA", r.LoRA.Name)
	}
	if n := len(r.References); n > 0 {
		add("入力画像", fmt.Sprintf("%d 枚", n))
	}
	if r.AvoidReferencePose {
		add("参照のポーズを写さない", "オン")
	}
	if r.TranslateToEnglish {
		add("英語に訳して送る", "オン")
	}
	return kv
}
