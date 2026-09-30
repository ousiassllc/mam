package service

// HoldStage claims the editing stage of the Project the way a running stage does.
func (s *EditorService) HoldStage(projectID string) (release func(), err error) {
	return s.acquire(projectID)
}
