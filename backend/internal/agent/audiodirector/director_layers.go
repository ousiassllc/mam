package audiodirector

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// speak makes Dialogue and Narration. Lines are spoken one after another: a line starts at its
// Scene's start, or right after the previous line when that is later.
func (r *run) speak(ctx context.Context, lines []domain.VoiceLine, sceneOfLine map[int]Scene) error {
	cursor := 0.0
	for i, line := range lines {
		cursor = max(cursor, sceneOfLine[i].Start)
		speech, err := r.speech.say(ctx, line)
		if err != nil {
			return err
		}
		layer := domain.LayerDialogue
		if line.Kind == domain.VoiceNarration {
			layer = domain.LayerNarration
		}
		// The provider has charged for the line: the ledger gets it before the local work can fail.
		if err := r.spend.record(ctx, r.speech.model, r.speech.price(line.Text)); err != nil {
			return err
		}
		rel, length, err := r.save(ctx, fmt.Sprintf("line-%02d", i+1), speech)
		if err != nil {
			return err
		}
		r.tl.Clips = append(r.tl.Clips, domain.AudioClip{
			VoiceLine: line, Layer: layer, Model: r.speech.model, Path: rel, StartSeconds: cursor, DurationSeconds: length,
		})
		cursor += length + LineGap
	}
	return nil
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
	if j.call == nil {
		r.skip(j.layer, j.prompt, domain.SkipError, "プロバイダーが設定されていません")
		return nil
	}
	release, ok, err := r.reserve(ctx, j.layer, j.prompt, j.cost)
	if err != nil || !ok {
		return err
	}
	defer release() // after the cost is in the ledger, or when the call was not paid
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
