package router

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// registerRepairAPI registers GET /api/v1/repair/jev-shadow-report: the figures of the Jev shadow
// runs (agreement with the Repair Agent, confidence distribution, latency, cost) that the decision
// to switch on Jev's selection stage is made from (spec/architecture/model-adapters.md §4.5).
func registerRepairAPI(api huma.API, services *service.Services) {
	type output struct {
		Body domain.ShadowReport
	}
	huma.Register(api, huma.Operation{
		OperationID: "get-jev-shadow-report",
		Method:      "GET",
		Path:        APIPrefix + "/repair/jev-shadow-report",
		Summary:     "Report of the Jev shadow runs of the Repair Agent",
		Tags:        []string{"repair"},
	}, func(ctx context.Context, _ *struct{}) (*output, error) {
		rep, err := services.Repair.ShadowReport(ctx)
		if err != nil {
			return nil, err
		}
		return &output{Body: rep}, nil
	})
}
