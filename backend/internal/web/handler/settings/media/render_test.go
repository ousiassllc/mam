package media_test

import (
	"bytes"
	"context"
	"os"
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
	return sectionWith(t, false, registered...)
}

// sectionWith is section for a settings file whose prefer_openai is preferOpenAI.
func sectionWith(t *testing.T, preferOpenAI bool, registered ...modeladapter.Vendor) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data", "models.json")
	if preferOpenAI {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(`{"prefer_openai": true}`), 0o600))
	}
	svc := modelsettings.New(path, false)
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
	assert.Contains(t, html, "API キーが足りないため、用途・Quality Policy 5 件のうち 5 件で生成できません", "2 purposes and 3 policies: preview (no feature asks for it) and the OpenAI-unified one (prefer_openai is off) are not counted")
	assert.NotContains(t, html, "代替モデルで動いています", "no row runs on its alternative: the sentence about them is not said")
	assert.Equal(t, 1, strings.Count(html, "API キーを設定"), "one button")
	assert.Contains(t, html, `href="/settings/api-keys"`)
	assert.NotContains(t, html, "設定を開く", "the link is not repeated for each line")
	assert.NotContains(t, html, "を指していますが", "no sentence for each place")
	assert.NotContains(t, html, "text-warning", "the section does not speak in the colour of a state")
	assert.Equal(t, 5, strings.Count(html, `data-testid="model-assignment-key-shortage"`), "a line for each key the counted rows need: Google, OpenAI, Kling, Seedance and Runway")
	assert.NotContains(t, regexp.MustCompile(`(?s)data-testid="model-assignment-key-shortages".*?</ul>`).FindString(html), "Black Forest Labs", "FLUX.2 is only the model of the preview purpose, which is not counted")
	assert.Contains(t, html, "のキーを入れると、")
	assert.Contains(t, html, "が使えるようになります")
	assert.Equal(t, 14, strings.Count(html, `data-testid="model-chip"`), "a chip under every model of the two tables, the rows that are not counted too")
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
	assert.Contains(t, banner, "API キーが足りないため、用途・Quality Policy 5 件のうち 3 件で生成できません。1 件は代替モデルで動いています", "edit_reference runs on its alternative (OpenAI); FAST, BALANCED and MAX QUALITY have no model with a key; preview is not counted")
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

func bannerTitleOf(t *testing.T, html string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)data-testid="model-assignment-key-banner-title">(.*?)</p>`).FindStringSubmatch(html)
	require.NotNil(t, m)
	return m[1]
}

func shortagesOf(html string) string {
	return regexp.MustCompile(`(?s)data-testid="model-assignment-key-shortages".*?</ul>`).FindString(html)
}

// #1157, some keys (OpenAI and Kling): no counted row is blocked, so the banner only says some run on an alternative, and the key that only the preview purpose
// needs (Black Forest Labs, FLUX.2) has no line.
func TestSection_OpenAIAndKlingLeaveNoRowThatCannotRun(t *testing.T) {
	html := section(t, modeladapter.VendorOpenAI, modeladapter.VendorKling)

	assert.Equal(t, "一部は代替モデルで動いています", bannerTitleOf(t, html))
	lines := shortagesOf(html)
	assert.NotContains(t, lines, "Black Forest Labs", "the key of a purpose that is not counted is not asked for")
	assert.NotContains(t, lines, "OpenAI")
	assert.NotContains(t, lines, "Kling")
	assert.Contains(t, lines, "Google")
	preview := rowOf(t, html, `data-purpose="preview"`)
	assert.Contains(t, preview, "model-chip", "the table still shows the preview row, and its chips")
}

// #1157: the preview purpose is asked for by no feature, so a key that only it lacks changes no count and puts up no banner.
func TestSection_PreviewIsNeverCounted(t *testing.T) {
	var registered []modeladapter.Vendor
	for _, v := range modeladapter.Vendors() {
		if v != modeladapter.VendorFlux {
			registered = append(registered, v)
		}
	}

	html := section(t, registered...)

	assert.NotContains(t, html, "model-assignment-key-banner", "FLUX.2 only serves preview, whose other model is Google's")
}

// #1157: the OpenAI-unified purpose is counted only while prefer_openai is on.
func TestSection_OpenAIUnifiedIsCountedOnlyWithPreferOpenAI(t *testing.T) {
	var registered []modeladapter.Vendor
	for _, v := range modeladapter.Vendors() {
		if v != modeladapter.VendorOpenAI && v != modeladapter.VendorGoogle {
			registered = append(registered, v)
		}
	}

	off := sectionWith(t, false, registered...)
	on := sectionWith(t, true, registered...)

	assert.Contains(t, bannerTitleOf(t, off), "用途・Quality Policy 5 件のうち")
	assert.Contains(t, bannerTitleOf(t, on), "用途・Quality Policy 6 件のうち")
	assert.Equal(t, strings.Count(off, `data-testid="model-assignment-image"`), strings.Count(on, `data-testid="model-assignment-image"`))
}

// #1157: the name of a model is never broken, and the chip is on the line under it in the column of the primary model as in the one of the alternative.
func TestSection_ModelNameIsOneLineAndTheChipIsUnderIt(t *testing.T) {
	html := section(t)

	for _, row := range []string{rowOf(t, html, `data-purpose="new_reference"`), rowOf(t, html, `data-policy="fast"`)} {
		cells := regexp.MustCompile(`(?s)<td[^>]*min-w-36[^>]*>.*?</td>`).FindAllString(row, -1)
		require.Len(t, cells, 2, "the primary and the alternative")
		for _, c := range cells {
			assert.Regexp(t, `<span class="whitespace-nowrap">[^<]+</span>`, c, "the name is on one line")
			assert.Regexp(t, `(?s)</span>\s*(<a [^>]*)?<span class="mt-1 block w-fit">\s*<span`, c, "the chip is a block of its own after the name")
		}
	}
}
