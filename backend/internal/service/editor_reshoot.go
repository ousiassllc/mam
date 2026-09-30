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
// each as a Shot of the Project and starts generating them. The request is the next round of the
// Editor → Reshoot Loop only when every Shot was queued; the Project then goes to GENERATING.
// A Shot that fails to start is removed and the Project stays in EDIT_JUDGING, so the same version
// can be asked again. Once every Shot of the round has its Selected Take they are put into the
// Edit Timeline and the Project is back in EDITING (ResumeReshoots). The rounds are limited by
// the Retry Policy, shared with the Repair / Retake Loop.
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
	reqs, err := s.addReshootShots(ctx, ver, res.Findings)
	if err != nil {
		return EditorView{}, err
	}
	// Queue every Shot before the round exists. The round stays pending only when every enqueue
	// succeeds, so a budget or routing failure does not block the next request.
	var (
		failed   []repository.NewReshoot
		startErr error
		started  int
	)
	for _, r := range reqs {
		if _, err := s.generation.gen.Start(ctx, r.ShotID, ""); err != nil {
			failed = append(failed, r)
			if startErr == nil {
				startErr = err
			}
			continue
		}
		started++
	}
	if startErr != nil {
		ids := make([]string, len(failed))
		for i, r := range failed {
			ids[i] = r.ShotID
		}
		if err := s.plans.RemoveShots(ctx, ids); err != nil {
			s.logger.Error("remove additional shots that failed to start", "err", err)
		}
		return EditorView{}, additionalShotStartError(failed, started, startErr)
	}
	if _, err := s.editors.CreateReshootRound(ctx, ver.ID, reqs); err != nil {
		return EditorView{}, notFound(err)
	}
	if err := s.projects.Transition(ctx, projectID, domain.StateGenerating); err != nil {
		return EditorView{}, err
	}
	lines := make([]string, len(reqs))
	for i, r := range reqs {
		lines[i] = fmt.Sprintf("・%s（%.1f 秒）: %s", clipKindLabel(r.Kind), r.DurationSeconds, r.Objective)
	}
	s.announce(ctx, projectID, fmt.Sprintf("Director に追加 Shot を %d 件要求しました（Editor → Reshoot Loop %d 回目）。生成できたら編集に戻ります。\n%s", len(reqs), rounds+1, strings.Join(lines, "\n")))
	return s.View(ctx, projectID, branchID)
}

// additionalShotStartError reports the Shots whose generation did not start. Shots that did start
// keep running; they are not part of a round unless every Shot of the call was queued.
func additionalShotStartError(failed []repository.NewReshoot, started int, err error) error {
	names := make([]string, len(failed))
	for i, r := range failed {
		name := strings.TrimSpace(r.Objective)
		if name == "" {
			name = r.ShotID
		}
		names[i] = name
	}
	msg := fmt.Sprintf("追加 Shot の生成を始められませんでした（%s）", strings.Join(names, "、"))
	if started > 0 {
		msg += fmt.Sprintf("。始められた %d 件の生成はそのまま続けます", started)
	}
	return fmt.Errorf("%s: %w", msg, mapGenerationError(err))
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
// the clip it follows.
func (s *EditorService) addReshootShots(ctx context.Context, ver domain.EditTimelineVersion, findings []domain.EditorFinding) ([]repository.NewReshoot, error) {
	projectID, branchID := ver.ProjectID, ver.BranchID
	idx, err := s.shotIndex(ctx, projectID, branchID)
	if err != nil {
		return nil, err
	}
	clips := ver.Timeline.Clips
	var (
		reqs       []repository.NewReshoot
		extraScene string
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
			return nil, notFound(err)
		}
		if newScene != nil {
			extraScene = shot.SceneID
		}
		reqs = append(reqs, repository.NewReshoot{ShotID: shot.ID, AfterShotID: anchor.ShotID, Kind: need.Kind, Objective: shot.Objective, DurationSeconds: shot.Duration})
	}
	if len(reqs) == 0 {
		return nil, conflict("追加できる Shot がありません（判定に追加 Shot の指定がありません）")
	}
	return reqs, nil
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

func (s *EditorService) applyReshoots(ctx context.Context, projectID string) error {
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
	defer release()
	pending, err := s.editors.PendingReshoots(ctx, projectID)
	if err != nil {
		return err
	}
	type round struct {
		branch string
		number int
	}
	groups := map[round][]domain.ReshootRequest{}
	var order []round
	for _, r := range pending {
		k := round{r.BranchID, r.Round}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	for _, k := range order {
		if err := s.applyRound(ctx, projectID, k.branch, groups[k]); err != nil {
			return err
		}
	}
	return nil
}

// applyRound puts the Shots of one round into the latest Edit Timeline of the series, each after
// the Shot it follows, as one new version. A round whose Shots are not all ready waits.
func (s *EditorService) applyRound(ctx context.Context, projectID, branchID string, reqs []domain.ReshootRequest) error {
	sources, err := s.roundSources(ctx, projectID, branchID, reqs)
	if err != nil || sources == nil {
		return err
	}
	latest, err := s.editors.LatestEditTimeline(ctx, projectID, branchID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	tl, err := domain.EditTimeline{Clips: insertRound(latest.Timeline.Clips, reqs, sources)}.Normalize()
	if err != nil {
		return fmt.Errorf("additional shots do not fit the timeline: %w", err)
	}
	ids := make([]string, len(reqs))
	for i, r := range reqs {
		ids[i] = r.ID
	}
	if _, err := s.editors.SaveReshootApplied(ctx, projectID, branchID, latest.ID, tl, ids); err != nil {
		if errors.Is(err, repository.ErrStale) || errors.Is(err, repository.ErrConflict) {
			return nil // edited or applied meanwhile: the next trigger looks again
		}
		return err
	}
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return err
	}
	s.announce(ctx, projectID, fmt.Sprintf("追加 Shot %d 件を編集タイムラインに入れました。編集に戻ります。タイムラインを確認して、もう一度 Editor Judge を実行してください。", len(reqs)))
	return s.toEditing(ctx, project)
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
