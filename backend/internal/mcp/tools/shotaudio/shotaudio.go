// Package shotaudio holds the MCP tools of the audio of a Shot (Issue #1153): which audio asset of the Project a Shot is made from by a speech-to-video model (Wan 2.2 S2V), and
// the ones it can be tied to. The tools are the screen's PUT /shots/:id/audio (spec/api/endpoints.md): both call shotaudiosvc.Service, so a refusal is the same sentence. The audio
// file itself enters the Project as an asset of type audio (a chat attachment, a Project upload: create_upload_ticket) and is handed out by get_asset_download_url.
package shotaudio

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/shotaudiosvc"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	mcpkit.Read(r, "get_shot_audio",
		"Shot に結び付けた音声（Wan 2.2 S2V など、音声から動画を作るモデルの入力）と、結び付けられる Project の音声アセット / The audio asset a Shot is made from, and the Project's audio assets it can use。",
		get(svc.ShotAudio))
	mcpkit.Write(r, "set_shot_audio",
		"Shot に Project の音声アセットを結び付ける（asset_id を空にすると外す）。次の生成から、音声を受け取るモデルが Keyframe と音声から動画を作る（長さは音声の長さ）。生成中は conflict、Prompt が LOCK なら locked、音声でないアセットは validation / Tie a Project audio asset to a Shot (empty asset_id takes it off).",
		set(svc.ShotAudio))
}

// GetInput names the Shot.
type GetInput struct {
	ShotID string `json:"shot_id"`
}

// SetInput names the Shot and the audio asset.
type SetInput struct {
	ShotID  string `json:"shot_id"`
	AssetID string `json:"asset_id,omitempty" jsonschema:"Project の音声アセットの ID（get_shot_audio の choices[].asset_id）。空か省略で、Shot の音声を外す"`
}

// Output is the audio of a Shot (shotaudiosvc.Report, also the answer of the JSON API).
type Output = shotaudiosvc.Report

func get(svc *shotaudiosvc.Service) mcpkit.Handler[GetInput, Output] {
	return func(ctx context.Context, in GetInput) (Output, error) {
		v, err := svc.Get(ctx, "", in.ShotID)
		return v.Report(), serviceError(err)
	}
}

func set(svc *shotaudiosvc.Service) mcpkit.Handler[SetInput, Output] {
	return func(ctx context.Context, in SetInput) (Output, error) {
		v, err := svc.Set(ctx, "", in.ShotID, in.AssetID)
		return v.Report(), serviceError(err)
	}
}

// serviceError turns a refusal of shotaudiosvc into the error of package service the tools report.
func serviceError(err error) error {
	var (
		invalid *shotaudiosvc.InvalidError
		locked  *shotaudiosvc.LockedError
	)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, shotaudiosvc.ErrNotFound):
		return service.ErrNotFound
	case errors.Is(err, shotaudiosvc.ErrBusy):
		return service.ErrBusy
	case errors.As(err, &locked):
		return &service.LockedError{Parameters: locked.Parameters}
	case errors.As(err, &invalid):
		return &service.ValidationError{Field: invalid.Field, Message: invalid.Message}
	}
	return err
}
