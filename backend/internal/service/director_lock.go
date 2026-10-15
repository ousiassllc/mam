package service

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// SetMode switches the Project between AUTO and DIRECTOR (PUT /projects/:id/mode) and tells the
// thread. Back in AUTO the pipeline looks at the Project again, so the stages after the ones the
// director fixed resume. The mode is kept: it is a column of the Project.
func (s *DirectorService) SetMode(ctx context.Context, projectID string, mode domain.Mode) (domain.Project, error) {
	if !mode.Valid() {
		return domain.Project{}, invalid("mode", "モードが正しくありません")
	}
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return domain.Project{}, err
	}
	if project.Mode == mode {
		return project, nil
	}
	if err := s.applyMode(ctx, projectID, mode, true); err != nil {
		return domain.Project{}, err
	}
	if mode == domain.ModeAuto {
		s.kick(ctx, projectID)
	}
	return s.getProject(ctx, projectID)
}

func modeNotice(mode domain.Mode) string {
	if mode == domain.ModeDirector {
		return "DIRECTOR MODE にしました。指示した箇所だけを直して待機します（AI が先に進めることはありません）。"
	}
	return "AUTO MODE に戻しました。この先の工程は AI が続けます。"
}

// applyMode switches the mode and, when announce, posts the notice of it. A direction that carries
// the switch reports it in its own reply instead (announce false), so that the thread says it once.
func (s *DirectorService) applyMode(ctx context.Context, projectID string, mode domain.Mode, announce bool) error {
	var msgs []domain.ChatMessage
	if announce {
		msgs = assignIDs([]domain.ChatMessage{assistantText(modeNotice(mode))})
	}
	if err := s.projects.Apply(ctx, repository.Turn{ProjectID: projectID, Messages: msgs, Mode: &mode}); err != nil {
		return notFound(err)
	}
	s.publishCreated(projectID, msgs)
	if mode == domain.ModeDirector {
		s.recordIntervention(ctx, domain.Intervention{ProjectID: projectID, Kind: domain.InterventionSwitchToDirector})
	}
	return nil
}

func checkLockInput(p domain.LockParameter, mode domain.LockMode, value string, valid bool) error {
	if !valid {
		return invalid("parameter", "このパラメータは設定できません")
	}
	if !mode.Valid() {
		return invalid("mode", "AUTO / MANUAL / LOCK のいずれかを選んでください")
	}
	if p != domain.ParamVideoModel {
		return nil
	}
	if value != "" && !slices.Contains(domain.VideoModels(), domain.ModelName(value)) {
		return invalid("value", "動画モデルが正しくありません")
	}
	if mode == domain.LockManual && value == "" {
		return invalid("value", "MANUAL では動画モデルを選んでください")
	}
	return nil
}

// SetShotLock sets one parameter of the Shot to AUTO, MANUAL or LOCK (PUT /shots/:id/locks) and
// returns the Shot's settings. value is the Video Model chosen with MANUAL or LOCK; LOCK without
// one freezes the model of the Shot's latest Take. Replacing the value of a LOCKed parameter that
// stays LOCKed is ErrLocked.
func (s *DirectorService) SetShotLock(ctx context.Context, shotID string, p domain.LockParameter, mode domain.LockMode, value string) (domain.ParameterLocks, error) {
	value = strings.TrimSpace(value)
	if err := checkLockInput(p, mode, value, p.ValidForShot()); err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if value != "" && mode != domain.LockAuto {
		raw, _ = json.Marshal(value)
	}
	if err := s.locks.SetShotLock(ctx, shotID, p, mode, raw); err != nil {
		return nil, lockedError(notFound(err))
	}
	if mode != domain.LockAuto {
		s.recordIntervention(ctx, domain.Intervention{ShotID: shotID, Kind: domain.InterventionLockChange})
	}
	return s.locks.ShotLocks(ctx, shotID)
}

// SetProjectLock sets Story or Music of the Project (PUT /projects/:id/locks). A LOCKed Story is
// never changed by the State Extractor.
func (s *DirectorService) SetProjectLock(ctx context.Context, projectID string, p domain.LockParameter, mode domain.LockMode) (domain.ParameterLocks, error) {
	if err := checkLockInput(p, mode, "", p.ValidForProject()); err != nil {
		return nil, err
	}
	if err := s.locks.SetProjectLock(ctx, projectID, p, mode); err != nil {
		return nil, notFound(err)
	}
	if mode != domain.LockAuto {
		s.recordIntervention(ctx, domain.Intervention{ProjectID: projectID, Kind: domain.InterventionLockChange})
	}
	return s.locks.ProjectLocks(ctx, projectID)
}

// ProjectLocks returns the setting of Story and Music.
func (s *DirectorService) ProjectLocks(ctx context.Context, projectID string) (domain.ParameterLocks, error) {
	return s.locks.ProjectLocks(ctx, projectID)
}
