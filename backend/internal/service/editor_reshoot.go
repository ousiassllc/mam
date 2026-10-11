package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// RequestAdditionalShot asks the Director for the additional Shots the Editor Judge of the Edit
// Timeline version found missing (POST /edit-timelines/:id/request-additional-shot, F-19): it adds
// each as a Shot of the Project and starts generating them. The Shots whose generation was queued
// are the next round of the Editor → Reshoot Loop and the Project goes to GENERATING; a Shot that
// fails to start is removed (and reported), so a started Shot is never left outside a round. When
// no Shot could start, the Project stays in EDIT_JUDGING and the same version can be asked again.
// Once every Shot of the round has its Selected Take they are put into the Edit Timeline and the
// Project is back in EDITING (ResumeReshoots). The rounds are limited by the Retry Policy, shared
// with the Repair / Retake Loop.
func (s *EditorService) RequestAdditionalShot(ctx context.Context, id string) (EditorView, error) {
	ver, err := s.editors.GetEditTimeline(ctx, id)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	release, err := s.acquire(ver.ProjectID)
	if err != nil {
		return EditorView{}, err
	}
	defer release()
	res, err := s.editors.LatestEditorJudge(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return EditorView{}, conflict("この版はまだ Editor Judge にかけていません")
	}
	if err != nil {
		return EditorView{}, err
	}
	return s.requestShots(ctx, ver, res)
}

// requestShots is RequestAdditionalShot with the stage held and the Judge result read.
func (s *EditorService) requestShots(ctx context.Context, ver domain.EditTimelineVersion, res domain.EditorJudgeResult) (EditorView, error) {
	projectID, branchID := ver.ProjectID, ver.BranchID
	if err := s.readyToRequestShots(ctx, ver, res); err != nil {
		return EditorView{}, err
	}
	policy, err := s.policies.Get(ctx, projectID)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	rounds, err := s.editors.ReshootRounds(ctx, projectID, branchID)
	if err != nil {
		return EditorView{}, err
	}
	if verdict := policy.Effective().CanRetry(rounds, 0, 0, 0); verdict != domain.RetryAllowed {
		return s.escalateReshoot(ctx, ver, res, rounds, verdict)
	}
	reqs, newScenes, err := s.addReshootShots(ctx, ver, res.Findings)
	if err != nil {
		return EditorView{}, err
	}
	// Queue every Shot before the round exists. The Shots that were queued form the round, so a
	// generation that has started always belongs to it; the ones that failed to start (budget,
	// routing) are removed again. With nothing queued there is no round and the next request is open.
	var (
		queued   []repository.NewReshoot
		failed   []repository.NewReshoot
		startErr error
	)
	for _, r := range reqs {
		if _, err := s.generation.gen.StartByPolicy(ctx, r.ShotID, "", nil); err != nil {
			failed = append(failed, r)
			if startErr == nil {
				startErr = err
			}
			continue
		}
		queued = append(queued, r)
	}
	// From here on the bookkeeping must not depend on the request: a Shot whose generation started
	// has to be put into the round, and one that did not has to go, even if the client has left
	// (or AUTO is shutting down) while the loop was running.
	ctx = context.WithoutCancel(ctx)
	if len(failed) > 0 {
		ids := make([]string, len(failed))
		for i, r := range failed {
			ids[i] = r.ShotID
		}
		if err := s.plans.RemoveShots(ctx, ids, newScenes); err != nil {
			s.logger.Error("remove additional shots that failed to start", "err", err)
		}
	}
	if len(queued) == 0 {
		return EditorView{}, additionalShotStartError(failed, startErr)
	}
	if _, err := s.editors.CreateReshootRound(ctx, ver.ID, queued); err != nil {
		return EditorView{}, notFound(err)
	}
	if err := s.projects.Transition(ctx, projectID, domain.StateGenerating); err != nil {
		return EditorView{}, err
	}
	lines := make([]string, len(queued))
	for i, r := range queued {
		lines[i] = fmt.Sprintf("・%s（%.1f 秒）: %s", clipKindLabel(r.Kind), r.DurationSeconds, r.Objective)
	}
	msg := fmt.Sprintf("Director に追加 Shot を %d 件要求しました（Editor → Reshoot Loop %d 回目）。生成できたら編集に戻ります。\n%s", len(queued), rounds+1, strings.Join(lines, "\n"))
	if len(failed) > 0 {
		msg += fmt.Sprintf("\n生成を始められなかった %d 件は見送りました（%s）。編集に戻ったら Editor Judge をやり直してください。", len(failed), additionalShotNames(failed))
	}
	s.announce(ctx, projectID, msg)
	return s.View(ctx, projectID, branchID)
}

// additionalShotNames lists the Shots by their objective.
func additionalShotNames(shots []repository.NewReshoot) string {
	names := make([]string, len(shots))
	for i, r := range shots {
		name := strings.TrimSpace(r.Objective)
		if name == "" {
			name = r.ShotID
		}
		names[i] = name
	}
	return strings.Join(names, "、")
}

// additionalShotStartError reports that none of the additional Shots could start generating.
func additionalShotStartError(failed []repository.NewReshoot, err error) error {
	return fmt.Errorf("追加 Shot の生成を始められませんでした（%s）: %w", additionalShotNames(failed), mapGenerationError(err))
}

// readyToRequestShots checks that the Judge found Shots missing in the latest version, that the
// Project waits for that (EDIT_JUDGING) and that the last round is done.
func (s *EditorService) readyToRequestShots(ctx context.Context, ver domain.EditTimelineVersion, res domain.EditorJudgeResult) error {
	projectID, branchID := ver.ProjectID, ver.BranchID
	if res.Verdict != domain.EditorNeedShot {
		return conflict("この版の Editor Judge は素材不足を判定していません")
	}
	if s.generation == nil || s.generation.gen == nil {
		return ErrUnavailable
	}
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return err
	}
	if latest, err := s.editors.LatestEditTimeline(ctx, projectID, branchID); err != nil {
		return notFound(err)
	} else if latest.ID != ver.ID {
		return conflict("この編集より新しい版があります。画面を更新してから Editor Judge をやり直してください")
	}
	if project.State != domain.StateEditJudging {
		return conflict("追加 Shot を要求できるのは Editor Judge が素材不足と判定した直後（EDIT_JUDGING）です（現在: %s）", project.State)
	}
	pending, err := s.editors.PendingReshoots(ctx, projectID)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(pending, func(r domain.ReshootRequest) bool { return r.BranchID == branchID }) {
		return conflict("前回の追加 Shot がまだできていません")
	}
	return nil
}

// addReshootShots adds, as Shots of the Project, the Shots the findings ask for, each anchored to
// the clip it follows. It also returns the Scenes it created for them.
func (s *EditorService) addReshootShots(ctx context.Context, ver domain.EditTimelineVersion, findings []domain.EditorFinding) ([]repository.NewReshoot, []string, error) {
	projectID, branchID := ver.ProjectID, ver.BranchID
	idx, err := s.shotIndex(ctx, projectID, branchID)
	if err != nil {
		return nil, nil, err
	}
	clips := ver.Timeline.Clips
	var (
		reqs       []repository.NewReshoot
		extraScene string
		newScenes  []string
	)
	for _, f := range findings {
		need := f.Need
		if need == nil || !need.Valid(len(clips)) {
			continue
		}
		anchor := clips[need.AfterClip-1]
		ref, ok := idx.byID[anchor.ShotID]
		if !ok {
			continue
		}
		shot := domain.Shot{SceneID: ref.Shot.SceneID, Objective: strings.TrimSpace(need.Objective), Duration: need.Seconds()}
		var newScene *domain.Scene
		if ref.Scene.BranchID != branchID {
			// The Shot follows one of the series the Branch hangs from: it goes into a Scene of the
			// Branch, so the additional Shot never appears in the series it is not part of.
			if extraScene == "" {
				newScene = &domain.Scene{BranchID: branchID, Title: "追加カット"}
			} else {
				shot.SceneID = extraScene
			}
		}
		if shot, err = s.plans.AppendContinuation(ctx, projectID, newScene, shot); err != nil {
			return nil, nil, notFound(err)
		}
		if newScene != nil {
			extraScene = shot.SceneID
			newScenes = append(newScenes, extraScene)
		}
		reqs = append(reqs, repository.NewReshoot{ShotID: shot.ID, AfterShotID: anchor.ShotID, Kind: need.Kind, Objective: shot.Objective, DurationSeconds: shot.Duration})
	}
	if len(reqs) == 0 {
		return nil, nil, conflict("追加できる Shot がありません（判定に追加 Shot の指定がありません）")
	}
	return reqs, newScenes, nil
}

func clipKindLabel(k domain.ClipKind) string {
	if k == domain.ClipReaction {
		return "リアクションショット"
	}
	return "インサートショット"
}

// escalateReshoot stops the Editor → Reshoot Loop when the Retry Policy allows no further round and
// asks the user for a decision (an Escalation card, posted at every Confirmation Level).
func (s *EditorService) escalateReshoot(ctx context.Context, ver domain.EditTimelineVersion, res domain.EditorJudgeResult, rounds int, verdict domain.RetryVerdict) (EditorView, error) {
	var missing []domain.EditorFinding
	for _, f := range res.Findings {
		if f.Need != nil {
			missing = append(missing, f)
		}
	}
	text := fmt.Sprintf("追加 Shot の要求を止めました: %s（これまでに %d 回要求しました）。\n足りない素材:\n%s", verdict.Reason(), rounds, describeFindings(missing))
	options := []string{
		"設定で上限を引き上げてから、もう一度 Editor Judge を実行する",
		"追加 Shot なしで、今ある素材のまま編集を続ける",
		"Shot を手動で追加して生成する",
	}
	if err := s.chatSvc.RaiseEscalation(context.WithoutCancel(ctx), ver.ProjectID, text, options, ""); err != nil {
		return EditorView{}, err
	}
	return s.View(ctx, ver.ProjectID, ver.BranchID)
}

// resumeReshoots is the return of the Editor → Reshoot Loop: it puts the additional Shots that
// have their Selected Take into the Edit Timeline of the trunk / Branch that asked for them and
// takes the Project back to EDITING. It runs on the ResumeEditing worker, and does nothing for a
// round whose Shots are not all ready.
func (s *EditorService) resumeReshoots(ctx context.Context) {
	ids, err := s.editors.ProjectsWithPendingReshoots(ctx)
	if err != nil {
		s.logger.Error("list projects with pending additional shots failed", "err", err)
		return
	}
	for _, id := range ids {
		if err := s.applyReshoots(ctx, id); err != nil {
			s.logger.Error("put additional shots into the edit timeline failed", "project_id", id, "err", err)
		}
	}
}

// reshootRound is the additional Shots one Reshoot round of a trunk / Branch asked for.
type reshootRound struct {
	branch string
	reqs   []domain.ReshootRequest
}

// pendingRounds groups the pending requests of the Project by round, in the order they were asked.
func (s *EditorService) pendingRounds(ctx context.Context, projectID string) ([]reshootRound, error) {
	pending, err := s.editors.PendingReshoots(ctx, projectID)
	if err != nil {
		return nil, err
	}
	type key struct {
		branch string
		number int
	}
	index := map[key]int{}
	var rounds []reshootRound
	for _, r := range pending {
		k := key{r.BranchID, r.Round}
		i, seen := index[k]
		if !seen {
			i = len(rounds)
			index[k] = i
			rounds = append(rounds, reshootRound{branch: r.BranchID})
		}
		rounds[i].reqs = append(rounds[i].reqs, r)
	}
	return rounds, nil
}

// anyRoundReady tells whether a round has all its Shots selected. It does not hold the editing
// stage: a round that still waits must not make a stage of the user (Final QC, a preview) busy.
func (s *EditorService) anyRoundReady(ctx context.Context, projectID string) (bool, error) {
	rounds, err := s.pendingRounds(ctx, projectID)
	if err != nil {
		return false, err
	}
	for _, k := range rounds {
		sources, err := s.roundSources(ctx, projectID, k.branch, k.reqs)
		if err != nil {
			return false, err
		}
		if sources != nil {
			return true, nil
		}
	}
	return false, nil
}

func (s *EditorService) applyReshoots(ctx context.Context, projectID string) error {
	if ready, err := s.anyRoundReady(ctx, projectID); err != nil || !ready {
		return err // every round still waits: the selection of its last Take wakes the worker again
	}
	release, err := s.acquire(projectID)
	if err != nil {
		// Another stage runs. Nothing else would bring us back (the last job may already be done), so
		// the stage wakes the worker when it is released. The second try closes the gap in which it
		// was released before the mark was set.
		s.missed.Store(projectID, struct{}{})
		if release, err = s.acquire(projectID); err != nil {
			return nil
		}
		s.missed.Delete(projectID)
	}
	trunk, err := s.applyPendingRounds(ctx, projectID, release)
	if err != nil || trunk == nil {
		return err
	}
	return s.autoJudgeReshot(ctx, projectID, *trunk)
}

// applyPendingRounds applies every ready round with the stage held and releases it. It returns
// the version the trunk's round made, nil when there is none.
func (s *EditorService) applyPendingRounds(ctx context.Context, projectID string, release func()) (*domain.EditTimelineVersion, error) {
	defer release()
	rounds, err := s.pendingRounds(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var trunk *domain.EditTimelineVersion
	for _, k := range rounds {
		ver, err := s.applyRound(ctx, projectID, k.branch, k.reqs)
		if err != nil {
			return nil, err
		}
		if ver != nil && k.branch == "" {
			trunk = ver
		}
	}
	return trunk, nil
}

// autoJudgeReshot has the Editor Judge look at the trunk version that holds the additional Shots
// when the Project is AUTO, as it did for the Rough Cut: PASS goes on to AUDIO, NEED_SHOT asks the
// next round within the Retry Policy (and escalates at its limit), Re-edit stops for the user. A
// DIRECTOR Project waits for the user to run the Judge.
func (s *EditorService) autoJudgeReshot(ctx context.Context, projectID string, ver domain.EditTimelineVersion) error {
	project, err := s.getProject(ctx, projectID)
	if err != nil || project.Mode != domain.ModeAuto || project.State != domain.StateEditing {
		return err
	}
	if _, err := s.JudgeTimeline(ctx, ver.ID); err != nil {
		if errors.Is(err, ErrStageBusy) || errors.Is(err, ErrNotFound) || ctx.Err() != nil {
			return nil
		}
		s.announce(ctx, projectID, fmt.Sprintf("追加 Shot を入れた編集の Editor Judge を自動で実行できませんでした: %s\n編集画面から Editor Judge を実行してください。", err))
		return err
	}
	return nil
}

// applyRound puts the Shots of one round into the latest Edit Timeline of the series, each after
// the Shot it follows, as one new version, and takes the Project back to EDITING. A round whose
// Shots are not all ready waits, and so does a round of a Project that cannot be edited (READY,
// EXPORTED, AWAITING_APPROVAL); the version is nil when nothing was applied.
func (s *EditorService) applyRound(ctx context.Context, projectID, branchID string, reqs []domain.ReshootRequest) (*domain.EditTimelineVersion, error) {
	sources, err := s.roundSources(ctx, projectID, branchID, reqs)
	if err != nil || sources == nil {
		return nil, err
	}
	latest, err := s.editors.LatestEditTimeline(ctx, projectID, branchID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tl, err := domain.EditTimeline{Clips: insertRound(latest.Timeline.Clips, reqs, sources)}.Normalize()
	if err != nil {
		return nil, fmt.Errorf("additional shots do not fit the timeline: %w", err)
	}
	ids := make([]string, len(reqs))
	for i, r := range reqs {
		ids[i] = r.ID
	}
	// The state goes first, as in Rough Cut and SaveTimeline: a Project that cannot be edited (READY /
	// EXPORTED were judged, mixed and checked for the version they have; AWAITING_APPROVAL waits for the
	// user) keeps its Edit Timeline, and the round waits for the Project to be edited again.
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.toEditing(ctx, project); err != nil {
		if errors.Is(err, ErrConflict) {
			return nil, nil
		}
		return nil, err
	}
	ver, err := s.editors.SaveReshootApplied(ctx, projectID, branchID, latest.ID, tl, ids)
	if err != nil {
		if errors.Is(err, repository.ErrStale) || errors.Is(err, repository.ErrConflict) {
			return nil, nil // edited or applied meanwhile: the next trigger looks again
		}
		return nil, err
	}
	msg := fmt.Sprintf("追加 Shot %d 件を編集タイムラインに入れました。編集に戻ります。", len(reqs))
	if project.Mode == domain.ModeAuto && branchID == "" {
		msg += "Editor Judge をもう一度実行します。"
	} else {
		msg += "タイムラインを確認して、もう一度 Editor Judge を実行してください。"
	}
	s.announce(ctx, projectID, msg)
	return &ver, nil
}

// roundSources reads the Selected Take of every Shot of the round. It is nil (and the error nil)
// while a Shot of the round is still being made.
func (s *EditorService) roundSources(ctx context.Context, projectID, branchID string, reqs []domain.ReshootRequest) ([]cutSource, error) {
	idx, err := s.shotIndex(ctx, projectID, branchID)
	if err != nil {
		return nil, err
	}
	shots := make([]repository.ShotRef, 0, len(reqs))
	for _, r := range reqs {
		ref, known := idx.byID[r.ShotID]
		if !known {
			return nil, nil
		}
		shots = append(shots, ref)
	}
	found, err := s.loadSources(ctx, selectedTakeIDs(shots))
	if err != nil {
		return nil, err
	}
	sources := make([]cutSource, len(reqs))
	for i, ref := range shots {
		src, ok := found.selected(ctx, ref.Shot)
		if !ok {
			return nil, nil // still being made
		}
		sources[i] = src
	}
	return sources, nil
}

// insertRound puts the clips of the round into a copy of clips, each after the Shot it follows. A
// Shot a Rough Cut made since already has is skipped.
func insertRound(clips []domain.EditClip, reqs []domain.ReshootRequest, sources []cutSource) []domain.EditClip {
	clips = slices.Clone(clips)
	placed := map[string]bool{}
	for i, r := range reqs {
		if slices.ContainsFunc(clips, func(c domain.EditClip) bool { return c.ShotID == r.ShotID }) {
			continue
		}
		out := r.DurationSeconds
		if sources[i].seconds > 0 {
			out = min(out, sources[i].seconds)
		}
		clip := domain.EditClip{
			ShotID: r.ShotID, TakeID: sources[i].takeID, Kind: r.Kind, SourceSeconds: sources[i].seconds, Out: out,
			Transition: domain.Transition{Type: domain.TransitionCut},
		}
		at := insertionPoint(len(clips), func(j int) string { return clips[j].ShotID }, r.AfterShotID, placed)
		clips = slices.Insert(clips, at, clip)
		placed[r.ShotID] = true
	}
	return clips
}
