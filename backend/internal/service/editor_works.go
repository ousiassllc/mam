package service

import "github.com/ousiassllc/mam/backend/internal/worktrack"

// Works is the export, the audio stage and the Editor Judge of this process, for the job list (worktrack.Source): the ones that run
// and the latest that ended. They have no row in the job table, so this is memory only and a restart forgets them. A stage that is
// refused before it starts (the Project is in another state, the stage is busy) is not recorded.
func (s *EditorService) Works() []worktrack.Record { return s.works.Works() }

// track records a stage of the Project as running; the caller ends it (Run.End) with the error of the stage.
func (s *EditorService) track(kind worktrack.Kind, projectID, text string) *worktrack.Run {
	return s.works.Begin(worktrack.Record{Kind: kind, ProjectID: projectID}, text)
}
