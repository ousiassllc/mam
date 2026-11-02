// Package failtext words the failures of the generation test that depend on where the call went.
package failtext

import (
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// LocalTimeout says what happened to a call to an endpoint the user registered that ran past its timeout, and what to do
// next. It says nothing about billing: a local call costs nothing per call, and mam cancels the job on the server. ok is
// false for any other error.
func LocalTimeout(err error) (msg string, ok bool) {
	var te *localcfg.TimeoutError
	if !errors.As(err, &te) {
		return "", false
	}
	return "ローカルの生成が接続先の待ち時間（" + duration(te.After) + "）を超えたため打ち切りました。設定画面の接続先でタイムアウトを延ばすか、解像度や秒数を小さくして実行し直してください", true
}

// duration is a time limit in words: whole hours, else whole minutes, else seconds.
func duration(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%d時間", d/time.Hour)
	case d >= time.Minute && d%time.Minute == 0:
		return fmt.Sprintf("%d分", d/time.Minute)
	}
	return fmt.Sprintf("%d秒", int(d.Round(time.Second)/time.Second))
}
