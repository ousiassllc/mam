// Package voicetrainpanel renders the voice training of a Character (Issue #936, spec/architecture/model-manager/local-runtime.md §18): under the
// reference voice form, the clips of the speaker, the amount of data, the start, the progress and the cancel of a training, and the trained voice.
// The Series page and the World State page hold a placeholder that loads it; the panel replaces itself with every answer and polls while a
// training runs.
package voicetrainpanel

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
)

// PollEvery is how often a panel of a running training asks for itself again (htmx trigger).
const PollEvery = "3s"

// ChangedEvent is the htmx event the reference voice form raises (HX-Trigger) when the reference voice was set or removed: a panel that shows
// "set the reference voice first" loads itself again.
const ChangedEvent = "voice-reference-changed"

// State is where the training of the Character stands, as the handler tells it: "" for none in this process, else the state of the latest one.
const (
	StateRunning   = "running"
	StateFailed    = "failed"
	StateCancelled = "cancelled"
)

// Clip is one training clip of the list.
type Clip struct {
	ID       string
	Text     string
	Duration time.Duration
}

// Limits are the amounts of data a training takes.
type Limits struct {
	MinClipSeconds, MaxClipSeconds, MinTotalSeconds, MaxTotalSeconds, MaxClips int
}

// Epochs are the settings of a training.
type Epochs struct{ SoVITS, GPT int }

// Data is one panel (what the handler makes of the summary of the training service; the presentation layer does not know the service). URL is
// .../characters/:characterId/voice-training of the Series or of the Project: it answers GET (the panel), POST (start), POST /cancel, POST
// /clips (a clip), DELETE /clips and /clips/:id, and DELETE /voice (the trained voice).
type Data struct {
	DOMID string
	URL   string
	// Name is the Character, for the accessible names of the controls.
	Name         string
	HasReference bool
	Clips        []Clip
	DataSeconds  float64
	// Problems say, in words, why a training cannot start with the data there is.
	Problems []string
	Limits   Limits
	Defaults Epochs
	Max      Epochs
	Trained  *voiceref.Finetune
	GPUBusy  bool // another Character's training holds the GPU
	// State, Percent and Text are the latest training of this process; Failure (or else Error, in words) is why one that failed failed.
	State   string
	Percent int
	Text    string
	Failure *apperr.Error
	Error   string
}

// Placeholder is the data of the element a page holds in place of the panel: it loads the panel and loads it again when the reference voice changes.
type Placeholder struct {
	DOMID string
	URL   string
}

func (d Data) running() bool { return d.State == StateRunning }

func (d Data) canStart() bool {
	return d.HasReference && len(d.Problems) == 0 && !d.running() && !d.GPUBusy
}

// failed is the training that ended badly and has not been replaced: the panel tells why.
func (d Data) failed() bool { return d.State == StateFailed }

func (d Data) cancelled() bool { return d.State == StateCancelled }

func (d Data) failure() apperr.View {
	if d.Failure != nil {
		return d.Failure.View(apperr.LangJa)
	}
	return apperr.New(apperr.CodeTrainFailed).WithDetail(d.Error).View(apperr.LangJa)
}

func (d Data) percent() string { return strconv.Itoa(d.Percent) }

// seconds says the length of the data: "62.5 秒（1 分 2 秒）".
func seconds(s float64) string {
	if s < 60 {
		return fmt.Sprintf("%.1f 秒", s)
	}
	return fmt.Sprintf("%.1f 秒（%s）", s, clock(int(s)))
}

func clipSeconds(c Clip) string {
	return fmt.Sprintf("%.1f 秒", c.Duration.Round(100*time.Millisecond).Seconds())
}

func limits(l Limits) string {
	return fmt.Sprintf("1 つ %d〜%d 秒・合計 %d 秒（%d 分）〜%d 分・%d 個まで", l.MinClipSeconds, l.MaxClipSeconds, l.MinTotalSeconds, l.MinTotalSeconds/60, l.MaxTotalSeconds/60, l.MaxClips)
}

// trained says the trained voice: "v2・SoVITS 8 エポック・GPT 15 エポック・音声 6 個（60 秒）・2026-11-04 12:00".
func trained(f *voiceref.Finetune) string {
	return fmt.Sprintf("%s・SoVITS %d エポック・GPT %d エポック・音声 %d 個（%d 秒）・%s", f.Version, f.SoVITSEpochs, f.GPTEpochs, f.Clips, f.DataSeconds, f.TrainedAt.Local().Format("2006-01-02 15:04"))
}

// clock writes seconds as "45秒", "3分20秒" or "1時間2分".
func clock(seconds int) string {
	switch {
	case seconds >= 3600:
		return fmt.Sprintf("%d時間%d分", seconds/3600, seconds%3600/60)
	case seconds >= 60:
		return fmt.Sprintf("%d分%d秒", seconds/60, seconds%60)
	}
	return fmt.Sprintf("%d秒", seconds)
}

func itoa(n int) string { return strconv.Itoa(n) }
