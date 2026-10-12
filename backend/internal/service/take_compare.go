package service

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// TakeView is a Take of the comparison.
type TakeView struct {
	domain.TakeInfo
	// Label names the Take in the comparison and in words: "A", "B", ...
	Label string
	// FPS is the frame rate of the file (0 when it could not be read).
	FPS float64
}

// Comparison is the Takes of a Shot side by side.
type Comparison struct {
	Project domain.Project
	Shot    domain.Shot
	Takes   []TakeView
	// MinAxisScore is the score every axis of a Take needs to reach for the Judge to let it pass: the
	// Quality Policy of the Project with the user's override applied. The Takes below it are the weak ones.
	MinAxisScore int
	// PartialUse tells whether Partial Take Use is offered: ffmpeg is installed and a Take can be cut.
	PartialUse bool
}

// Compare returns the Takes of the Shot, oldest first. The size, length and frame rate of a file
// the Model Adapter did not describe are read from the file.
func (s *TakeService) Compare(ctx context.Context, shotID string) (Comparison, error) {
	ref, err := s.shots.GetShot(ctx, shotID)
	if err != nil {
		return Comparison{}, notFound(err)
	}
	takes, err := s.takes.ShotTakes(ctx, shotID)
	if err != nil {
		return Comparison{}, err
	}
	project, err := s.projects.Get(ctx, ref.ProjectID)
	if err != nil {
		return Comparison{}, notFound(err)
	}
	policy, err := s.policies.Get(ctx, ref.ProjectID)
	if err != nil {
		return Comparison{}, notFound(err)
	}
	out := Comparison{Project: project, Shot: ref.Shot, MinAxisScore: policy.Effective().MinAxisScore, Takes: make([]TakeView, len(takes))}
	for i, t := range takes {
		v := TakeView{TakeInfo: t, Label: domain.TakeLabel(i)}
		if abs := s.assetPath(t.FilePath); s.tools != nil && abs != "" {
			if info, err := s.tools.Probe(ctx, abs); err == nil {
				v.FPS = info.FPS
				if v.Resolution == "" && info.Height > 0 {
					v.Resolution = fmt.Sprintf("%dp", info.Height)
				}
				if v.DurationSeconds == 0 {
					v.DurationSeconds = info.Duration
				}
			}
		}
		out.Takes[i] = v
		out.PartialUse = out.PartialUse || (s.tools != nil && t.Usable())
	}
	return out, nil
}
