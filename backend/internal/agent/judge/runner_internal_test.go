package judge

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestLockedDimensions_ComeFromTheSpecAndFromTheLockTable(t *testing.T) {
	spec := domain.ShotSpec{
		Character: &domain.SpecActor{ID: "c1"},
		Product:   &domain.SpecProduct{ID: "p1"},
	}
	assert.Empty(t, lockedDimensions(spec, nil), "nothing is locked")

	got := lockedDimensions(spec, []domain.LockKey{
		{TargetType: "character", TargetID: "c1", Parameter: "identity"},
		{TargetType: "product", TargetID: "p1", Parameter: "label"},
		{TargetType: "product", TargetID: "other", Parameter: "geometry"},
	})
	assert.Equal(t, map[domain.JudgeDimension]bool{
		domain.DimCharacterIdentity: true, domain.DimFace: true, domain.DimLogoTextAccuracy: true,
	}, got, "another product's lock does not apply")

	spec.Character.Identity, spec.Product.Geometry = "locked", "locked"
	got = lockedDimensions(spec, nil)
	assert.True(t, got[domain.DimCharacterIdentity])
	assert.True(t, got[domain.DimProductGeometry], "the spec's own 'locked' counts")
}

func TestAssetPath_StaysInsideTheAssetsDirectory(t *testing.T) {
	r := &Runner{Deps{AssetsDir: "/data/assets"}}
	p, err := r.assetPath("proj/a.mp4")
	assert.NoError(t, err)
	assert.Equal(t, "/data/assets/proj/a.mp4", p)
	for _, bad := range []string{"../etc/passwd", "proj/../../x", "/../x"} {
		_, err := r.assetPath(bad)
		assert.Error(t, err, bad)
	}
	_, err = (&Runner{}).assetPath("a")
	assert.Error(t, err, "no assets directory")
}

func TestSummary_SaysWhyTheNumbersAreMissing(t *testing.T) {
	res := domain.JudgeResult{Result: domain.JudgeFail, OverallConfidence: 0.9,
		Issues:  []domain.JudgeIssue{{Type: "face_identity_drift", Severity: domain.SeverityHigh, TimeRange: "1.0-2.0s", LockViolation: true}},
		Metrics: domain.JudgeMetrics{FaceIdentityStatus: domain.FaceUnavailable}}
	s := Summary(2, "0123456789", res)
	assert.Contains(t, s, "Shot 2 · Take 01234567")
	assert.Contains(t, s, "FAIL")
	assert.Contains(t, s, "[high] face_identity_drift 1.0-2.0s（LOCK 違反）")
	assert.Contains(t, s, "定性評価のみ")

	res.Metrics = domain.JudgeMetrics{FaceIdentityStatus: domain.FaceMeasured, FaceIdentity: []domain.FaceIdentityMetric{{Evaluated: false}}}
	assert.Contains(t, Summary(1, "x", res), "顔を検出できないフレームが多い")
}
