// Package audioupload is the upload purpose "gentest_audio" of the MCP tools of the generation test (Issue #1153): the audio file a speech-to-video model
// (Wan 2.2 S2V) makes the clip from. The bytes are staged like an input picture and the answer's audio_id goes to estimate_generation_test and
// run_generation_test as a reference with the role "audio".
package audioupload

import (
	"context"
	"io"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/audioin"
)

// Purpose is the name of the upload purpose (create_upload_ticket).
const Purpose = "gentest_audio"

// Spec is the upload purpose. svc is the Service of the generation test (an error when the app has none) and mapErr turns its errors into the tool errors,
// both of the parent package, which this one cannot import.
func Spec(svc func() (*gentest.Service, error), mapErr func(error) error) mcpkit.UploadSpec {
	return mcpkit.UploadSpec{
		Purpose:     Purpose,
		Description: "音声から動画（S2V）の入力音声。WAV か MP3、50 MiB まで。応答の audio_id を estimate_generation_test / run_generation_test の references に {source: upload, id: audio_id, role: audio} として渡す / Input audio of a speech-to-video generation (WAV or MP3, up to 50 MiB); pass the returned audio_id as {source: upload, id, role: audio}.",
		MaxBytes:    audioin.MaxBytes,
		Handle: func(_ context.Context, in mcpkit.UploadInput) (any, error) {
			g, err := svc()
			if err != nil {
				return nil, err
			}
			f, err := in.Header.Open()
			if err != nil {
				return nil, err
			}
			defer func() { _ = f.Close() }()
			// One byte more than the limit, so that a larger file is refused instead of being cut to the limit and accepted.
			data, err := io.ReadAll(io.LimitReader(f, audioin.MaxBytes+1))
			if err != nil {
				return nil, err
			}
			id, mime, err := g.StageAudio(data)
			if err != nil {
				return nil, mapErr(err)
			}
			return map[string]any{"audio_id": id, "mime_type": mime}, nil
		},
	}
}
