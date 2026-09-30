package router

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func isNotFound(err error) bool { return errors.Is(err, service.ErrNotFound) }

// registerJudgeAPI registers GET /api/v1/takes/{id}/judge-result: the newest AI Video Judge result of a Take.
func registerJudgeAPI(api huma.API, services *service.Services) {
	type output struct {
		Body domain.JudgeResult
	}
	huma.Register(api, huma.Operation{
		OperationID: "get-take-judge-result",
		Method:      "GET",
		Path:        APIPrefix + "/takes/{id}/judge-result",
		Summary:     "Latest AI Video Judge result of a Take",
		Tags:        []string{"judge"},
		Errors:      []int{404},
	}, func(ctx context.Context, in *struct {
		ID string `path:"id" doc:"Take ID"`
	}) (*output, error) {
		res, err := services.Judge.LatestResult(ctx, in.ID)
		if isNotFound(err) {
			return nil, huma.Error404NotFound("この Take にはまだ Judge 結果がありません")
		}
		if err != nil {
			return nil, err
		}
		return &output{Body: res}, nil
	})
}
