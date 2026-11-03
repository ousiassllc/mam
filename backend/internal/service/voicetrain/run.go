package voicetrain

import (
	"context"
	"errors"
	"time"

	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// The epochs of the two trainings: the defaults and the largest values are those of the sliders of the vendor's webui (SoVITS 8 and at most 25, GPT 15
// and at most 50).
const (
	DefaultEpochsSoVITS = 8
	MaxEpochsSoVITS     = 25
	DefaultEpochsGPT    = 15
	MaxEpochsGPT        = 50
)

// Options are what a training is started with; 0 is the default of the field.
type Options struct {
	EpochsSoVITS, EpochsGPT int
}

func (o Options) check() (Options, error) {
	if o.EpochsSoVITS == 0 {
		o.EpochsSoVITS = DefaultEpochsSoVITS
	}
	if o.EpochsGPT == 0 {
		o.EpochsGPT = DefaultEpochsGPT
	}
	if o.EpochsSoVITS < 1 || o.EpochsSoVITS > MaxEpochsSoVITS {
		return o, invalidf("SoVITS のエポック数は 1〜%d にしてください", MaxEpochsSoVITS)
	}
	if o.EpochsGPT < 1 || o.EpochsGPT > MaxEpochsGPT {
		return o, invalidf("GPT のエポック数は 1〜%d にしてください", MaxEpochsGPT)
	}
	return o, nil
}

// Start checks the request and starts the training of the Character in the background (Status, Cancel). It refuses at once, with the reason, when:
// the Character has no reference voice, the clips are not the amount of data a training takes (spec §16.3), another training runs, the bundled ComfyUI
// is generating, the runtime is not there (not supported on this machine, not installed, weights missing, an install running), or the disk lacks the
// room. Nothing has been sent to the GPU by then.
func (s *Service) Start(ctx context.Context, o voicerefsvc.Owner, characterID string, opt Options) (Status, error) {
	opt, err := opt.check()
	if err != nil {
		return Status{}, err
	}
	ref, err := s.reference(ctx, o, characterID)
	if err != nil {
		return Status{}, err
	}
	name, err := s.Voices.CharacterName(ctx, o, characterID)
	if err != nil {
		return Status{}, err
	}
	clips, err := s.Clips(ctx, o, characterID)
	if err != nil {
		return Status{}, err
	}
	if p := problems(clips); len(p) > 0 {
		return Status{}, invalidf("%s", p[0])
	}

	r := &run{id: newID(), key: keyOf(o, characterID), owner: o, done: make(chan struct{})}
	s.mu.Lock()
	if s.cur != nil {
		s.mu.Unlock()
		return Status{}, conflictf("ほかの登場人物の声を学習しています。GPU は 1 つなので、終わってから（または中止してから）始めてください")
	}
	s.cur = r
	s.mu.Unlock()
	started := false
	defer func() {
		if !started {
			s.release(r)
		}
	}()

	if s.GPU != nil {
		if err := s.GPU.Before(ctx); err != nil {
			return Status{}, gpuRefusal(err)
		}
	}
	held, err := s.Runtime.BeginTool(ctx, RuntimeID, Pretrained)
	if err != nil {
		return Status{}, runtimeRefusal(err)
	}
	defer func() {
		if !started {
			held.End()
		}
	}()
	secs := seconds(clips)
	if err := s.checkDisk(held.WorkDir(), reserveBytes+int64(secs)*perSecondBytes+clipBytes(clips)); err != nil {
		return Status{}, err
	}
	if err := s.checkDisk(s.Voices.WeightsRoot(), keptWeightsBytes); err != nil {
		return Status{}, err
	}

	now := s.Now()
	_, vram := held.GPU()
	j, err := s.prepare(o, characterID, r.id, held.WorkDir(), clips, ref.Language, opt, vram)
	if err != nil {
		return Status{}, err
	}
	rec := worktrack.Record{ID: r.id, Kind: worktrack.KindVoiceTrain, Subject: name}
	if o.IsSeries() {
		rec.SeriesID = o.ID()
	} else {
		rec.ProjectID = o.ID()
	}
	r.work = s.works.Begin(rec, "学習を始めています")
	runCtx, cancel := context.WithCancelCause(context.Background())
	r.cancel = cancel
	r.status = Status{ID: r.id, State: worktrack.Running, CharacterID: characterID, Character: name, Epochs: opt, StartedAt: now}
	r.stageAt = now
	s.remember(r)
	started = true
	s.wg.Add(1)
	go s.execute(runCtx, r, held, j, clips, secs)
	return r.snapshot(now), nil
}

// release lets the next training start.
func (s *Service) release(r *run) {
	s.mu.Lock()
	if s.cur == r {
		s.cur = nil
	}
	s.mu.Unlock()
}

// keepRuns is how many trainings of this process the Service remembers by id.
const keepRuns = 50

func (s *Service) remember(r *run) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.id] = r
	s.byKey[r.key] = r
	s.order = append(s.order, r.id)
	for len(s.order) > keepRuns {
		delete(s.runs, s.order[0])
		s.order = s.order[1:]
	}
}

// Status is the training with the id (the id of its job in the job list), false for one that is not known (an older one, or one of an earlier run of mam).
func (s *Service) Status(id string) (Status, bool) {
	s.mu.Lock()
	r := s.runs[id]
	s.mu.Unlock()
	if r == nil {
		return Status{}, false
	}
	return r.snapshot(s.Now()), true
}

// Current is the training of the Character that runs now, or the latest of this process that ended; false when this process has run none for it.
func (s *Service) Current(o voicerefsvc.Owner, characterID string) (Status, bool) {
	s.mu.Lock()
	r := s.byKey[keyOf(o, characterID)]
	s.mu.Unlock()
	if r == nil {
		return Status{}, false
	}
	return r.snapshot(s.Now()), true
}

// Busy reports whether a training (of any Character) holds the GPU.
func (s *Service) Busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur != nil
}

// Cancel stops the training of the Character and returns when it has: the processes are ended, its working directory and the weights it had written
// are removed. ErrNoRun when none of that Character is running.
func (s *Service) Cancel(o voicerefsvc.Owner, characterID string) error {
	s.mu.Lock()
	cur := s.cur
	s.mu.Unlock()
	if cur == nil || cur.key != keyOf(o, characterID) {
		return ErrNoRun
	}
	cur.cancel(errCancelled)
	select {
	case <-cur.done:
	case <-time.After(time.Minute):
		return errors.New("the training did not stop within a minute")
	}
	return nil
}
