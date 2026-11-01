package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
	"github.com/ousiassllc/mam/backend/internal/service/audiosummary"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

// GenerateAudio makes the Audio Timeline of the latest Edit Timeline of the trunk (branchID "") or
// of a Branch (POST /projects/:id/audio/generate): the Audio Director reads the Scenes of the cut,
// speaks Dialogue and Narration, makes the BGM, Ambience, SE, Foley and Lip Sync the Quality Policy
// pays for within the budget, mixes everything with the sound of the Rough Cut and normalizes the
// loudness. The result is the next Audio Timeline version of that Branch. The Project must be in
// AUDIO (the Editor Judge passed). The providers are the saved settings (audiosettings), read on every run.
func (s *EditorService) GenerateAudio(ctx context.Context, projectID, branchID string) (EditorView, error) {
	opts, err := s.prefs.Load()
	if err != nil {
		return EditorView{}, audioError(err)
	}
	return s.GenerateAudioWith(ctx, projectID, branchID, opts)
}

// AudioOptions are the choices of one run of the Audio Director: the saved audio settings (audiosettings.Settings).
type AudioOptions = audiosettings.Settings

// GenerateAudioWith is GenerateAudio with the choices of the run.
func (s *EditorService) GenerateAudioWith(ctx context.Context, projectID, branchID string, opts AudioOptions) (EditorView, error) {
	if field := localvoice.InvalidProvider(opts.VoiceProvider, opts.LipSyncProvider); field != "" {
		return EditorView{}, invalid(field, "声のプロバイダー（voice_provider）は elevenlabs か local、口パクのプロバイダー（lip_sync_provider）は synclabs か local を指定してください")
	}
	if s.audio == nil || s.tools == nil {
		return EditorView{}, ErrUnavailable
	}
	release, err := s.acquire(projectID)
	if err != nil {
		return EditorView{}, err
	}
	defer release()

	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	if project.State != domain.StateAudio {
		return EditorView{}, conflict("音声を作れるのは Editor Judge に合格した後（AUDIO）です（現在: %s）", project.State)
	}
	ver, err := s.editors.LatestEditTimeline(ctx, projectID, branchID)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	roughPath, info, err := s.roughInfo(ctx, ver)
	if err != nil {
		return EditorView{}, err
	}
	brief, _, err := s.briefOptions(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	policy, err := s.policies.Get(ctx, projectID)
	if err != nil {
		return EditorView{}, notFound(err)
	}
	in := audiodirector.Input{
		ProjectID: projectID, EditTimelineID: ver.ID, Brief: brief, Template: project.ProductionTemplate, Quality: policy.Quality,
		Rough: roughPath, HasSound: info.HasAudio, Seconds: info.Duration, VoiceProvider: opts.VoiceProvider, LipSyncProvider: opts.LipSyncProvider,
	}
	if in.Scenes, err = s.audioScenes(ctx, projectID, branchID, ver.Timeline); err != nil {
		return EditorView{}, err
	}
	if domain.AudioScopeOf(policy.Quality).LipSync {
		if in.Clips, err = s.videoClips(ctx, ver.Timeline); err != nil {
			return EditorView{}, err
		}
	}
	notes, err := s.prefs.Settle(ctx, &in, s.voiceRef, branchID, opts.FallbackToCloud)
	if err != nil {
		return EditorView{}, audioError(err)
	}
	if err := s.fixMusic(ctx, projectID, branchID, &in); err != nil {
		return EditorView{}, err
	}
	clones, err := s.voices.List(ctx, projectID)
	if err != nil {
		return EditorView{}, err
	}
	in.Voices = localvoice.VoiceIDs(clones)

	in.RunID = domain.NewID()
	tl, err := s.audio.Produce(ctx, in)
	if err != nil {
		return EditorView{}, audioError(err)
	}
	if _, err := s.editors.SaveAudioTimeline(context.WithoutCancel(ctx), projectID, branchID, tl); err != nil {
		// No Audio Timeline refers to the files of the run: they are not kept (the cost ledger is).
		_ = s.audio.DiscardRun(in)
		return EditorView{}, notFound(err)
	}
	s.announce(ctx, projectID, assistantText(audiosummary.Of(tl, branchID != "", notes...)))
	return s.View(ctx, projectID, branchID)
}

// fixMusic holds the BGM when the Project's Music is MANUAL or LOCK (spec/spec.md §6): the Audio
// Director carries over the BGM of the latest Audio Timeline, whose files are still there, instead
// of planning and making it again.
func (s *EditorService) fixMusic(ctx context.Context, projectID, branchID string, in *audiodirector.Input) error {
	locks, err := s.locks.ProjectLocks(ctx, projectID)
	if err != nil {
		return err
	}
	if locks.Mode(domain.ParamMusic) == domain.LockAuto {
		return nil
	}
	in.MusicFixed = true
	prev, err := s.editors.LatestAudioTimeline(ctx, projectID, branchID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, c := range prev.Timeline.Clips {
		if c.LayerOf() == domain.LayerBGM && s.fileExists(c.Path) {
			in.FixedBGM = append(in.FixedBGM, c)
		}
	}
	return nil
}

// audioScenes reads the Scenes of the cut in the order they play: where each sits in the finished
// video, how many clips it has, what the script says there (Dialogue / Narration only for Scenes of
// the script, not for the Scenes of a Branch) and what the Shots of its clips are for.
func (s *EditorService) audioScenes(ctx context.Context, projectID, branchID string, tl domain.EditTimeline) ([]audiodirector.Scene, error) {
	idx, err := s.shotIndex(ctx, projectID, branchID)
	if err != nil {
		return nil, err
	}
	var script domain.Script
	switch sv, err := s.plans.LatestScript(ctx, projectID); {
	case err == nil:
		script = sv.Doc.Script
	case !errors.Is(err, repository.ErrNotFound):
		return nil, err
	}
	starts := tl.Starts()
	var scenes []audiodirector.Scene
	at := map[string]int{} // scene ID -> position in scenes
	for i, c := range tl.Clips {
		ref, ok := idx.byID[c.ShotID]
		if !ok {
			continue
		}
		end := starts[i] + c.Length()
		pos, seen := at[ref.Scene.ID]
		if !seen {
			pos = len(scenes)
			at[ref.Scene.ID] = pos
			sc := audiodirector.Scene{
				Index: pos + 1, Title: ref.Scene.Title, Objective: ref.Scene.Objective, Location: ref.Scene.Location, Start: starts[i], End: end,
			}
			if ref.Scene.BranchID == "" && ref.Scene.OrderIndex < len(script.Scenes) {
				ss := script.Scenes[ref.Scene.OrderIndex]
				sc.Description, sc.Lines = ss.Description, ss.Dialogue
			}
			scenes = append(scenes, sc)
		}
		sc := &scenes[pos]
		sc.Start, sc.End = min(sc.Start, starts[i]), max(sc.End, end)
		sc.Clips++
		if ref.Shot.Objective != "" {
			sc.ShotObjectives = append(sc.ShotObjectives, ref.Shot.Objective)
		}
	}
	return scenes, nil
}

// videoClips are the clips of the Edit Timeline with their Take files, for Lip Sync.
func (s *EditorService) videoClips(ctx context.Context, tl domain.EditTimeline) ([]audiodirector.VideoClip, error) {
	starts := tl.Starts()
	clips := make([]audiodirector.VideoClip, 0, len(tl.Clips))
	sources, err := s.loadSources(ctx, clipTakeIDs(tl.Clips))
	if err != nil {
		return nil, err
	}
	for i, c := range tl.Clips {
		src, ok := sources.take(ctx, c.TakeID)
		if !ok {
			return nil, conflict("クリップ %d の Take のファイルが見つかりません。編集し直してください", i+1)
		}
		clips = append(clips, audiodirector.VideoClip{Index: i + 1, TakeID: c.TakeID, Path: s.assetPath(src.path), In: c.In, Out: c.Out, Start: starts[i]})
	}
	return clips, nil
}

// audioError is the error the user sees when the audio stage failed.
func audioError(err error) error {
	var (
		pe  *audiodirector.ProviderError
		be  *audiodirector.BudgetError
		api *llm.APIError
	)
	reason, local := localspeech.Reason(err)
	switch {
	case local:
		return &AIError{Reason: reason, Err: err}
	case errors.Is(err, voicerefsvc.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, audiodirector.ErrNoAPIKey):
		return &AIError{Reason: "ElevenLabs の API キーが設定されていません（設定画面で elevenlabs のキーを登録してください）", Err: err}
	case errors.As(err, &be):
		return conflict("セリフの音声合成に約 $%.2f かかりますが、残りの予算が $%.2f です。予算上限を引き上げてから再実行してください", be.Needed, be.Remaining)
	case errors.As(err, &pe) && pe.Status == http.StatusTooManyRequests:
		return &AIError{Reason: "ElevenLabs のレート制限に達しました。しばらく待ってから再実行してください", Err: err}
	case errors.As(err, &pe):
		return &AIError{Reason: "ElevenLabs の呼び出しに失敗しました", Err: err}
	case errors.Is(err, media.ErrUnavailable):
		return ErrUnavailable
	case errors.Is(err, llm.ErrInvalidOutput), errors.Is(err, llm.ErrNoAPIKey), errors.Is(err, llm.ErrTimeout), errors.Is(err, llm.ErrCircuitOpen), errors.As(err, &api):
		return aiError(err) // the Audio Director's planner
	}
	return &AIError{Reason: "音声のミキシングに失敗しました: " + err.Error(), Err: err}
}

// MaxVoiceSampleBytes is the largest voice sample recording.
const MaxVoiceSampleBytes = 10 << 20

// CloneVoice clones a voice from a sample recording and makes it the voice of the speaker's lines
// in the Project (POST /projects/:id/audio/voices, ElevenLabs instant voice cloning). A speaker
// has one voice: a new clone replaces the old one.
func (s *EditorService) CloneVoice(ctx context.Context, projectID, speaker, name string, file Upload) (domain.VoiceClone, error) {
	speaker, name = strings.TrimSpace(speaker), strings.TrimSpace(name)
	if speaker == "" || len([]rune(speaker)) > domain.MaxSpeakerRunes {
		return domain.VoiceClone{}, invalid("speaker", "話者名は 1〜%d 文字で入力してください", domain.MaxSpeakerRunes)
	}
	if file.Open == nil {
		return domain.VoiceClone{}, invalid("sample", "声のサンプルの音声ファイルを選んでください")
	}
	if name == "" {
		name = speaker
	}
	if s.cloner == nil {
		return domain.VoiceClone{}, ErrUnavailable
	}
	if _, err := s.getProject(ctx, projectID); err != nil {
		return domain.VoiceClone{}, err
	}
	f, err := file.Open()
	if err != nil {
		return domain.VoiceClone{}, invalid("sample", "声のサンプルを読み取れませんでした")
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxVoiceSampleBytes+1))
	switch {
	case err != nil:
		return domain.VoiceClone{}, invalid("sample", "声のサンプルを読み取れませんでした")
	case len(data) == 0 || len(data) > MaxVoiceSampleBytes:
		return domain.VoiceClone{}, invalid("sample", "声のサンプルは 1 バイト以上 %d MB 以下にしてください", MaxVoiceSampleBytes>>20)
	}
	voiceID, err := s.cloner.Clone(ctx, name, audiodirector.Sample{Filename: file.Name, Data: data})
	if err != nil {
		return domain.VoiceClone{}, audioError(err)
	}
	// The voice is billed once the vendor created it: it is stored even when the request is gone, and
	// a failed save leaves its voice_id in the log (the vendor keeps the voice; it is not lost silently).
	clone, err := s.voices.Save(context.WithoutCancel(ctx), projectID, speaker, name, voiceID)
	if err != nil {
		s.logger.Error("save the cloned voice failed", "project_id", projectID, "speaker", speaker, "voice_id", voiceID, "err", err)
		return domain.VoiceClone{}, notFound(err)
	}
	return clone, nil
}

// Voices lists the cloned voices of the Project.
func (s *EditorService) Voices(ctx context.Context, projectID string) ([]domain.VoiceClone, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.voices.List(ctx, projectID)
}
