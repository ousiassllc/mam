package audiodirector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/media"
)

// LineGap is the pause left between two spoken lines.
const LineGap = 0.3

// Levels and fades of the mix.
const (
	// LayerFadeSeconds is the fade in / out of BGM and Ambience at the ends of a Scene.
	LayerFadeSeconds = 0.5
	// lineTolerance is how far a line may run past the end of the clip it is lip-synced to.
	lineTolerance = 0.1
)

// Input is what Produce makes the Audio Timeline of: the cut, its Scenes and the Project's policy.
type Input struct {
	ProjectID      string
	EditTimelineID string
	Brief          domain.Brief
	Template       domain.ProductionTemplate
	// Quality decides which layers are made (domain.AudioScopeOf).
	Quality domain.QualityPolicy
	// Scenes are the Scenes of the cut in the order they play.
	Scenes []Scene
	// Clips are the clips of the Edit Timeline with their Take files (used by Lip Sync).
	Clips []VideoClip
	// MusicFixed is true when the Project's Music is MANUAL or LOCK (spec/spec.md §6): the BGM is
	// not planned or made again, and FixedBGM, the BGM of the previous Audio Timeline, is carried
	// over as it is (none when there was none).
	MusicFixed bool
	FixedBGM   []domain.AudioClip
	// Voices maps a speaker name to the cloned voice that speaks their lines.
	Voices map[string]string
	// VoiceProvider is who speaks the lines ("" selects ElevenLabs). LocalVoices maps a speaker name (localvoice.SpeakerOf) to the
	// reference voice the local provider speaks their lines with; it is read for localvoice.Local only.
	VoiceProvider localvoice.Provider
	LocalVoices   map[string]localvoice.Voice
	// LipSyncProvider is who redoes the mouths of MAX QUALITY ("" selects Sync Labs).
	LipSyncProvider localvoice.LipSyncProvider
	// Rough is the rendered Rough Cut and HasSound tells whether it has sound of its own.
	Rough    string
	HasSound bool
	// Seconds is the length of the cut.
	Seconds float64
	// TargetLUFS is the loudness of the mix (0 selects domain.DefaultTargetLUFS).
	TargetLUFS float64
	// RunID names the directory of the run below <project>/audio/<edit timeline>/. Empty selects a
	// new ID; a caller that may have to DiscardRun the files after Produce succeeded sets it.
	RunID string
}

// Deps are what a Director needs.
type Deps struct {
	Providers Providers
	// Planner plans the BGM, Ambience, SE and Foley; nil selects the rule-based plan.
	Planner *Planner
	// Ledger records the cost and gives the budget ceiling; nil records nothing and has no ceiling.
	Ledger Ledger
	Tools  media.Tools
	// Root is the assets directory.
	Root string
	// Prices estimate the provider calls; the zero value selects DefaultPrices.
	Prices Prices
}

// Director makes the Audio Timeline (F-20).
type Director struct {
	Deps
	planner *Planner
}

// New returns a Director.
func New(deps Deps) *Director {
	if deps.Prices.LipSyncPerSecond == nil {
		deps.Prices = DefaultPrices()
	}
	d := &Director{Deps: deps, planner: deps.Planner}
	if d.planner == nil {
		d.planner = NewPlanner(nil)
	}
	return d
}

// Produce makes the Audio Timeline of the cut. Dialogue and Narration are always made (TTS, cloned
// voices where the speaker has one); the Quality Policy decides which of BGM, Ambience, SE, Foley
// and Lip Sync come on top of them. The layers are planned from the Scene Emotion and the editing
// tempo of each Scene, every provider call is recorded in the cost ledger, and an optional layer
// that the budget ceiling, a missing key or a provider failure does not allow is left out and
// listed in Skipped. Everything is then mixed with the sound of the Rough Cut and normalized with
// local DSP. A cut without lines and without sound gets no mix.
//
// A run that does not succeed removes its directory; the cost ledger keeps what was paid.
func (d *Director) Produce(ctx context.Context, in Input) (domain.AudioTimeline, error) {
	rel := runRel(in)
	if err := d.checkRunDir(in.ProjectID, rel); err != nil {
		return domain.AudioTimeline{}, err
	}
	tl, err := d.produce(ctx, in, rel)
	if err != nil {
		_ = os.RemoveAll(filepath.Join(d.Root, filepath.FromSlash(rel)))
		return domain.AudioTimeline{}, err
	}
	return tl, nil
}

// DiscardRun removes the files of the run that Produce made for in (in.RunID must be the one given
// to Produce). It is for the caller whose own step after a successful Produce failed, so that the
// files no Audio Timeline refers to do not pile up.
func (d *Director) DiscardRun(in Input) error {
	if in.RunID == "" {
		return nil
	}
	rel := runRel(in)
	if err := d.checkRunDir(in.ProjectID, rel); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(d.Root, filepath.FromSlash(rel)))
}

// runRel is the directory of the run of in, relative to the assets directory.
func runRel(in Input) string {
	id := in.RunID
	if id == "" {
		id = domain.NewID()
	}
	return path.Join(in.ProjectID, "audio", in.EditTimelineID, id)
}

func (d *Director) produce(ctx context.Context, in Input, rel string) (domain.AudioTimeline, error) {
	target := in.TargetLUFS
	if target == 0 {
		target = domain.DefaultTargetLUFS
	}
	if !in.LipSyncProvider.Valid() {
		return domain.AudioTimeline{}, fmt.Errorf("%w: %q", localvoice.ErrUnknownLipSyncProvider, in.LipSyncProvider)
	}
	scope := domain.AudioScopeOf(in.Quality)
	tl := domain.AudioTimeline{
		Mode: domain.AudioModeDirector, EditTimelineID: in.EditTimelineID, Quality: in.Quality,
		TargetLUFS: target, DurationSeconds: in.Seconds, Clips: []domain.AudioClip{},
	}
	var lines []domain.VoiceLine
	sceneOfLine := map[int]Scene{} // index in lines -> Scene
	for _, sc := range in.Scenes {
		for _, entry := range sc.Lines {
			if line, ok := domain.ParseVoiceLine(entry); ok {
				if line.Kind == domain.VoiceDialogue && in.VoiceProvider != localvoice.Local {
					line.VoiceID = in.Voices[line.Speaker]
				}
				sceneOfLine[len(lines)] = sc
				lines = append(lines, line)
			}
		}
	}
	say, err := d.speakerFor(ctx, in, lines)
	if err != nil {
		return domain.AudioTimeline{}, err
	}
	spend := &spending{ledger: d.Ledger, projectID: in.ProjectID}
	var speechCost float64
	for _, l := range lines {
		speechCost += say.price(l.Text)
	}
	// The speech is held up front, so that runs of the Project that start at the same time cannot
	// together pass the ceiling; it is released once the lines are in the ledger.
	releaseSpeech, err := spend.reserve(ctx, speechCost)
	if err != nil {
		return domain.AudioTimeline{}, err
	}
	defer releaseSpeech()

	r := &run{d: d, in: in, tl: &tl, spend: spend, scope: scope, rel: rel, speech: say}
	// The plan costs nothing outside: a planner failure ends the run before any provider is paid.
	if err := r.planScenes(ctx); err != nil {
		return domain.AudioTimeline{}, err
	}
	if err := r.speak(ctx, lines, sceneOfLine); err != nil {
		return domain.AudioTimeline{}, err
	}
	releaseSpeech() // the lines are in the ledger now
	tl.SpeechExpected = len(tl.Clips) > 0
	if err := r.makeLayers(ctx); err != nil {
		return domain.AudioTimeline{}, err
	}
	if err := r.lipSync(ctx); err != nil {
		return domain.AudioTimeline{}, err
	}
	tl.EstimatedCost = spend.estimated

	base := ""
	if in.HasSound {
		base = in.Rough
	}
	mixRel := path.Join(r.rel, "mix.m4a")
	loud, err := d.Tools.Master(ctx, base, r.tracks(), in.Seconds, target, filepath.Join(d.Root, filepath.FromSlash(mixRel)))
	switch {
	case errors.Is(err, media.ErrSilent):
		return tl, nil // nothing to hear: the Rough Cut has no sound and there is nothing to add
	case err != nil:
		return domain.AudioTimeline{}, err
	}
	tl.MixPath, tl.MeasuredLUFS, tl.TruePeakDB = mixRel, loud.Integrated, loud.TruePeak
	return tl, nil
}

// ErrUnsafePath is returned when the ID of the Project or the Edit Timeline would put the audio of
// the run outside the Project's own audio directory (a row ID of an imported bundle is not trusted).
var ErrUnsafePath = errors.New("audiodirector: the audio directory would be outside the Project's directory")

// checkRunDir makes sure rel, the directory of a run, lies below <projectID>/audio/ inside Root.
// It runs before anything is paid for.
func (d *Director) checkRunDir(projectID, rel string) error {
	if !strings.HasPrefix(rel, projectID+"/audio/") {
		return ErrUnsafePath
	}
	within, err := filepath.Rel(d.Root, filepath.Join(d.Root, filepath.FromSlash(rel)))
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) || filepath.IsAbs(within) {
		return ErrUnsafePath
	}
	return nil
}

// run is one Produce.
type run struct {
	d     *Director
	in    Input
	tl    *domain.AudioTimeline
	spend *spending
	scope domain.AudioScope
	// rel is the directory of the run, relative to the assets directory.
	rel  string
	plan Plan
	// speech is who speaks the lines of the run.
	speech speaker
}

func (r *run) abs(rel string) string { return filepath.Join(r.d.Root, filepath.FromSlash(rel)) }

// save writes generated audio into the run's directory and returns its path relative to the
// assets directory and its length.
func (r *run) save(ctx context.Context, name string, sp Speech) (string, float64, error) {
	rel := path.Join(r.rel, name+"."+sp.Ext)
	abs := r.abs(rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return "", 0, fmt.Errorf("create audio directory: %w", err)
	}
	if err := fsutil.WriteFileAtomic(abs, sp.Data, 0o600); err != nil {
		return "", 0, fmt.Errorf("write audio: %w", err)
	}
	length, err := r.d.Tools.AudioDuration(ctx, abs)
	if err != nil {
		return "", 0, err
	}
	return rel, length, nil
}

// skip lists a layer or step that was left out.
func (r *run) skip(layer domain.AudioLayer, prompt, reason, detail string) {
	r.tl.Skipped = append(r.tl.Skipped, domain.SkippedAudio{Layer: layer, Prompt: prompt, Reason: reason, Detail: detail})
}

// tracks are the clips of the timeline as the tracks of the mix.
func (r *run) tracks() []media.Track {
	out := make([]media.Track, 0, len(r.tl.Clips))
	for _, c := range r.tl.Clips {
		tr := media.Track{Path: r.abs(c.Path), Start: c.StartSeconds, GainDB: c.GainDB}
		switch c.LayerOf() {
		case domain.LayerBGM, domain.LayerAmbience:
			tr.Length, tr.Loop, tr.Fade = c.DurationSeconds, c.Loop, LayerFadeSeconds
		case domain.LayerSE, domain.LayerFoley:
			tr.Length = c.DurationSeconds
		}
		out = append(out, tr)
	}
	return out
}
