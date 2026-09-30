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
	assert.Empty(t, lockedDimensions(spec, domain.WorldContext{}, nil, nil), "nothing is locked")

	got := lockedDimensions(spec, domain.WorldContext{}, domain.LockSet{
		{Target: domain.TargetCharacter, ID: "c1", Field: "identity"}:  {},
		{Target: domain.TargetProduct, ID: "p1", Field: "label"}:       {},
		{Target: domain.TargetProduct, ID: "other", Field: "geometry"}: {},
	}, nil)
	assert.Equal(t, map[domain.JudgeDimension]bool{
		domain.DimCharacterIdentity: true, domain.DimFace: true, domain.DimLogoTextAccuracy: true,
	}, got, "another product's lock does not apply")

	spec.Character.Identity, spec.Product.Geometry = "locked", "locked"
	got = lockedDimensions(spec, domain.WorldContext{}, nil, nil)
	assert.True(t, got[domain.DimCharacterIdentity])
	assert.True(t, got[domain.DimProductGeometry], "the spec's own 'locked' counts")
}

func TestLockedDimensions_AttributeLocksOfCharacterProductAndLocationMapToTheirDimensions(t *testing.T) {
	world := domain.WorldContext{
		Characters: []domain.WorldCharacter{{ID: "c1", Name: "Mio"}},
		Products:   []domain.WorldProduct{{ID: "p1", Name: "Serum"}},
		Locations:  []domain.WorldLocation{{ID: "l1", Name: "Street"}},
	}
	// The Shot Spec names its entities by name; the lock rows use their IDs.
	spec := domain.ShotSpec{
		Character: &domain.SpecActor{ID: "Mio"}, Product: &domain.SpecProduct{ID: "serum"}, Location: &domain.SpecLocation{ID: "Street"},
	}
	locks := domain.LockSet{
		{Target: domain.TargetCharacter, ID: "c1", Field: "wardrobe"}:        {},
		{Target: domain.TargetProduct, ID: "p1", Field: "orientation"}:       {},
		{Target: domain.TargetLocation, ID: "l1", Field: "lighting_state"}:   {},
		{Target: domain.TargetLocation, ID: "l1", Field: "layout_reference"}: {},
		{Target: domain.TargetCharacter, ID: "c1", Field: "voice"}:           {}, // nothing to see in a video
	}
	assert.Equal(t, map[domain.JudgeDimension]bool{
		domain.DimCharacterConsistency: true, domain.DimProductAccuracy: true, domain.DimLighting: true, domain.DimBackground: true,
	}, lockedDimensions(spec, world, locks, nil))

	other := domain.LockSet{{Target: domain.TargetLocation, ID: "elsewhere", Field: "weather"}: {}}
	assert.Empty(t, lockedDimensions(spec, world, other, nil), "a lock of a Location the Shot is not in does not apply")
}

func TestAssetPath_StaysInsideTheAssetsDirectory(t *testing.T) {
	r := &Runner{Deps: Deps{AssetsDir: "/data/assets"}}
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

func TestLockedDimensions_ParametersLockedOnTheShotReachTheJudge(t *testing.T) {
	locked := domain.LockSetting{Mode: domain.LockLocked}
	shotLocks := domain.ParameterLocks{
		domain.ParamCharacterIdentity: locked,
		domain.ParamProductGeometry:   locked,
		domain.ParamActing:            locked,
		domain.ParamCamera:            locked,
		domain.ParamLighting:          locked,
		domain.ParamDialogue:          locked, // has no Judge dimension
		domain.ParamPrompt:            {Mode: domain.LockManual},
	}

	got := lockedDimensions(domain.ShotSpec{}, domain.WorldContext{}, nil, shotLocks)

	assert.Equal(t, map[domain.JudgeDimension]bool{
		domain.DimCharacterIdentity: true, domain.DimFace: true, domain.DimProductGeometry: true,
		domain.DimActing: true, domain.DimCameraMotion: true, domain.DimLighting: true,
	}, got)
	assert.Empty(t, lockedDimensions(domain.ShotSpec{}, domain.WorldContext{}, nil, domain.ParameterLocks{domain.ParamActing: {Mode: domain.LockManual}}), "only LOCK counts, not MANUAL")
}
