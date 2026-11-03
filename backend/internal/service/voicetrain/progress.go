package voicetrain

import (
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
)

// stage is one process of the training: the step the driver is given, what it is called, and the share of the bar it fills (in percent of the
// whole). The shares are for the bar only (mam's own: the two trainings are most of the time), not a measure of the time each takes.
type stage struct {
	step, label string
	from, to    int
}

// stages are the steps of the training in order: the three that make the data (the vendor's "1A 1B 1C"), the SoVITS (S2) training and the GPT (S1)
// training. Only the two trainings count epochs.
var stages = []stage{
	{"text", "書き起こしの音素と BERT 特徴", 0, 5},
	{"hubert", "音声の特徴（SSL）", 5, 15},
	{"semantic", "セマンティックトークン", 15, 20},
	{"sovits", "SoVITS の学習", 20, 60},
	{"gpt", "GPT の学習", 60, 98},
}

// counts reports whether the stage tells how far it is in epochs (the trainings) or in files (hubert); the others only start and end.
func (st stage) counts() bool { return st.step == "hubert" || st.step == "sovits" || st.step == "gpt" }

// epochs reports whether the stage counts epochs (not files).
func (st stage) epochs() bool { return st.step == "sovits" || st.step == "gpt" }

// Progress is how far a running training is: the stage it is in, the count inside the stage and the time (Issue #936 asks for "epoch n of m, the time
// that has gone and the time that is left", in the shape of get_job's progress, Issue #1002).
type Progress struct {
	// Stage is the 1-based number of the stage and Label its name; Stages is how many there are.
	Stage, Stages int
	Label         string
	// Epoch counts the finished epochs of a training stage and Epochs is how many it will run (0, 0 for a stage that does not count epochs);
	// Done and Total count the files of the stage that counts them.
	Epoch, Epochs int
	Done, Total   int
	// Percent is how far the whole training is (0-99).
	Percent int
	// Elapsed is the time since the training started, StageElapsed the time since the stage did.
	Elapsed, StageElapsed time.Duration
}

// at is the Progress of stage i (0-based) with done of total counted.
func at(i, done, total int, elapsed, stageElapsed time.Duration) Progress {
	st := stages[i]
	p := Progress{Stage: i + 1, Stages: len(stages), Label: st.label, Percent: st.from, Elapsed: elapsed, StageElapsed: stageElapsed}
	if st.counts() && total > 0 {
		done = max(0, min(done, total))
		p.Percent = st.from + (st.to-st.from)*done/total
		if st.epochs() {
			p.Epoch, p.Epochs = done, total
		} else {
			p.Done, p.Total = done, total
		}
	}
	p.Percent = min(p.Percent, 99)
	return p
}

// eta is the time the rest of the stage's epochs take at the speed they have had: false before the first epoch is done and when none is left. It is
// the stage's only: the training does not know how long the next stage will take (the GPT is not the SoVITS).
func (p Progress) eta() (time.Duration, bool) {
	if p.Epochs == 0 || p.Epoch == 0 || p.Epoch >= p.Epochs {
		return 0, false
	}
	return p.StageElapsed / time.Duration(p.Epoch) * time.Duration(p.Epochs-p.Epoch), true
}

// View is the progress in the shape of get_job's "progress" (live.View): the stage as the node, its epoch as the step, the time since the training
// started and the time the rest of the stage needs. It has the epoch of a stage that counts them; a stage that does not has the stage number.
func (p Progress) View() *live.View {
	v := &live.View{Node: p.Label, Percent: p.Percent, ElapsedSeconds: int(p.Elapsed.Round(time.Second) / time.Second)}
	switch {
	case p.Epochs > 0:
		v.Step, v.MaxStep = p.Epoch, p.Epochs
	case p.Total > 0:
		v.Step, v.MaxStep = p.Done, p.Total
	default:
		v.Step, v.MaxStep = p.Stage, p.Stages
	}
	if eta, ok := p.eta(); ok {
		secs := int(eta.Round(time.Second) / time.Second)
		v.ETASeconds = &secs
	} else if p.Epochs > 0 && p.Epoch == 0 {
		v.ETAUnavailable = "最初のエポックが終わるまで、残り時間を見積もれません"
	}
	return v
}

// Text says it in words for the job list: "SoVITS の学習 エポック 3 / 8（2 / 5 段階）・経過 12分・この段階の残り約 20分".
func (p Progress) Text() string {
	what := p.Label
	switch {
	case p.Epochs > 0:
		what += fmt.Sprintf(" エポック %d / %d", p.Epoch, p.Epochs)
	case p.Total > 0:
		what += fmt.Sprintf(" %d / %d 件", p.Done, p.Total)
	}
	out := fmt.Sprintf("%s（%d / %d 段階）・経過 %s", what, p.Stage, p.Stages, Clock(int(p.Elapsed.Seconds())))
	if eta, ok := p.eta(); ok {
		out += "・この段階の残り約 " + Clock(int(eta.Seconds()))
	}
	return out
}

// Clock writes seconds as "45秒", "3分20秒" or "1時間2分".
func Clock(seconds int) string {
	switch {
	case seconds >= 3600:
		return fmt.Sprintf("%d時間%d分", seconds/3600, seconds%3600/60)
	case seconds >= 60:
		return fmt.Sprintf("%d分%d秒", seconds/60, seconds%60)
	}
	return fmt.Sprintf("%d秒", seconds)
}
