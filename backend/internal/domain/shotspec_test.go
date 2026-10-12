package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func validSpec() domain.ShotSpec {
	return domain.ShotSpec{
		ShotObjective: "Character notices something behind her and starts running",
		Acting:        []domain.ActingBeat{{Time: "0.0-0.7", Action: "subtle reaction"}, {Time: "0.7-4.0", Action: "begin running"}},
		Camera:        domain.SpecCamera{Framing: "medium"},
	}
}

func TestShotSpec_ValidateAcceptsAnEnglishSpecInsideTheShot(t *testing.T) {
	assert.NoError(t, validSpec().Validate(4))
}

func TestShotSpec_ValidateRejectsWhatCannotDriveAGeneration(t *testing.T) {
	cases := map[string]func(*domain.ShotSpec){
		"no objective":        func(s *domain.ShotSpec) { s.ShotObjective = " " },
		"no framing":          func(s *domain.ShotSpec) { s.Camera.Framing = "" },
		"beat beyond the end": func(s *domain.ShotSpec) { s.Acting[1].Time = "0.7-6.0" },
		"beat overlaps":       func(s *domain.ShotSpec) { s.Acting[1].Time = "0.3-4.0" },
		"beat is not a range": func(s *domain.ShotSpec) { s.Acting[0].Time = "soon" },
		"beat starts at NaN":  func(s *domain.ShotSpec) { s.Acting[0].Time = "nan-0.7" },
		"beat ends at NaN":    func(s *domain.ShotSpec) { s.Acting[1].Time = "0.7-nan" },
		"beat ends at Inf":    func(s *domain.ShotSpec) { s.Acting[1].Time = "0.7-inf" },
		"empty action":        func(s *domain.ShotSpec) { s.Acting[0].Action = "" },
		"empty constraint":    func(s *domain.ShotSpec) { s.Constraints = []string{" "} },
		"Japanese":            func(s *domain.ShotSpec) { s.Style = "冷たい青" },
	}
	for name, mutate := range cases {
		s := validSpec()
		mutate(&s)
		assert.Error(t, s.Validate(4), name)
	}
}

func TestMergeShotSpec_ReplacesSpecKeysAndKeepsTheOtherWriters(t *testing.T) {
	stored := json.RawMessage(`{"storyboard":{"description":"window"},"references":[{"kind":"style"}],"shot_objective":"old","style":"old style"}`)
	merged, err := domain.MergeShotSpec(stored, validSpec())
	require.NoError(t, err)

	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(merged, &doc))
	assert.JSONEq(t, `{"description":"window"}`, string(doc["storyboard"]))
	assert.JSONEq(t, `[{"kind":"style"}]`, string(doc["references"]))
	assert.JSONEq(t, `"Character notices something behind her and starts running"`, string(doc["shot_objective"]))
	assert.NotContains(t, doc, "style", "a key the new spec does not have is dropped, not left stale")
}

func TestShot_CanonicalSpecIsPresentOnlyOnceAnObjectiveWasWritten(t *testing.T) {
	_, ok := domain.Shot{ShotSpec: json.RawMessage(`{"storyboard":{"description":"x"}}`)}.CanonicalSpec()
	assert.False(t, ok, "a storyboard alone is not a Canonical Shot Spec")

	merged, err := domain.MergeShotSpec(nil, validSpec())
	require.NoError(t, err)
	spec, ok := domain.Shot{ShotSpec: merged}.CanonicalSpec()
	require.True(t, ok)
	assert.Equal(t, validSpec().ShotObjective, spec.ShotObjective)
}

func TestReferences_RoundTripThroughTheShotSpec(t *testing.T) {
	refs := []domain.Reference{{Kind: domain.RefCharacter, Name: "Mio", AssetID: "a1", Prompt: "p", Model: domain.ModelImagen4}}
	raw, err := domain.WithReferences(json.RawMessage(`{"shot_objective":"x"}`), refs)
	require.NoError(t, err)
	assert.Equal(t, refs, domain.Shot{ShotSpec: raw}.References())
	assert.Empty(t, domain.Shot{ShotSpec: json.RawMessage(`{}`)}.References())
}

func TestParseTimeRange(t *testing.T) {
	from, to, err := domain.ParseTimeRange("0.7-1.3s")
	require.NoError(t, err)
	assert.InDelta(t, 0.7, from, 1e-9)
	assert.InDelta(t, 1.3, to, 1e-9)
	for _, bad := range []string{"", "1.3", "a-b", "-1-2", "nan-2", "1-nan", "NaN-NaN", "1-inf", "1-Infinity", "inf-2"} {
		_, _, err := domain.ParseTimeRange(bad)
		assert.Error(t, err, bad)
	}
}
