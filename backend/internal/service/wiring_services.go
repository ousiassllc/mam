package service

import (
	"time"

	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/bundle"
	"github.com/ousiassllc/mam/backend/internal/repository/review"
	"github.com/ousiassllc/mam/backend/internal/service/mainwait"
)

// services assembles the DI root from what was wired.
func (w *wiring) services() *Services {
	c, d := w.c, w.deps
	return &Services{
		Health:      NewHealthService(d.DB),
		Credentials: NewCredentialService(d.Credentials),
		Projects:    &ProjectService{c},
		Chat:        w.chat,
		Plans:       &PlanService{c, mainwait.Reader{Plans: c.plans, References: c.references, World: w.r.world, Gate: w.gate}},
		Generation:  w.generations,
		World:       w.worlds,
		Director:    w.director,
		Policy:      w.policyService(),
		Judge:       w.judges,
		Repair:      w.repairs,
		Continue:    w.continues,
		Reference:   &ReferenceService{c},
		Trace:       &TraceService{traces: repository.NewTraceRepository(d.DB)},
		Versions:    &VersionService{core: c, versions: w.r.versions, director: w.director, world: w.worlds},
		Bundle:      &BundleService{core: c, bundles: bundle.New(d.DB), freeBytes: d.FreeBytes},
		Series:      &SeriesService{core: c, series: w.r.series},
		Takes:       w.takes,
		Notes:       w.notes,
		Branch:      w.branch,
		Editor:      w.editors,
		Brand:       &BrandService{core: c, brands: w.r.brands},
		Review:      &ReviewService{core: c, reviews: review.New(d.DB), editors: w.editors.editors, chatSvc: w.chat, now: time.Now},
		Analytics:   &AnalyticsService{c},
		VoiceRef:    w.voiceRef,
	}
}
