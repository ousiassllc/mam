package service

import "context"

// ResumeEditing asks the worker to look for editing follow-ups: additional Shots of the Reshoot Loop
// that are ready to go into their Edit Timeline, and AUTO Projects whose editing stage can start.
// Call it whenever a job finished or a Take was selected.
func (s *EditorService) ResumeEditing() { s.resume.Wake() }

// Close stops the background worker of ResumeEditing.
func (s *EditorService) Close() { s.resume.Close() }

func (s *EditorService) resumeAll(ctx context.Context) {
	s.resumeReshoots(ctx)
	s.resumeAuto(ctx)
}
