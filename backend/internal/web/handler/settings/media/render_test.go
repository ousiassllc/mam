package media_test

import (
	"bytes"
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/media"
	organism "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
)

// section is the category as the screen draws it for a setting whose API keys are registered for the vendors given: the judgement of a model follows the key of
// its vendor, as the app's follows the key store (no real key is read).
func section(t *testing.T, registered ...modeladapter.Vendor) string {
	t.Helper()
	svc := modelsettings.New(filepath.Join(t.TempDir(), "data", "models.json"), false)
	svc.SetAvailability(func(id string) (modelfilter.Availability, bool) {
		for _, v := range registered {
			if modeladapter.VendorOf(domain.ModelName(id)) == v {
				return modelfilter.Usable(), true
			}
		}
		return modelfilter.Unusable(modelfilter.ReasonNoKey, "API キーが登録されていません（設定の「API キー」）", "/settings/api-keys"), true
	})
	v, err := svc.View()
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, organism.Section(media.Data(v)).Render(context.Background(), &buf))
	return buf.String()
}

// rowOf is the row of the table that carries the attribute (data-purpose="…" or data-policy="…").
func rowOf(t *testing.T, html, attr string) string {
	t.Helper()
	row := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(attr) + `.*?</tr>`).FindString(html)
	require.NotEmpty(t, row, attr)
	return row
}

// #1157, no key at all: one banner (not a sentence for each place), the one button to the API keys, a line for each missing key, a chip beside each model, and no
// colour of a state in the section.
func TestSection_NoKeyIsOneBannerOneButtonAndAChipBesideEachModel(t *testing.T) {
	html := section(t)

	assert.Equal(t, 1, strings.Count(html, `data-testid="model-assignment-key-banner"`))
	assert.Contains(t, html, "API キーが足りないため、用途・Quality Policy 7 件のうち 7 件で生成できません", "4 purposes and 3 policies: all 7 rows, every one with neither of its models usable")
	assert.NotContains(t, html, "代替モデルで動いています", "no row runs on its alternative: the sentence about them is not said")
	assert.Equal(t, 1, strings.Count(html, "API キーを設定"), "one button")
	assert.Contains(t, html, `href="/settings/api-keys"`)
	assert.NotContains(t, html, "設定を開く", "the link is not repeated for each line")
	assert.NotContains(t, html, "を指していますが", "no sentence for each place")
	assert.NotContains(t, html, "text-warning", "the section does not speak in the colour of a state")
	assert.Equal(t, 6, strings.Count(html, `data-testid="model-assignment-key-shortage"`), "a line for each key the default assignment needs: Google, OpenAI, Black Forest Labs, Kling, Seedance and Runway")
	assert.Contains(t, html, "のキーを入れると、")
	assert.Contains(t, html, "が使えるようになります")
	assert.Equal(t, 14, strings.Count(html, `data-testid="model-chip"`), "a chip beside every model of the two tables")
	assert.Contains(t, html, `title="API キーが登録されていません（設定の「API キー」）"`, "the full reason is the tooltip of the chip")
	assert.Contains(t, html, "キー未登録")
}

// #1157, only some keys: the banner counts and lists what is still missing, and a route whose alternative runs says so ("代替で動きます") instead of being warned about.
func TestSection_SomeKeysLeftNamesTheMissingOnesAndSeparatesWhatRunsOnTheAlternative(t *testing.T) {
	html := section(t, modeladapter.VendorOpenAI)

	banner := regexp.MustCompile(`(?s)data-testid="model-assignment-key-banner".*?</section>`).FindString(html)
	require.NotEmpty(t, banner)
	assert.NotContains(t, banner, "OpenAI", "the key that is there is not asked for")
	assert.Contains(t, banner, "Google")
	assert.Contains(t, banner, "API キーが足りないため、用途・Quality Policy 7 件のうち 4 件で生成できません。1 件は代替モデルで動いています", "edit_reference runs on its alternative (OpenAI); preview, FAST, BALANCED and MAX QUALITY have no model with a key")
	assert.NotRegexp(t, `\d 件が使えるようになります|代替で動いています`, banner, "a line of a key does not count places, and does not say what runs on an alternative")

	edit := rowOf(t, html, `data-purpose="edit_reference"`)
	assert.Contains(t, edit, `data-kind="fallback"`)
	assert.Contains(t, edit, "代替で動きます")
	assert.Equal(t, 1, strings.Count(edit, `data-testid="model-chip"`), "only the model without its key has a chip")
	fresh := rowOf(t, html, `data-purpose="new_reference"`)
	assert.NotContains(t, fresh, "model-assignment-row-note", "its primary (OpenAI) runs: only the chip of its alternative (Google) says the key is missing")
	assert.Equal(t, 1, strings.Count(fresh, `data-testid="model-chip"`))
}

// #1157: with every key registered the banner is gone and the table stays, quiet.
func TestSection_AllKeysRegisteredLeavesOnlyTheTables(t *testing.T) {
	html := section(t, modeladapter.Vendors()...)

	assert.NotContains(t, html, "model-assignment-key-banner")
	assert.NotContains(t, html, "API キーを設定")
	assert.NotContains(t, html, `data-testid="model-chip"`)
	assert.NotContains(t, html, "model-assignment-row-note")
	assert.Equal(t, len(modeladapter.ImagePurposes()), strings.Count(html, `data-testid="model-assignment-image"`))
	assert.Equal(t, len(domain.QualityPolicies()), strings.Count(html, `data-testid="model-assignment-policy"`))
}

// #1157: when no row is blocked and some run on their alternative, the banner only says so (and still offers the key that is missing).
func TestSection_OnlyRowsOnTheirAlternativeSaysSomeRunOnIt(t *testing.T) {
	var registered []modeladapter.Vendor
	for _, v := range modeladapter.Vendors() {
		if v != modeladapter.VendorKling {
			registered = append(registered, v)
		}
	}

	html := section(t, registered...)

	banner := regexp.MustCompile(`(?s)data-testid="model-assignment-key-banner-title">(.*?)</p>`).FindStringSubmatch(html)
	require.NotNil(t, banner)
	assert.Equal(t, "一部は代替モデルで動いています", banner[1], "FAST has Kling as its primary and Seedance, which runs, as its alternative")
	assert.Contains(t, html, "Kling")
	assert.Contains(t, html, "API キーを設定")
}

// #1157: the header of the number of Takes stays on one line and its column is as wide as its content.
func TestSection_TakesHeaderDoesNotWrap(t *testing.T) {
	html := section(t, modeladapter.Vendors()...)

	th := regexp.MustCompile(`<th[^>]*>\s*テイク数\s*</th>`).FindString(html)
	require.NotEmpty(t, th)
	assert.Contains(t, th, "whitespace-nowrap")
	assert.Contains(t, th, "w-px")
}
