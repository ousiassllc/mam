// Package voicereference holds the MCP tools of the reference voice of a Character (F-20, F-29): the recording a
// local voice provider clones. The recording is uploaded with create_upload_ticket (purpose
// character_voice_reference); here are the change of its transcript and language, its removal and its download URL,
// for the canonical Characters of a Series (series_id) and for the Characters of a Project (project_id).
// The screen's routes are in spec/api/endpoints.md §2.9.
package voicereference

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

// Names of the upload purpose and the download kinds of this package.
const (
	PurposeUpload  = "character_voice_reference"
	KindSeriesRef  = "series_voice_reference"
	KindProjectRef = "project_voice_reference"
)

// Register adds the tools, the upload purpose and the download kinds of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	v := svc.VoiceRef
	r.Files.RegisterUpload(uploadSpec(v))
	r.Files.RegisterDownload(KindSeriesRef, download(v.PlaybackForSeries))
	r.Files.RegisterDownload(KindProjectRef, download(v.PlaybackForProject))

	mcpkit.Write(r, "update_character_voice_reference",
		"登場人物の参照音声の書き起こしと言語だけを変える（音声ファイルは create_upload_ticket の purpose=character_voice_reference で送る）。渡さない項目はそのまま。series_id か project_id のどちらか一方を渡す。Series の正本なら正本に新しいバージョンができ、作成済みの Episode は変わらない。参照音声が未設定なら validation / Change the transcript / language of a Character's reference voice.",
		update(svc), mcpkit.Idempotent())
	mcpkit.Destroy(r, "delete_character_voice_reference",
		"登場人物の参照音声を削除する（ほかの Episode や正本に複製された音声は残る）。series_id か project_id のどちらか一方を渡す。未設定なら何もしない / Remove the reference voice of a Character.",
		remove(svc), mcpkit.Idempotent())
	mcpkit.Read(r, "get_character_voice_reference_url",
		"登場人物の参照音声（音声ファイル）のダウンロード URL（15 分有効）。series_id か project_id のどちらか一方を渡す。未設定なら not_found / Download URL of a Character's reference voice.",
		url(r))
}

// Target names the Character: of a Series canon (series_id) or of a Project (project_id), exactly one.
type Target struct {
	SeriesID    string `json:"series_id,omitempty" jsonschema:"Series の正本の登場人物のとき（get_series の canon.characters の id と組）"`
	ProjectID   string `json:"project_id,omitempty" jsonschema:"Project の登場人物のとき（get_world_state の characters の id と組）"`
	CharacterID string `json:"character_id"`
}

func (t Target) check() error {
	if (t.SeriesID == "") == (t.ProjectID == "") {
		return &service.ValidationError{Field: "series_id", Message: "series_id か project_id のどちらか一方を指定してください"}
	}
	if strings.TrimSpace(t.CharacterID) == "" {
		return &service.ValidationError{Field: "character_id", Message: "character_id を指定してください"}
	}
	return nil
}

// Reference is the reference voice of a Character as the tools tell it (the stored file is not named).
type Reference struct {
	MimeType   string `json:"mime_type"`
	SizeBytes  int64  `json:"size_bytes"`
	DurationMS int    `json:"duration_ms" jsonschema:"録音の長さ（ミリ秒）。ffprobe がなくて測れなかったときは 0"`
	Text       string `json:"text,omitempty" jsonschema:"音声の書き起こし"`
	Language   string `json:"language" jsonschema:"ja, en, zh, ko, yue"`
}

// Output is the Character and its reference voice after the call; VoiceReference is nil when it has none.
type Output struct {
	SeriesID       string     `json:"series_id,omitempty"`
	ProjectID      string     `json:"project_id,omitempty"`
	CharacterID    string     `json:"character_id"`
	VoiceReference *Reference `json:"voice_reference"`
}

func output(t Target, r *voiceref.Reference) Output {
	out := Output{SeriesID: t.SeriesID, ProjectID: t.ProjectID, CharacterID: t.CharacterID}
	if r != nil {
		out.VoiceReference = &Reference{MimeType: r.MimeType, SizeBytes: r.SizeBytes, DurationMS: r.DurationMS, Text: r.Text, Language: r.Language}
	}
	return out
}

// serviceError turns a refusal of voicerefsvc into the error of package service the tools report.
func serviceError(err error) error {
	var invalid *voicerefsvc.InvalidError
	switch {
	case errors.As(err, &invalid):
		return &service.ValidationError{Field: invalid.Field, Message: invalid.Message}
	case errors.Is(err, voicerefsvc.ErrNotFound):
		return service.ErrNotFound
	}
	return err
}

func set(ctx context.Context, svc *voicerefsvc.Service, t Target, in voicerefsvc.Input) (Output, error) {
	if err := t.check(); err != nil {
		return Output{}, err
	}
	var ref voiceref.Reference
	var err error
	if t.SeriesID != "" {
		ref, err = svc.SetForSeries(ctx, t.SeriesID, t.CharacterID, in)
	} else {
		ref, err = svc.SetForProject(ctx, t.ProjectID, t.CharacterID, in)
	}
	if err != nil {
		return Output{}, serviceError(err)
	}
	return output(t, &ref), nil
}

// UpdateInput is the change of the transcript and the language.
type UpdateInput struct {
	Target
	Text     *string `json:"text,omitempty" jsonschema:"書き起こし（音声が言っている内容、500 文字以内。空文字で消す）"`
	Language *string `json:"language,omitempty" jsonschema:"ja（既定）, en, zh, ko, yue"`
}

func update(svc *service.Services) mcpkit.Handler[UpdateInput, Output] {
	return func(ctx context.Context, in UpdateInput) (Output, error) {
		return set(ctx, svc.VoiceRef, in.Target, voicerefsvc.Input{Text: in.Text, Language: in.Language})
	}
}

func remove(svc *service.Services) mcpkit.Handler[Target, Output] {
	return func(ctx context.Context, in Target) (Output, error) {
		if err := in.check(); err != nil {
			return Output{}, err
		}
		var err error
		if in.SeriesID != "" {
			err = svc.VoiceRef.RemoveForSeries(ctx, in.SeriesID, in.CharacterID)
		} else {
			err = svc.VoiceRef.RemoveForProject(ctx, in.ProjectID, in.CharacterID)
		}
		if err != nil {
			return Output{}, serviceError(err)
		}
		return output(in, nil), nil
	}
}

func url(r *mcpkit.Registrar) mcpkit.Handler[Target, mcpkit.Link] {
	return func(ctx context.Context, in Target) (mcpkit.Link, error) {
		if err := in.check(); err != nil {
			return mcpkit.Link{}, err
		}
		if in.SeriesID != "" {
			return r.Files.DownloadURL(ctx, KindSeriesRef, in.SeriesID+"/"+in.CharacterID)
		}
		return r.Files.DownloadURL(ctx, KindProjectRef, in.ProjectID+"/"+in.CharacterID)
	}
}

func download(playback func(ctx context.Context, ownerID, characterID string) (voicerefsvc.Playback, error)) mcpkit.DownloadFunc {
	return func(ctx context.Context, id string) (mcpkit.DownloadFile, error) {
		ownerID, characterID, ok := strings.Cut(id, "/")
		if !ok {
			return mcpkit.DownloadFile{}, service.ErrNotFound
		}
		p, err := playback(ctx, ownerID, characterID)
		if err != nil {
			return mcpkit.DownloadFile{}, serviceError(err)
		}
		return mcpkit.DownloadFile{Name: p.Name, ContentType: p.MimeType, Path: p.Path}, nil
	}
}

func uploadSpec(svc *voicerefsvc.Service) mcpkit.UploadSpec {
	return mcpkit.UploadSpec{
		Purpose: PurposeUpload,
		Description: fmt.Sprintf("登場人物の参照音声（声のクローン用の録音、%s、%d MB まで、3〜10 秒）を置き換える。series_id（Series の正本）か project_id（Project の登場人物）のどちらか一方と character_id を渡す。Series の正本なら正本に新しいバージョンができ、作成済みの Episode は変わらない / Set the reference voice of a Character.",
			strings.Join(voiceref.Extensions(), " "), voiceref.MaxBytes>>20),
		MaxBytes: voiceref.MaxBytes,
		Params: []mcpkit.UploadParam{
			{Name: "series_id", Description: "Series の正本の登場人物のとき（project_id とどちらか一方）"},
			{Name: "project_id", Description: "Project の登場人物のとき（series_id とどちらか一方）"},
			{Name: "character_id", Description: "get_series の canon.characters / get_world_state の characters の id", Required: true},
			{Name: "text", Description: "録音の書き起こし（500 文字以内。省略すると空）"},
			{Name: "language", Description: "ja（既定）, en, zh, ko, yue"},
		},
		Handle: func(ctx context.Context, in mcpkit.UploadInput) (any, error) {
			p := in.Params
			input := voicerefsvc.Input{File: voicerefsvc.File(in.Upload())}
			if p["text"] != "" {
				input.Text = new(p["text"])
			}
			if p["language"] != "" { // left out: the language the Character has
				input.Language = new(p["language"])
			}
			return set(ctx, svc, Target{SeriesID: p["series_id"], ProjectID: p["project_id"], CharacterID: p["character_id"]}, input)
		},
	}
}
