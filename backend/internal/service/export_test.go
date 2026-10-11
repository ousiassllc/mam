package service

// HoldStage claims the editing stage of the Project the way a running stage does.
func (s *EditorService) HoldStage(projectID string) (release func(), err error) {
	return s.acquire(projectID)
}

// AudioError is audioError: the error the user sees when the audio stage failed.
func AudioError(err error) error { return audioError(err) }
