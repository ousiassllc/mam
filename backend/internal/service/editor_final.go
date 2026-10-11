package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/finalqc"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// FinalQC runs Final QC on the latest Edit and Audio Timelines (POST /projects/:id/final-qc). The
// Project must be in AUDIO. PASS moves it to READY (through the Final checkpoint when the
// Confirmation Level asks for it); FAIL sends it back to the stage that has the problem.
// Final QC always inspects the trunk; viewBranchID only picks the Branch whose editor panel is
// answered ("" is the trunk), so that the panel the button was pressed in keeps its scope.
func (s *EditorService) FinalQC(ctx context.Context, projectID, viewBranchID string) (EditorView, error) {
	release, err := s.acquire(projectID)
	if err != nil {
		return EditorView{}, err
	}
	defer release()

	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	if project.State != domain.StateAudio && project.State != domain.StateFinalQC {
		return EditorView{}, conflict("Final QC を実行できるのは音声を作った後（AUDIO）です（現在: %s）", project.State)
	}
	in, err := s.finalQCInput(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	if err := s.projects.Transition(ctx, projectID, domain.StateFinalQC); err != nil {
		return EditorView{}, err
	}
	findings := finalqc.Check(in)
	verdict := domain.FinalQCPass
	if finalqc.Failed(findings) {
		verdict = domain.FinalQCFail
	}
	res, err := s.editors.SaveFinalQC(ctx, projectID, in.Audio.ID, verdict, findings)
	if err != nil {
		s.backTo(ctx, projectID, domain.StateAudio)
		return EditorView{}, err
	}
	if err := s.afterFinalQC(ctx, in, res, findings); err != nil {
		s.backTo(ctx, projectID, domain.StateAudio)
		return EditorView{}, err
	}
	return s.View(ctx, projectID, viewBranchID)
}

// finalQCInput gathers what Final QC checks: the latest Edit and Audio Timelines, whether the mix
// and the Take files are there, and the Brand constraints.
func (s *EditorService) finalQCInput(ctx context.Context, projectID string) (finalqc.Input, error) {
	edit, err := s.editors.LatestEditTimeline(ctx, projectID, "")
	if err != nil {
		return finalqc.Input{}, notFound(err)
	}
	audio, err := s.editors.LatestAudioTimeline(ctx, projectID, "")
	if errors.Is(err, repository.ErrNotFound) {
		return finalqc.Input{}, conflict("音声がまだありません。先に音声を作成してください")
	}
	if err != nil {
		return finalqc.Input{}, err
	}
	in := finalqc.Input{Edit: edit, Audio: audio, MixFileExists: s.fileExists(audio.Timeline.MixPath)}
	sources, err := s.loadSources(ctx, clipTakeIDs(edit.Timeline.Clips))
	if err != nil {
		return finalqc.Input{}, err
	}
	for i, c := range edit.Timeline.Clips {
		if _, ok := sources.take(ctx, c.TakeID); !ok {
			in.MissingClips = append(in.MissingClips, i+1)
		}
	}
	if err := s.brandInput(ctx, projectID, &in); err != nil {
		return finalqc.Input{}, err
	}
	return in, nil
}

// afterFinalQC carries out the result: FAIL sends the Project back to the stage that has the
// problem; PASS asks for the Final checkpoint or goes to READY at once.
func (s *EditorService) afterFinalQC(ctx context.Context, in finalqc.Input, res domain.FinalQCResult, findings []domain.QCFinding) error {
	projectID := in.Edit.ProjectID
	if res.Verdict == domain.FinalQCFail {
		stage := finalqc.ReturnStage(findings)
		if err := s.projects.Transition(ctx, projectID, stage); err != nil {
			return err
		}
		s.announce(ctx, projectID, fmt.Sprintf("Final QC: FAIL。%s に戻します。\n%s", stageLabel(stage), describeQC(findings)))
		return nil
	}
	summary := fmt.Sprintf("Final QC に合格しました（%.1f 秒 / %.1f LUFS）。書き出してよいですか？", in.Edit.Timeline.Duration(), in.Audio.Timeline.MeasuredLUFS)
	if len(findings) > 0 {
		summary += "\n" + describeQC(findings)
	}
	posted, err := s.chatSvc.RequestApproval(ctx, projectID, domain.CheckpointFinal, summary, res.ID)
	if err != nil || posted {
		return err
	}
	if err := s.projects.Transition(ctx, projectID, domain.StateReady); err != nil {
		return err
	}
	text := "Final QC: PASS。書き出しの準備ができました。"
	if len(findings) > 0 {
		text += "\n" + describeQC(findings)
	}
	s.announce(ctx, projectID, text)
	return nil
}

// brandInput gives Final QC the Brand / Safety Constraints, the lines the deliverable speaks, the
// texts of the Shot Specs of its clips and the Brand Issues the AI Video Judge found in their Takes.
// Issues of a constraint that was deleted since are not counted.
func (s *EditorService) brandInput(ctx context.Context, projectID string, in *finalqc.Input) error {
	brand, err := s.brands.List(ctx, projectID)
	if err != nil {
		return err
	}
	if len(brand) == 0 {
		return nil
	}
	in.Brand = brand
	idx, err := s.shotIndex(ctx, projectID, "")
	if err != nil {
		return err
	}
	scenes, err := s.audioScenes(ctx, projectID, "", in.Edit.Timeline)
	if err != nil {
		return err
	}
	for _, sc := range scenes {
		for _, line := range sc.Lines {
			if vl, ok := domain.ParseVoiceLine(line); ok {
				in.Spoken = append(in.Spoken, vl.Text)
			}
		}
	}
	for i, c := range in.Edit.Timeline.Clips {
		if r, ok := idx.byID[c.ShotID]; ok {
			if spec, ok := r.Shot.CanonicalSpec(); ok {
				in.ShotTexts = append(in.ShotTexts, spec.BrandText())
			}
		}
		res, err := s.judges.Latest(ctx, c.TakeID)
		if errors.Is(err, repository.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		for _, is := range res.Issues {
			if _, ok := brand.ByID(is.BrandConstraintID); ok {
				in.ClipBrandIssues = append(in.ClipBrandIssues, finalqc.ClipBrandIssue{Clip: i + 1, Issue: is})
			}
		}
	}
	return nil
}

func stageLabel(state domain.ProjectState) string {
	if state == domain.StateEditing {
		return "編集（EDITING）"
	}
	return "音声（AUDIO）"
}

func describeQC(findings []domain.QCFinding) string {
	lines := make([]string, len(findings))
	for i, f := range findings {
		lines[i] = "・" + f.Message
	}
	return strings.Join(lines, "\n")
}
