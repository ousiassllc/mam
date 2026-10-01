package audiodirector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

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

// VideoClip is a clip of the Edit Timeline with the file of its Take.
type VideoClip struct {
	// Index is the 1-based position of the clip in the Edit Timeline.
	Index  int
	TakeID string
	// Path is the Take file (absolute).
	Path string
	// In and Out are the seconds of the Take the clip plays; Start is where it begins in the cut.
	In, Out, Start float64
}

// Length is how long the clip plays.
func (c VideoClip) Length() float64 { return c.Out - c.In }

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
				if line.Kind == domain.VoiceDialogue {
					line.VoiceID = in.Voices[line.Speaker]
				}
				sceneOfLine[len(lines)] = sc
				lines = append(lines, line)
			}
		}
	}
	if len(lines) > 0 && d.Providers.Speech == nil {
		return domain.AudioTimeline{}, ErrNoAPIKey
	}
	spend, err := newSpending(ctx, d.Ledger, in.ProjectID)
	if err != nil {
		return domain.AudioTimeline{}, err
	}
	var speechCost float64
	for _, l := range lines {
		speechCost += d.Prices.TTS(l.Text)
	}
	if !spend.fits(speechCost) {
		return domain.AudioTimeline{}, &BudgetError{Needed: speechCost, Remaining: spend.remaining()}
	}

	r := &run{d: d, in: in, tl: &tl, spend: spend, scope: scope, rel: rel}
	// The plan costs nothing outside: a planner failure ends the run before any provider is paid.
	if err := r.planScenes(ctx); err != nil {
		return domain.AudioTimeline{}, err
	}
	if err := r.speak(ctx, lines, sceneOfLine); err != nil {
		return domain.AudioTimeline{}, err
	}
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

// speak makes Dialogue and Narration. Lines are spoken one after another: a line starts at its
// Scene's start, or right after the previous line when that is later.
func (r *run) speak(ctx context.Context, lines []domain.VoiceLine, sceneOfLine map[int]Scene) error {
	cursor := 0.0
	for i, line := range lines {
		cursor = max(cursor, sceneOfLine[i].Start)
		speech, err := r.d.Providers.Speech.Synthesize(ctx, line)
		if err != nil {
			return err
		}
		layer := domain.LayerDialogue
		if line.Kind == domain.VoiceNarration {
			layer = domain.LayerNarration
		}
		// The provider has charged for the line: the ledger gets it before the local work can fail.
		if err := r.spend.record(ctx, domain.ModelElevenLabsTTS, r.d.Prices.TTS(line.Text)); err != nil {
			return err
		}
		rel, length, err := r.save(ctx, fmt.Sprintf("line-%02d", i+1), speech)
		if err != nil {
			return err
		}
		r.tl.Clips = append(r.tl.Clips, domain.AudioClip{
			VoiceLine: line, Layer: layer, Model: domain.ModelElevenLabsTTS, Path: rel, StartSeconds: cursor, DurationSeconds: length,
		})
		cursor += length + LineGap
	}
	return nil
}

// skip lists a layer or step that was left out.
func (r *run) skip(layer domain.AudioLayer, prompt, reason, detail string) {
	r.tl.Skipped = append(r.tl.Skipped, domain.SkippedAudio{Layer: layer, Prompt: prompt, Reason: reason, Detail: detail})
}

// job is one optional sound to generate.
type job struct {
	layer domain.AudioLayer
	model domain.ModelName
	// prompt is what the sound is made from; start and length place it on the timeline.
	prompt        string
	start, length float64
	// seconds is how long a sound to generate; loop asks for one that repeats and plays in a loop.
	seconds float64
	loop    bool
	cost    float64
	// call is nil when the provider is not configured.
	call func(ctx context.Context) (Speech, error)
}

// planScenes reads the Scenes (emotion, tempo) and plans their layers.
func (r *run) planScenes(ctx context.Context) error {
	in := r.in
	plan, err := r.d.planner.Plan(ctx, PlanInput{Brief: in.Brief, Template: in.Template, Scenes: in.Scenes, Scope: r.scope})
	if err != nil {
		return err
	}
	r.plan = plan
	for _, sc := range in.Scenes {
		as := domain.AudioScene{
			Index: sc.Index, Title: sc.Title, Start: sc.Start, End: sc.End, Tempo: sc.Tempo(), Emotion: plan[sc.Index].Emotion, Clips: sc.Clips,
		}
		if sc.Clips > 0 {
			as.AvgClipSeconds = sc.Length() / float64(sc.Clips)
		}
		r.tl.Scenes = append(r.tl.Scenes, as)
	}
	for _, l := range domain.PlannedLayers() {
		if !r.scope.Has(l) {
			r.skip(l, "", domain.SkipPolicy, in.Quality.Label()+" では作りません")
		}
	}
	return nil
}

// makeLayers makes BGM, Ambience, SE and Foley in that priority: what the budget cannot pay for
// comes last.
func (r *run) makeLayers(ctx context.Context) error {
	for _, layer := range []domain.AudioLayer{domain.LayerBGM, domain.LayerAmbience, domain.LayerSE, domain.LayerFoley} {
		if layer == domain.LayerBGM && r.in.MusicFixed {
			r.tl.Clips = append(r.tl.Clips, r.in.FixedBGM...)
			continue
		}
		for _, j := range r.jobs(layer) {
			if err := r.make(ctx, j); err != nil {
				return err
			}
		}
	}
	return nil
}

// jobs are the sounds of the layer the plan asks for.
func (r *run) jobs(layer domain.AudioLayer) []job {
	if !r.scope.Has(layer) {
		return nil
	}
	prices, prov := r.d.Prices, r.d.Providers
	var out []job
	for _, sc := range r.in.Scenes {
		sp := r.plan[sc.Index]
		switch layer {
		case domain.LayerBGM:
			if sp.BGM == "" || sc.Length() <= 0 {
				continue
			}
			secs := min(max(sc.Length(), MinMusicSeconds), MaxMusicSeconds)
			j := job{layer: layer, model: domain.ModelElevenLabsMusic, prompt: sp.BGM, start: sc.Start, length: sc.Length(), seconds: secs, cost: prices.Music(secs)}
			if prov.Music != nil {
				j.call = func(ctx context.Context) (Speech, error) { return prov.Music.Compose(ctx, j.prompt, j.seconds) }
			}
			out = append(out, j)
		case domain.LayerAmbience:
			if sp.Ambience == "" || sc.Length() <= 0 {
				continue
			}
			secs := min(max(sc.Length(), MinSFXSeconds), MaxSFXSeconds)
			j := job{layer: layer, model: domain.ModelElevenLabsSFX, prompt: sp.Ambience, start: sc.Start, length: sc.Length(), seconds: secs, loop: true, cost: prices.SFX(secs)}
			if prov.Sounds != nil {
				j.call = func(ctx context.Context) (Speech, error) { return prov.Sounds.Generate(ctx, j.prompt, j.seconds, true) }
			}
			out = append(out, j)
		default:
			for _, e := range sp.Effects {
				if e.Layer != layer {
					continue
				}
				j := job{layer: layer, model: domain.ModelElevenLabsSFX, prompt: e.Prompt, start: sc.Start + e.At, length: e.Seconds, seconds: e.Seconds, cost: prices.SFX(e.Seconds)}
				if prov.Sounds != nil {
					j.call = func(ctx context.Context) (Speech, error) {
						return prov.Sounds.Generate(ctx, j.prompt, j.seconds, false)
					}
				}
				out = append(out, j)
			}
		}
	}
	return out
}

// make generates the sound of the job and places it. A sound that cannot be made (no provider,
// over budget, the provider failed) is listed in Skipped; only a cancelled run or a failure of the
// local side (disk, ledger) is an error.
func (r *run) make(ctx context.Context, j job) error {
	switch {
	case j.call == nil:
		r.skip(j.layer, j.prompt, domain.SkipError, "プロバイダーが設定されていません")
		return nil
	case !r.spend.fits(j.cost):
		r.skip(j.layer, j.prompt, domain.SkipBudget, fmt.Sprintf("見積 $%.2f に対し残り予算が $%.2f です", j.cost, r.spend.remaining()))
		return nil
	}
	speech, err := j.call(ctx)
	if err == nil {
		// The provider has charged for the sound: the ledger gets it before the local work or a
		// cancelled run can lose it.
		if err := r.spend.record(ctx, j.model, j.cost); err != nil {
			return err
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		r.skip(j.layer, j.prompt, domain.SkipError, err.Error())
		return nil
	}
	n := len(r.tl.Clips) + 1
	rel, length, err := r.save(ctx, fmt.Sprintf("%s-%02d", j.layer, n), speech)
	if err != nil {
		r.skip(j.layer, j.prompt, domain.SkipError, err.Error())
		return nil
	}
	clip := domain.AudioClip{
		Layer: j.layer, Prompt: j.prompt, Model: j.model, Path: rel, StartSeconds: j.start,
		DurationSeconds: j.length, Loop: j.loop, GainDB: j.layer.MixGainDB(),
	}
	switch {
	case j.loop:
		clip.Loop = length < j.length-0.25 // a file as long as the Scene needs no loop
	default:
		clip.DurationSeconds = min(length, j.length)
	}
	r.tl.Clips = append(r.tl.Clips, clip)
	return nil
}

// lipSync redoes the mouth of the clips where a character speaks (MAX QUALITY): a clip whose
// Dialogue lines all start and end inside it gets the lines placed at their moments as the audio
// Sync Labs follows. A clip with a line across its end is given up as a whole (a mouth redone
// without that line would not match the mix). The new picture replaces the Take's at export.
func (r *run) lipSync(ctx context.Context) error {
	if !r.scope.LipSync {
		r.skip(domain.LayerLipSync, "", domain.SkipPolicy, r.in.Quality.Label()+" では作りません")
		return nil
	}
	groups := map[int][]domain.AudioClip{} // clip index -> its lines
	crossed := map[int]bool{}              // clip index -> a line runs over its end
	for _, c := range r.tl.Clips {
		if c.LayerOf() != domain.LayerDialogue || c.Speaker == "" {
			continue
		}
		vi := slices.IndexFunc(r.in.Clips, func(v VideoClip) bool {
			return c.StartSeconds >= v.Start-lineTolerance && c.StartSeconds < v.Start+v.Length()
		})
		switch {
		case vi < 0:
			r.skip(domain.LayerLipSync, c.Text, domain.SkipError, "セリフがどのクリップにも入っていません")
		case c.End() > r.in.Clips[vi].Start+r.in.Clips[vi].Length()+lineTolerance:
			r.skip(domain.LayerLipSync, c.Text, domain.SkipError, "セリフがクリップの境目をまたぎます")
			crossed[r.in.Clips[vi].Index] = true
		default:
			idx := r.in.Clips[vi].Index
			groups[idx] = append(groups[idx], c)
		}
	}
	for idx := range crossed {
		delete(groups, idx)
	}
	if len(groups) == 0 {
		return nil
	}
	if r.d.Providers.LipSync == nil {
		r.skip(domain.LayerLipSync, "", domain.SkipError, "Lip Sync のプロバイダーが設定されていません")
		return nil
	}
	model := r.scope.LipSyncModel
	for _, vc := range r.in.Clips {
		lines := groups[vc.Index]
		if len(lines) == 0 {
			continue
		}
		cost := r.d.Prices.LipSync(model, vc.Length())
		if !r.spend.fits(cost) {
			r.skip(domain.LayerLipSync, lines[0].Text, domain.SkipBudget, fmt.Sprintf("見積 $%.2f に対し残り予算が $%.2f です", cost, r.spend.remaining()))
			continue
		}
		out, stop, err := r.syncClip(ctx, vc, lines, model, cost)
		if err != nil {
			return err
		}
		if out != nil {
			r.tl.LipSync = append(r.tl.LipSync, *out)
		}
		if stop {
			break
		}
	}
	return nil
}

// syncClip lip-syncs one clip. The clip is nil when it was left out (listed in Skipped); stop is
// true when the following clips cannot be lip-synced either (no key). A call Sync Labs accepted is
// recorded at cost before its result is stored, so a local failure afterwards still counts.
func (r *run) syncClip(ctx context.Context, vc VideoClip, lines []domain.AudioClip, model string, cost float64) (out *domain.LipSyncClip, stop bool, err error) {
	dir := filepath.Join(r.d.Root, filepath.FromSlash(r.rel), fmt.Sprintf("lipsync-%02d-work", vc.Index))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, fmt.Errorf("create audio directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	label := lines[0].Text
	video := filepath.Join(dir, "clip.mp4")
	if _, err := r.d.Tools.Compose(ctx, []media.Segment{{Path: vc.Path, Start: vc.In, End: vc.Out}}, video); err != nil {
		return nil, false, r.localFailure(ctx, err, label)
	}
	voices := make([]media.Track, len(lines))
	for i, l := range lines {
		voices[i] = media.Track{Path: r.abs(l.Path), Start: l.StartSeconds - vc.Start}
	}
	audio := filepath.Join(dir, "voice.wav")
	if err := r.d.Tools.PlaceVoices(ctx, voices, vc.Length(), audio); err != nil {
		return nil, false, r.localFailure(ctx, err, label)
	}
	vb, err := os.ReadFile(video) // #nosec G304 -- a file this run just wrote
	if err != nil {
		return nil, false, fmt.Errorf("read clip: %w", err)
	}
	ab, err := os.ReadFile(audio) // #nosec G304 -- a file this run just wrote
	if err != nil {
		return nil, false, fmt.Errorf("read voice: %w", err)
	}
	data, err := r.d.Providers.LipSync.Sync(ctx, LipSyncRequest{Model: model, Video: Sample{Filename: "clip.mp4", Data: vb}, Audio: Sample{Filename: "voice.wav", Data: ab}})
	if err == nil {
		// Sync Labs has charged for the clip: the ledger gets it even when the run was cancelled meanwhile.
		if err := r.spend.record(ctx, domain.ModelSyncLabs, cost); err != nil {
			return nil, false, err
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, false, ctxErr
	}
	if errors.Is(err, ErrNoSyncLabsKey) {
		r.skip(domain.LayerLipSync, "", domain.SkipError, "Sync Labs の API キーが設定されていません（設定画面で synclabs のキーを登録してください）")
		return nil, true, nil
	}
	if err != nil {
		r.skip(domain.LayerLipSync, label, domain.SkipError, err.Error())
		return nil, false, nil
	}
	rel := path.Join(r.rel, fmt.Sprintf("lipsync-%02d.mp4", vc.Index))
	if err := fsutil.WriteFileAtomic(r.abs(rel), data, 0o600); err != nil {
		return nil, false, fmt.Errorf("write lip sync: %w", err)
	}
	info, err := r.d.Tools.Probe(ctx, r.abs(rel))
	if err != nil {
		_ = os.Remove(r.abs(rel))
		r.skip(domain.LayerLipSync, label, domain.SkipError, "Lip Sync の映像を読み取れませんでした: "+err.Error())
		return nil, false, nil
	}
	return &domain.LipSyncClip{ClipIndex: vc.Index, TakeID: vc.TakeID, Path: rel, DurationSeconds: info.Duration, Model: domain.ModelSyncLabs}, false, nil
}

// localFailure lists a clip that ffmpeg could not prepare; a cancelled run is an error.
func (r *run) localFailure(ctx context.Context, err error, label string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	r.skip(domain.LayerLipSync, label, domain.SkipError, "Lip Sync の入力を作れませんでした: "+err.Error())
	return nil
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
