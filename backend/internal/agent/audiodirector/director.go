package audiodirector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/media"
)

// LineGap is the pause left between two spoken lines.
const LineGap = 0.3

// Cue is the script lines of a Scene and the second of the Rough Cut the Scene begins at.
type Cue struct {
	Start float64
	Lines []string // "speaker: line" or a narration line (domain.ParseVoiceLine)
}

// Input is what the Audio Director works from.
type Input struct {
	ProjectID string
	// EditTimelineID is the Edit Timeline version the audio is made for; the files are kept in a
	// directory named after it.
	EditTimelineID string
	// Cues are the spoken lines in story order.
	Cues []Cue
	// Rough is the rendered Rough Cut (absolute path) and HasSound tells whether it has a sound track.
	Rough    string
	HasSound bool
	// Seconds is the length of the Rough Cut.
	Seconds float64
	// TargetLUFS is the loudness to normalize to; zero selects domain.DefaultTargetLUFS.
	TargetLUFS float64
}

// Director makes the simple Audio Timeline.
type Director struct {
	tts   Synthesizer
	tools media.Tools
	root  string
}

// New returns a Director that speaks through tts, mixes with tools and keeps its files under the
// assets directory root.
func New(tts Synthesizer, tools media.Tools, root string) *Director {
	return &Director{tts: tts, tools: tools, root: root}
}

// Produce speaks the lines, places them on the timeline, mixes them with the sound of the Rough Cut
// and normalizes the loudness. Lines are spoken one after another: a line starts at its Scene's
// start, or right after the previous line when that is later. A Project without lines gets only
// the normalized sound of the Rough Cut, and one with no sound at all gets no mix.
func (d *Director) Produce(ctx context.Context, in Input) (domain.AudioTimeline, error) {
	target := in.TargetLUFS
	if target == 0 {
		target = domain.DefaultTargetLUFS
	}
	tl := domain.AudioTimeline{EditTimelineID: in.EditTimelineID, TargetLUFS: target, DurationSeconds: in.Seconds, Clips: []domain.AudioClip{}}
	lines := 0
	for _, cue := range in.Cues {
		lines += len(cue.Lines)
	}
	tl.SpeechExpected = lines > 0
	if lines > 0 && d.tts == nil {
		return domain.AudioTimeline{}, ErrNoAPIKey
	}

	rel := path.Join(in.ProjectID, "audio", in.EditTimelineID)
	var voices []media.Voice
	cursor, n := 0.0, 0
	for _, cue := range in.Cues {
		cursor = max(cursor, cue.Start)
		for _, entry := range cue.Lines {
			line, ok := domain.ParseVoiceLine(entry)
			if !ok {
				continue
			}
			speech, err := d.tts.Synthesize(ctx, line)
			if err != nil {
				return domain.AudioTimeline{}, err
			}
			n++
			file := path.Join(rel, fmt.Sprintf("line-%02d.%s", n, speech.Ext))
			abs := filepath.Join(d.root, filepath.FromSlash(file))
			if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
				return domain.AudioTimeline{}, fmt.Errorf("create audio directory: %w", err)
			}
			if err := fsutil.WriteFileAtomic(abs, speech.Data, 0o600); err != nil {
				return domain.AudioTimeline{}, fmt.Errorf("write speech: %w", err)
			}
			length, err := d.tools.AudioDuration(ctx, abs)
			if err != nil {
				return domain.AudioTimeline{}, err
			}
			tl.Clips = append(tl.Clips, domain.AudioClip{VoiceLine: line, Path: file, StartSeconds: cursor, DurationSeconds: length})
			voices = append(voices, media.Voice{Path: abs, Start: cursor})
			cursor += length + LineGap
		}
	}
	tl.SpeechExpected = len(tl.Clips) > 0

	base := ""
	if in.HasSound {
		base = in.Rough
	}
	mixRel := path.Join(rel, "mix.m4a")
	loud, err := d.tools.Master(ctx, base, voices, in.Seconds, target, filepath.Join(d.root, filepath.FromSlash(mixRel)))
	switch {
	case errors.Is(err, media.ErrSilent):
		return tl, nil // nothing to hear: the Rough Cut has no sound and the script no lines
	case err != nil:
		return domain.AudioTimeline{}, err
	}
	tl.MixPath, tl.MeasuredLUFS, tl.TruePeakDB = mixRel, loud.Integrated, loud.TruePeak
	return tl, nil
}
