package appupdate

import (
	"context"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/bgwork"
)

// Schedule is when Start checks. The zero value is replaced by the defaults.
type Schedule struct {
	// FirstCheck is the wait before the first check, so that the start of the app is not slowed down
	// (and the network is up after an automatic launch).
	FirstCheck time.Duration
	// Interval is the wait between two checks that found nothing to retry.
	Interval time.Duration
	// InitialBackoff and MaxBackoff bound the wait before a repeated check: a check that failed for a
	// reason that may pass by itself (no network, GitHub rate limit) or whose newer release the safety
	// gate holds back is repeated after InitialBackoff, doubling up to MaxBackoff.
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// Defaults of Schedule.
const (
	DefaultFirstCheck     = time.Minute
	DefaultInterval       = 6 * time.Hour
	DefaultInitialBackoff = time.Minute
	DefaultMaxBackoff     = time.Hour
)

func (sc Schedule) withDefaults() Schedule {
	if sc.FirstCheck <= 0 {
		sc.FirstCheck = DefaultFirstCheck
	}
	if sc.Interval <= 0 {
		sc.Interval = DefaultInterval
	}
	if sc.InitialBackoff <= 0 {
		sc.InitialBackoff = DefaultInitialBackoff
	}
	if sc.MaxBackoff <= 0 {
		sc.MaxBackoff = DefaultMaxBackoff
	}
	return sc
}

// Start checks on schedule on a goroutine of its own until ctx ends or stop is called (stop waits for
// the check that is running; ctx cancels it). A build that does not check at all (a dev build) ends it
// after the first check.
func (s *Service) Start(ctx context.Context, sc Schedule) (stop func()) {
	sc = sc.withDefaults()
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		bgwork.RunSafely(s.logger, "update schedule", func() { s.loop(ctx, sc) })
	}()
	return func() { cancel(); wg.Wait() }
}

func (s *Service) loop(ctx context.Context, sc Schedule) {
	delay, backoff := sc.FirstCheck, sc.InitialBackoff
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		err := s.Check(ctx)
		switch {
		case ctx.Err() != nil, s.installing.Load(), s.checker.Status().DevBuild:
			return
		case err != nil && !retryable(ctx, err):
			s.logger.Error("update: check failed, not repeating before the next scheduled check", "err", err)
			delay, backoff = sc.Interval, sc.InitialBackoff
		case err != nil:
			s.logger.Error("update: check failed, repeating with backoff", "err", err, "in", backoff)
			delay, backoff = backoff, min(backoff*2, sc.MaxBackoff)
		case s.pending():
			s.logger.Info("update: a newer release is held back, repeating with backoff", "in", backoff)
			delay, backoff = backoff, min(backoff*2, sc.MaxBackoff)
		default:
			delay, backoff = sc.Interval, sc.InitialBackoff
		}
	}
}
