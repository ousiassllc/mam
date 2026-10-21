package service

import "context"

// ResumeEditing asks the worker to look for editing follow-ups: Reshoot Loop Shots that are ready to go into
// their Edit Timeline, and AUTO Projects whose editing stage can start. Call it when a job finished or a Take was selected.
func (s *EditorService) ResumeEditing() { s.resume.Wake() }

// Busy reports whether an editing stage, an AUTO chain or the resume worker runs: the app updater waits for it.
func (s *EditorService) Busy() bool { return s.stage.Busy() || s.auto.Busy() || !s.resume.Idle() }

// Close stops the background worker of ResumeEditing.
func (s *EditorService) Close() { s.resume.Close() }

func (s *EditorService) resumeAll(ctx context.Context) {
	s.resumeReshoots(ctx)
	s.resumeAuto(ctx)
}
