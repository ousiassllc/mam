package modelmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/progress"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
)

// handleGentest is the handler of domain.JobGentest. It is not resumable: the ComfyUI prompt that was running
// has no owner after a restart, so the queue fails the job and the user runs it again.
func (s *Service) handleGentest(ctx context.Context, job domain.Job) (err error) {
	var p gentestPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("bad payload: %w", err)
	}
	target := "gentest:" + p.ModelID
	defer s.tracker.Finish(job.ID)
	set := func(u track.Update) { s.tracker.Set(job.ID, target, u) }
	set(track.Update{Phase: "prepare", Message: "準備しています"})

	rec, dir, err := s.beginTrial(job, p)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			rec.Status, rec.Error = GentestFailed, err.Error()
			now := s.Now().UTC()
			rec.FinishedAt = &now
			_ = writeRecord(dir, rec)
		}
	}()
	cfg, err := s.readyConfig(ctx, p.ModelID)
	if err != nil {
		return userFacing(ctx, err)
	}
	_, endpoint, name, _ := localcfg.ParseID(p.ModelID)
	model, ok := cfg.LocalModels[localcfg.Key(endpoint, name)]
	if !ok {
		return fmt.Errorf("モデル %s がありません", p.ModelID)
	}
	backends := localbackends.Build(cfg, s.Keys, s.Backends)
	ctx = progress.With(ctx, func(u progress.Update) {
		set(track.Update{Percent: u.Percent, Phase: "generate", Message: u.Message})
	})
	set(track.Update{Phase: "generate", Message: "生成を始めています"})
	data, mime, err := s.generate(ctx, backends, domain.ModelName(p.ModelID), model, p, dir)
	if err != nil {
		return userFacing(ctx, err)
	}
	file := "output" + extensionFor(mime)
	if err := data.store(filepath.Join(dir, file)); err != nil {
		return err
	}
	rec.Status, rec.File, rec.MimeType, rec.SizeBytes = GentestSucceeded, file, mime, data.size
	now := s.Now().UTC()
	rec.FinishedAt = &now
	if err := writeRecord(dir, rec); err != nil {
		return err
	}
	set(track.Update{Percent: 100, Phase: "done", Message: "生成しました"})
	return nil
}

// beginTrial creates the trial's directory, moves the picture in and writes the record as running.
func (s *Service) beginTrial(job domain.Job, p gentestPayload) (GentestRecord, string, error) {
	dir := filepath.Join(s.gentestDir(), job.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return GentestRecord{}, "", err
	}
	media := localcfg.MediaImage
	if _, _, _, ok := localcfg.ParseID(p.ModelID); ok {
		if m, found := s.modelOf(p.ModelID); found {
			media = m.Media
		}
	}
	rec := GentestRecord{
		ID: job.ID, ModelID: p.ModelID, Media: media, Prompt: p.Prompt, NegativePrompt: p.NegativePrompt, AspectRatio: p.AspectRatio,
		Resolution: p.Resolution, DurationSeconds: p.DurationSeconds, HasReference: p.Input != "", Status: GentestRunning, CreatedAt: s.Now().UTC(),
	}
	if p.Input != "" {
		src := filepath.Join(s.gentestDir(), inputsDirName, p.Input)
		if err := os.Rename(src, filepath.Join(dir, referenceName(p))); err != nil {
			return rec, dir, fmt.Errorf("参照画像を取り出せません: %w", err)
		}
	}
	return rec, dir, writeRecord(dir, rec)
}

func referenceName(p gentestPayload) string { return "reference" + filepath.Ext(p.Input) }

func (s *Service) modelOf(id string) (localcfg.Model, bool) {
	_, endpoint, name, ok := localcfg.ParseID(id)
	if !ok {
		return localcfg.Model{}, false
	}
	m, found := s.Adapter.Config().LocalModels[localcfg.Key(endpoint, name)]
	return m, found
}

func writeRecord(dir string, rec GentestRecord) error {
	data, err := json.MarshalIndent(rec, "", " ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, recordFile), data, 0o600)
}

// readyConfig starts the bundled engine when the model is on it and returns the setting to run with: the engine's
// address is in it by then (the endpoint is synced when the engine starts).
func (s *Service) readyConfig(ctx context.Context, modelID string) (modeladapter.Config, error) {
	_, endpoint, _, _ := localcfg.ParseID(modelID)
	if endpoint == BundledEndpoint {
		if _, err := s.StartEngine(ctx); err != nil {
			return modeladapter.Config{}, err
		}
	}
	return s.Adapter.Config(), nil
}

// result is a generated file in memory or staged.
type result struct {
	data []byte
	blob *modeladapter.Blob
	size int64
}

func (r result) store(dst string) error {
	if r.blob != nil {
		return r.blob.StoreAs(dst, 0o600)
	}
	return fsutil.WriteFileAtomic(dst, r.data, 0o600)
}

// generate calls the backend of the model with the trial's request.
func (s *Service) generate(ctx context.Context, b modeladapter.Backends, id domain.ModelName, m localcfg.Model, p gentestPayload, dir string) (result, string, error) {
	var ref *modeladapter.Input
	if p.Input != "" {
		data, err := os.ReadFile(filepath.Join(dir, referenceName(p))) //nolint:gosec // dir is the trial's own directory
		if err != nil {
			return result{}, "", fmt.Errorf("参照画像を読めません: %w", err)
		}
		ref = &modeladapter.Input{Data: data, MimeType: p.InputMime}
	}
	if m.Media == localcfg.MediaVideo {
		backend, ok := b.Video[id]
		if !ok {
			return result{}, "", fmt.Errorf("モデル %s の接続先を用意できません", id)
		}
		req := modeladapter.VideoRequest{
			Prompt: p.Prompt, NegativePrompt: p.NegativePrompt, DurationSeconds: p.DurationSeconds, Resolution: p.Resolution, AspectRatio: p.AspectRatio,
		}
		if ref != nil {
			if caps, _ := m.VideoCaps(); caps.FirstFrame {
				req.FirstFrame = ref
			} else {
				req.References = []modeladapter.Input{*ref}
			}
		}
		out, err := backend.GenerateVideo(ctx, "", req)
		if err != nil {
			return result{}, "", err
		}
		return result{blob: &out.File, size: out.File.Size()}, out.MimeType, nil
	}
	backend, ok := b.Image[id]
	if !ok {
		return result{}, "", fmt.Errorf("モデル %s の接続先を用意できません", id)
	}
	req := modeladapter.ImageRequest{Prompt: p.Prompt, AspectRatio: p.AspectRatio}
	if ref != nil {
		req.Inputs = []modeladapter.Input{*ref}
	}
	out, err := backend.GenerateImage(ctx, "", req)
	if err != nil {
		return result{}, "", err
	}
	return result{data: out.Data, size: int64(len(out.Data))}, out.MimeType, nil
}

func extensionFor(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	}
	return ".png"
}
