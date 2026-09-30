package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func cs(items ...domain.BrandConstraint) domain.BrandConstraints { return items }

func forbidden(id string, level domain.EnforcementLevel, text string) domain.BrandConstraint {
	return domain.BrandConstraint{ID: id, Type: domain.BrandForbidden, Level: level, Text: text}
}

func TestBrandConstraint_ValidateAcceptsTheEightTypesAndRejectsTheRest(t *testing.T) {
	assert.Len(t, domain.BrandConstraintTypes(), 8)
	for _, typ := range domain.BrandConstraintTypes() {
		c := domain.BrandConstraint{Type: typ, Level: domain.EnforceAdvisory, Text: "x"}
		assert.Empty(t, c.Validate(), typ)
	}
	tests := []struct {
		name string
		c    domain.BrandConstraint
	}{
		{"unknown type", domain.BrandConstraint{Type: "tone", Level: domain.EnforceLock, Text: "x"}},
		{"unknown level", domain.BrandConstraint{Type: domain.BrandLogoRule, Level: "strict", Text: "x"}},
		{"blank text", domain.BrandConstraint{Type: domain.BrandLogoRule, Level: domain.EnforceLock, Text: " \n "}},
		{"too long", domain.BrandConstraint{Type: domain.BrandLogoRule, Level: domain.EnforceLock, Text: strings.Repeat("あ", domain.MaxBrandTextRunes+1)}},
	}
	for _, tc := range tests {
		assert.NotEmpty(t, tc.c.Validate(), tc.name)
	}
}

func TestBrandConstraints_ForbiddenInMatchesEveryLineCaseInsensitivelyAcrossWhitespace(t *testing.T) {
	list := cs(forbidden("a", domain.EnforceLock, "最安値\nNo.1 in  Japan\n\n"), forbidden("b", domain.EnforceAdvisory, "cheap"))

	got := list.ForbiddenIn("The CHEAP serum, no.1 in\njapan!")
	require.Len(t, got, 2)
	assert.Equal(t, []string{"No.1 in  Japan", "cheap"}, []string{got[0].Term, got[1].Term})
	assert.Empty(t, list.ForbiddenIn("A calm morning"))
	assert.Empty(t, cs(domain.BrandConstraint{Type: domain.BrandGuideline, Level: domain.EnforceLock, Text: "cheap"}).ForbiddenIn("cheap"), "only forbidden expressions are matched as words")
}

func TestBrandConstraints_CheckTextFailsOnlyForLockConstraints(t *testing.T) {
	list := cs(forbidden("lock", domain.EnforceLock, "最安値"), forbidden("adv", domain.EnforceAdvisory, "激安"))

	assert.NoError(t, list.CheckText("激安の商品です"), "an Advisory violation is a warning")
	err := list.CheckText("最安値！激安！")
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrBrandViolation))
	var bv *domain.BrandViolationError
	require.True(t, errors.As(err, &bv))
	require.Len(t, bv.Violations, 1, "only the LOCK violation is reported")
	assert.Equal(t, "lock", bv.Violations[0].Constraint.ID)
	assert.Contains(t, bv.Message(), "最安値")
}

func TestBrandConstraints_MissingDisclaimersLooksAtAllTexts(t *testing.T) {
	list := cs(
		domain.BrandConstraint{ID: "d1", Type: domain.BrandDisclaimer, Level: domain.EnforceLock, Text: "効果には個人差があります"},
		domain.BrandConstraint{ID: "d2", Type: domain.BrandDisclaimer, Level: domain.EnforceAdvisory, Text: "Results may vary"},
	)
	got := list.MissingDisclaimers("ナレーション", "テロップ: 効果には個人差があります")
	require.Len(t, got, 1)
	assert.Equal(t, "d2", got[0].Constraint.ID)
	assert.Empty(t, list.MissingDisclaimers("効果には個人差があります", "results MAY vary"))
	assert.Len(t, list.MissingDisclaimers(), 2)
}

func TestBrandConstraints_PromptSectionNamesEveryConstraintWithItsIDAndLevel(t *testing.T) {
	assert.Empty(t, domain.BrandConstraints(nil).PromptSection())
	got := cs(forbidden("id-1", domain.EnforceLock, "最安値\n業界No.1"),
		domain.BrandConstraint{ID: "id-2", Type: domain.BrandLogoRule, Level: domain.EnforceAdvisory, Text: "ロゴを\n歪めない"}).PromptSection()

	assert.Contains(t, got, `LOCKED (never violate)] id=id-1 Forbidden expressions`)
	assert.Contains(t, got, `"最安値", "業界No.1"`)
	assert.Contains(t, got, `ADVISORY (avoid where possible)] id=id-2 Logo rules: ロゴを 歪めない`)
}

func TestShotSpec_BrandTextCoversWhatTheShotShowsAndSays(t *testing.T) {
	spec := domain.ShotSpec{
		ShotObjective: "A woman holds the serum",
		Product:       &domain.SpecProduct{ID: "Serum", Label: "BEST PRICE"},
		Acting:        []domain.ActingBeat{{Time: "0.0-4.0", Action: "smiles at the camera"}},
		Constraints:   []string{"preserve_logo"},
	}
	got := spec.BrandText()
	for _, want := range []string{"holds the serum", "BEST PRICE", "smiles at the camera", "preserve_logo"} {
		assert.Contains(t, got, want)
	}
}
