package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/editor"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Timeline returns the latest Edit Timeline of the Project for the editor (ErrNotFound before the
// first Rough Cut).
func (s *EditorService) Timeline(ctx context.Context, projectID, branchID string) (TimelineView, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return TimelineView{}, err
	}
	ver, err := s.editors.LatestEditTimeline(ctx, projectID, branchID)
	if err != nil {
		return TimelineView{}, notFound(err)
	}
	return s.timelineView(ctx, ver)
}

func (s *EditorService) timelineView(ctx context.Context, ver domain.EditTimelineVersion) (TimelineView, error) {
	idx, err := s.shotIndex(ctx, ver.ProjectID, ver.BranchID)
	if err != nil {
		return TimelineView{}, err
	}
	project, err := s.getProject(ctx, ver.ProjectID)
	if err != nil {
		return TimelineView{}, err
	}
	v := TimelineView{EditTimelineVersion: ver, Editable: editable(project.State), Clips: make([]ClipView, len(ver.Timeline.Clips)), Duration: ver.Timeline.Duration()}
	starts := ver.Timeline.Starts()
	for i, c := range ver.Timeline.Clips {
		cv := ClipView{EditClip: c, Start: starts[i], Label: idx.labels[c.ShotID], VideoURL: domain.TakeVideoURL(c.TakeID), ThumbnailURL: domain.TakeThumbnailURL(c.TakeID)}
		if cv.Label == "" {
			cv.Label = fmt.Sprintf("クリップ %d", i+1)
		}
		if r, ok := idx.byID[c.ShotID]; ok {
			cv.Objective = r.Shot.Objective
		}
		v.Clips[i] = cv
	}
	if j, err := s.editors.LatestEditorJudge(ctx, ver.ID); err == nil {
		v.Judge = &j
	} else if !errors.Is(err, repository.ErrNotFound) {
		return TimelineView{}, err
	}
	return v, nil
}

// RoughCut makes the Rough Cut from the Selected Takes of every Shot of the trunk of the Project,
// or of its Branch (POST /projects/:id/edit/rough-cut), and starts EDITING. Every Shot needs a
// Selected Take with a file. The Editor Agent then edits what the Production Template ranks (Pace,
// Hook, CTA Timing, Caption: editor.Advance) and additional Shots the Reshoot Loop already put in
// stay in the cut, after the Shot they follow.
func (s *EditorService) RoughCut(ctx context.Context, projectID, branchID string) (EditorView, error) {
	release, err := s.acquire(projectID)
	if err != nil {
		return EditorView{}, err
	}
	defer release()

	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	idx, err := s.shotIndex(ctx, projectID, branchID)
	if err != nil {
		return EditorView{}, err
	}
	reshoots, err := s.editors.ListReshoots(ctx, projectID, branchID)
	if err != nil {
		return EditorView{}, err
	}
	sources, err := s.loadSources(ctx, selectedTakeIDs(idx.order))
	if err != nil {
		return EditorView{}, err
	}
	cuts, err := storyCuts(ctx, idx, reshoots, sources)
	if err != nil {
		return EditorView{}, err
	}
	cuts = placeReshoots(ctx, cuts, idx, reshoots, sources)
	tl, notes, err := s.editRoughCut(ctx, project, idx, branchID, cuts)
	if err != nil {
		return EditorView{}, err
	}
	// The state change and the stored version go together, even when the caller stops meanwhile.
	persist := context.WithoutCancel(ctx)
	if err := s.toEditing(persist, project); err != nil {
		return EditorView{}, err
	}
	if _, err := s.editors.SaveEditTimeline(persist, projectID, branchID, "", tl); err != nil {
		return EditorView{}, notFound(err)
	}
	text := fmt.Sprintf("Rough Cut を作成しました（%d クリップ / %.1f 秒）。タイムラインで並べ替え・Cut Point・Transition・字幕を編集できます。", len(tl.Clips), tl.Duration())
	if len(notes) > 0 {
		text += fmt.Sprintf("\nProduction Template（%s）に合わせて編集しました:\n・%s", project.ProductionTemplate.Label(), strings.Join(notes, "\n・"))
	}
	s.announce(ctx, projectID, text)
	return s.View(ctx, projectID, branchID)
}

// storyCuts are the cuts of the story Shots (the ones no Reshoot round added), each with its
// Selected Take. A Shot without a Selected Take that has a file refuses the Rough Cut.
func storyCuts(ctx context.Context, idx shotIndex, reshoots []domain.ReshootRequest, sources *takeSources) ([]editor.ShotCut, error) {
	additional := make(map[string]bool, len(reshoots))
	for _, r := range reshoots {
		additional[r.ShotID] = true
	}
	var story []repository.ShotRef
	for _, r := range idx.order {
		if !additional[r.Shot.ID] {
			story = append(story, r)
		}
	}
	if len(story) == 0 {
		return nil, invalid("", "Shot がまだありません。Scene / Shot を作成してから Rough Cut を作ってください")
	}
	cuts := make([]editor.ShotCut, 0, len(story))
	var missing []string
	for _, r := range story {
		src, ok := sources.selected(ctx, r.Shot)
		if !ok {
			missing = append(missing, idx.labels[r.Shot.ID])
			continue
		}
		cuts = append(cuts, editor.ShotCut{ShotID: r.Shot.ID, TakeID: src.takeID, SceneIndex: idx.scene[r.Shot.ID], Planned: r.Shot.Duration, TakeSeconds: src.seconds})
	}
	if len(missing) > 0 {
		return nil, invalid("", "Selected Take（ファイルのある採用 Take）がない Shot があります: %s", strings.Join(missing, "、"))
	}
	return cuts, nil
}

// placeReshoots puts the additional Shots the Reshoot Loop already applied into the cut, each after
// the Shot it follows. One without a usable Take now is left out.
func placeReshoots(ctx context.Context, cuts []editor.ShotCut, idx shotIndex, reshoots []domain.ReshootRequest, sources *takeSources) []editor.ShotCut {
	placed := map[string]bool{}
	for _, r := range reshoots {
		ref, known := idx.byID[r.ShotID]
		if !known || r.Status != domain.ReshootApplied {
			continue
		}
		src, ok := sources.selected(ctx, ref.Shot)
		if !ok {
			continue
		}
		at := insertionPoint(len(cuts), func(i int) string { return cuts[i].ShotID }, r.AfterShotID, placed)
		c := editor.ShotCut{ShotID: r.ShotID, TakeID: src.takeID, Planned: r.DurationSeconds, TakeSeconds: src.seconds, Kind: r.Kind}
		if at > 0 {
			c.SceneIndex = cuts[at-1].SceneIndex
		}
		cuts = slices.Insert(cuts, at, c)
		placed[r.ShotID] = true
	}
	return cuts
}

// editRoughCut turns the cuts into the Edit Timeline: it keeps the clips of the Shots the user cut
// by hand and lets the Editor Agent edit what the Production Template ranks. The notes say what it did.
func (s *EditorService) editRoughCut(ctx context.Context, project domain.Project, idx shotIndex, branchID string, cuts []editor.ShotCut) (domain.EditTimeline, []string, error) {
	tl, err := editor.RoughCut(cuts).Normalize()
	if err != nil {
		return domain.EditTimeline{}, nil, invalid("", "%s", timelineMessage(err))
	}
	if tl, err = s.keepFixedClips(ctx, idx, project.ID, branchID, tl); err != nil {
		return domain.EditTimeline{}, nil, err
	}
	brief, _, err := s.briefOptions(ctx, project.ID)
	if err != nil {
		return domain.EditTimeline{}, nil, err
	}
	facts, err := s.shotFacts(ctx, idx, tl)
	if err != nil {
		return domain.EditTimeline{}, nil, err
	}
	tl, notes := editor.Advance(editor.JudgeInput{Brief: brief, Template: project.ProductionTemplate, Timeline: tl, Shots: facts})
	if tl, err = tl.Normalize(); err != nil {
		return domain.EditTimeline{}, nil, invalid("", "%s", timelineMessage(err))
	}
	return tl, notes, nil
}

// keepFixedClips puts back, into a Rough Cut that is made again, the clips of the Shots whose
// Editing is MANUAL or LOCK (spec/spec.md §6): the AI does not re-cut what the user cut. A clip
// whose Take file is gone is not brought back, since it could not be played.
func (s *EditorService) keepFixedClips(ctx context.Context, idx shotIndex, projectID, branchID string, fresh domain.EditTimeline) (domain.EditTimeline, error) {
	fixed, err := s.editingFixed(ctx, idx, fresh)
	if err != nil || len(fixed) == 0 {
		return fresh, err
	}
	prev, err := s.editors.LatestEditTimeline(ctx, projectID, branchID)
	if errors.Is(err, repository.ErrNotFound) {
		return fresh, nil
	}
	if err != nil {
		return domain.EditTimeline{}, err
	}
	sources, err := s.loadSources(ctx, clipTakeIDs(prev.Timeline.Clips))
	if err != nil {
		return domain.EditTimeline{}, err
	}
	usable := make(map[string]bool, len(fixed))
	for _, c := range prev.Timeline.Clips {
		if !fixed[c.ShotID] {
			continue
		}
		if _, ok := sources.take(ctx, c.TakeID); ok {
			usable[c.ShotID] = true
		}
	}
	kept, err := editor.KeepFixedClips(fresh, prev.Timeline, usable).Normalize()
	if err != nil {
		return domain.EditTimeline{}, invalid("", "%s", timelineMessage(err))
	}
	return kept, nil
}

// editingFixed lists the Shots of the timeline whose Editing parameter is MANUAL or LOCK.
func (s *EditorService) editingFixed(ctx context.Context, idx shotIndex, tl domain.EditTimeline) (map[string]bool, error) {
	fixed := map[string]bool{}
	for _, c := range tl.Clips {
		if _, ok := idx.byID[c.ShotID]; !ok || fixed[c.ShotID] {
			continue
		}
		locks, err := s.locks.ShotLocks(ctx, c.ShotID)
		if err != nil {
			return nil, err
		}
		if locks.Mode(domain.ParamEditing) != domain.LockAuto {
			fixed[c.ShotID] = true
		}
	}
	return fixed, nil
}

// insertionPoint is where an additional Shot that follows the Shot anchor goes into a list of n
// clips: right after the anchor and the additional Shots already placed there (the end when the
// anchor is not in the list).
func insertionPoint(n int, shotAt func(i int) string, anchor string, placed map[string]bool) int {
	at := n
	for i := range n {
		if shotAt(i) == anchor {
			at = i + 1
		}
	}
	for at < n && placed[shotAt(at)] {
		at++
	}
	return at
}

// timelineMessage is the message of an invalid timeline without its sentinel prefix.
func timelineMessage(err error) string {
	return strings.TrimPrefix(err.Error(), domain.ErrInvalidTimeline.Error()+": ")
}

// SaveTimeline saves an edited timeline as the next version (PUT /edit-timelines/:id). id is the
// version the editor was showing: when a newer one exists the save is refused (ErrConflict), so
// that an old editor cannot overwrite a newer edit. Saving takes the Project back to EDITING.
func (s *EditorService) SaveTimeline(ctx context.Context, id string, tl domain.EditTimeline) (TimelineView, error) {
	base, err := s.editors.GetEditTimeline(ctx, id)
	if err != nil {
		return TimelineView{}, notFound(err)
	}
	release, err := s.acquire(base.ProjectID)
	if err != nil {
		return TimelineView{}, err
	}
	defer release()

	project, err := s.getProject(ctx, base.ProjectID)
	if err != nil {
		return TimelineView{}, err
	}
	idx, err := s.shotIndex(ctx, project.ID, base.BranchID)
	if err != nil {
		return TimelineView{}, err
	}
	sources, err := s.loadSources(ctx, clipTakeIDs(tl.Clips))
	if err != nil {
		return TimelineView{}, err
	}
	for i := range tl.Clips {
		c := &tl.Clips[i]
		r, ok := idx.byID[c.ShotID]
		if !ok {
			return TimelineView{}, invalid("clips", "クリップ %d の Shot がこの Project にありません", i+1)
		}
		src, ok := sources.take(ctx, c.TakeID)
		if !ok {
			return TimelineView{}, invalid("clips", "クリップ %d（%s）の Take のファイルが見つかりません", i+1, idx.labels[r.Shot.ID])
		}
		if sources.shotOf(c.TakeID) != r.Shot.ID {
			return TimelineView{}, invalid("clips", "クリップ %d の Take は %s の Take ではありません", i+1, idx.labels[r.Shot.ID])
		}
		c.SourceSeconds = src.seconds // the file decides; the client's value is not trusted
	}
	norm, err := tl.Normalize()
	if err != nil {
		return TimelineView{}, invalid("clips", "%s", timelineMessage(err))
	}
	if latest, err := s.editors.LatestEditTimeline(ctx, project.ID, base.BranchID); err != nil {
		return TimelineView{}, notFound(err)
	} else if latest.ID != id {
		return TimelineView{}, conflict("別の編集が保存されています。画面を更新してから編集し直してください")
	}
	if err := s.toEditing(ctx, project); err != nil {
		return TimelineView{}, err
	}
	ver, err := s.editors.SaveEditTimeline(ctx, project.ID, base.BranchID, id, norm)
	switch {
	case errors.Is(err, repository.ErrStale):
		return TimelineView{}, conflict("別の編集が保存されています。画面を更新してから編集し直してください")
	case err != nil:
		return TimelineView{}, notFound(err)
	}
	s.recordIntervention(ctx, domain.Intervention{ProjectID: project.ID, Kind: domain.InterventionEditTimeline, Override: true})
	return s.timelineView(ctx, ver)
}

// shotFacts is what the Editor Judge needs to know about the Shots of the clips.
func (s *EditorService) shotFacts(ctx context.Context, idx shotIndex, tl domain.EditTimeline) (map[string]editor.ShotFacts, error) {
	facts := make(map[string]editor.ShotFacts, len(tl.Clips))
	fixed, err := s.editingFixed(ctx, idx, tl)
	if err != nil {
		return nil, err
	}
	sources, err := s.loadSources(ctx, clipTakeIDs(tl.Clips)) // a failed read is not a missing file: the Editor Judge must not report one
	if err != nil {
		return nil, err
	}
	for _, c := range tl.Clips {
		r, ok := idx.byID[c.ShotID]
		if !ok {
			continue
		}
		f := editor.ShotFacts{Label: idx.labels[c.ShotID], Objective: r.Shot.Objective, SelectedTakeID: r.Shot.SelectedTakeID, EditingFixed: fixed[c.ShotID]}
		if sb, ok := r.Shot.Storyboard(); ok {
			f.Dialogue = sb.Dialogue
		}
		if spec, ok := r.Shot.CanonicalSpec(); ok {
			f.HasProduct = spec.Product != nil
		}
		_, f.TakeReady = sources.take(ctx, c.TakeID)
		facts[c.ShotID] = f
	}
	return facts, nil
}
