package chatwire

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/shotaudio"
	"github.com/ousiassllc/mam/backend/internal/service/shotaudiosvc"
)

// The chat operation that ties an attached audio file to a Shot (service/chatops/shotaudio, #1153): the screen and the MCP tool set_shot_audio call the same
// shotaudiosvc.Service.Set, and the file enters the Project as an asset of type audio when the chat stores the attachment.
func init() {
	builders = append(builders, func(env Env, reg *chatops.Runtime) {
		if env.ShotAudio == nil {
			return
		}
		svc := env.ShotAudio
		shot := func(v shotaudiosvc.View) shotaudio.Shot {
			out := shotaudio.Shot{Scene: v.Scene, Number: v.Number, HasAudio: v.Tie != nil}
			if v.Tie != nil {
				out.Seconds, out.Problem = v.Tie.Seconds, v.Tie.Problem
			}
			return out
		}
		reg.Handle(shotaudio.OpSetShotAudio, shotaudio.New(shotaudio.Deps{
			Shot: func(ctx context.Context, projectID, shotID string) (shotaudio.Shot, error) {
				v, err := svc.Get(ctx, projectID, shotID)
				return shot(v), shotAudioRefusal(err)
			},
			Set: func(ctx context.Context, projectID, shotID, assetID string) (shotaudio.Shot, error) {
				v, err := svc.Set(ctx, projectID, shotID, assetID)
				return shot(v), shotAudioRefusal(err)
			},
		}))
	})
}

// shotAudioRefusal turns what the screen refuses (a Shot or a file that is gone, a Shot that is being generated, a LOCKed Prompt, a file that is no audio) into *chatops.Refusal.
func shotAudioRefusal(err error) error {
	var (
		invalid *shotaudiosvc.InvalidError
		locked  *shotaudiosvc.LockedError
	)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, shotaudiosvc.ErrNotFound):
		return &chatops.Refusal{Message: "Shot か音声ファイルが見つかりません。音声ファイルを添付して、もう一度送ってください"}
	case errors.Is(err, shotaudiosvc.ErrBusy):
		return &chatops.Refusal{Message: "この Shot は生成中（または修復中）のため、音声を変えられません。終わってからもう一度お願いします"}
	case errors.As(err, &locked):
		return &chatops.Refusal{Message: "この Shot の Prompt は LOCK されているため、音声を変えられません。LOCK を解除してから指示してください"}
	case errors.As(err, &invalid):
		return &chatops.Refusal{Message: invalid.Message}
	}
	return err
}
