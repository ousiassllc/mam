// Package shotaudioapi is the JSON API of the audio of a Shot (Issue #1153): GET and PUT /api/v1/shots/{id}/audio. The audio is the sound a speech-to-video model (Wan 2.2 S2V)
// makes the Shot's clip from. It is a package of its own, outside router, so that router stays inside its line limit; router registers it with the other operations. The Shot
// screen's form (PUT /shots/:id/audio, an action route) and the MCP tools go through the same shotaudiosvc.Service, so a refusal is the same sentence on every entry point.
package shotaudioapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/mam/backend/internal/service/shotaudiosvc"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

type output struct {
	Body shotaudiosvc.Report
}

// Register adds get-shot-audio and set-shot-audio.
func Register(api huma.API, svc *shotaudiosvc.Service) {
	huma.Register(api, huma.Operation{
		OperationID: "get-shot-audio",
		Method:      http.MethodGet,
		Path:        paths.APIPrefix + "/shots/{id}/audio",
		Summary:     "The audio asset a Shot is made from, and the audio assets of its Project it can use",
		Tags:        []string{"shots"},
		Errors:      []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ID string `path:"id" doc:"Shot ID"`
	}) (*output, error) {
		v, err := svc.Get(ctx, "", in.ID)
		if err != nil {
			return nil, statusError(err)
		}
		return &output{Body: v.Report()}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "set-shot-audio",
		Method:      http.MethodPut,
		Path:        paths.APIPrefix + "/shots/{id}/audio",
		Summary:     "Tie an audio asset of the Project to a Shot, or take the audio off (asset_id \"\")",
		Description: "The next generation of the Shot makes its clip from this sound with a model that takes one (the clip is as long as the sound). " +
			"409: the Shot is being generated or repaired. 423: the Prompt of the Shot is LOCKed. 422: the asset is not an audio file. 404: the Shot, or an asset of this Project, does not exist.",
		Tags:   []string{"shots"},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusLocked},
	}, func(ctx context.Context, in *struct {
		ID   string `path:"id" doc:"Shot ID"`
		Body struct {
			AssetID string `json:"asset_id" doc:"ID of an audio asset of the Shot's Project (choices[].asset_id of the GET); \"\" takes the audio off"`
		}
	}) (*output, error) {
		v, err := svc.Set(ctx, "", in.ID, in.Body.AssetID)
		if err != nil {
			return nil, statusError(err)
		}
		return &output{Body: v.Report()}, nil
	})
}

// statusError is the HTTP answer to a refusal of shotaudiosvc; any other error is the server's.
func statusError(err error) error {
	var (
		invalid *shotaudiosvc.InvalidError
		locked  *shotaudiosvc.LockedError
	)
	switch {
	case errors.Is(err, shotaudiosvc.ErrNotFound):
		return huma.Error404NotFound("Shot または音声アセットが見つかりません")
	case errors.Is(err, shotaudiosvc.ErrBusy):
		return huma.Error409Conflict("この Shot は生成中か修正中です。終わってから音声を変えてください")
	case errors.As(err, &locked):
		names := make([]string, len(locked.Parameters))
		for i, p := range locked.Parameters {
			names[i] = p.Label()
		}
		return huma.NewError(http.StatusLocked, strings.Join(names, " / ")+" は LOCK されているため、音声を変えられません。LOCK を解除してから指示してください")
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	}
	return err
}
