package service

// HoldStage claims the editing stage of the Project the way a running stage does.
func (s *EditorService) HoldStage(projectID string) (release func(), err error) {
	return s.acquire(projectID)
}

// AudioError is audioError: the error the user sees when the audio stage failed.
func AudioError(err error) error { return audioError(nil, err) }

// EditingWorkerIdle reports whether the worker of ResumeEditing has no run going on or asked for.
func (s *EditorService) EditingWorkerIdle() bool { return s.resume.Idle() }

// WaitProposals returns when every background promotion proposal started so far has ended. A test that
// asserts a note proposes nothing calls it first, so the absence is seen after the proposal ran.
func (s *NoteService) WaitProposals() { s.proposals.Wait() }
