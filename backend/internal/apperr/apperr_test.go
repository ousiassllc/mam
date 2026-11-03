package apperr_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
)

func TestEveryCodeHasItsTexts(t *testing.T) {
	for _, c := range apperr.Codes() {
		for _, lang := range []apperr.Lang{apperr.LangJa, apperr.LangEn} {
			v := apperr.New(c).View(lang)
			assert.NotEqual(t, string(c)+".cause", v.Cause, "%s %s", c, lang)
			assert.NotEqual(t, string(c)+".remedy", v.Remedy, "%s %s", c, lang)
			assert.NotEmpty(t, v.Cause, c)
			assert.NotEmpty(t, v.Remedy, c)
			assert.NotEmpty(t, v.Class, c)
			assert.NotContains(t, v.Cause+v.Remedy, "{", "%s %s: an argument is not given a default", c, lang)
			for _, l := range v.Settings {
				assert.NotEmpty(t, l.Label)
				assert.True(t, strings.HasPrefix(l.Path, "/"))
			}
		}
	}
}

func TestBothLanguagesHaveTheSameKeys(t *testing.T) {
	for _, k := range apperr.Keys() {
		assert.NotEqual(t, k, apperr.Text(apperr.LangEn, k, nil), "the English text of %s is missing", k)
	}
	assert.Equal(t, "nope", apperr.Text(apperr.LangJa, "nope", nil), "an unknown key shows as itself")
}

func TestViewFillsTheArgumentsAndLinks(t *testing.T) {
	v := apperr.New(apperr.CodeInputMissing).With("param", "first_frame").WithDetail("the workflow needs first_frame (12)").View(apperr.LangJa)
	assert.Contains(t, v.Cause, "（first_frame）")
	assert.Contains(t, v.Remedy, "first_frame に使う画像")
	assert.Equal(t, "the workflow needs first_frame (12)", v.Detail)
	assert.Equal(t, apperr.ClassValidation, v.Class)

	k := apperr.New(apperr.CodeKeyMissing).View(apperr.LangEn)
	require.Len(t, k.Settings, 1)
	assert.Equal(t, "/settings/api-keys", k.Settings[0].Path)
	assert.Contains(t, k.Settings[0].Label, "API keys")
}

func TestAFixedCauseKeepsItsSentenceAndTheCatalogAddsTheRemedy(t *testing.T) {
	v := apperr.New(apperr.CodeConflict).WithCause("この Shot はすでに生成中です。").View(apperr.LangJa)
	assert.Equal(t, "この Shot はすでに生成中です。", v.Cause)
	assert.NotEmpty(t, v.Remedy)
	assert.Equal(t, v.Cause+v.Remedy, v.Sentence(apperr.LangJa))
}

func TestRecordRoundTrip(t *testing.T) {
	e := apperr.New(apperr.CodeVRAM).With("x", "y").WithDetail("CUDA out of memory")
	e.ID = "err_1"
	back, ok := apperr.Unmarshal(e.Marshal())
	require.True(t, ok)
	assert.Equal(t, e.Code, back.Code)
	assert.Equal(t, "CUDA out of memory", back.Detail)
	assert.Equal(t, "err_1", back.ID)

	for _, bad := range []string{"", "plain text", `{"code":"no_such_code"}`} {
		_, ok := apperr.Unmarshal(bad)
		assert.False(t, ok, bad)
	}
}

func TestWrapKeepsTheFailureBehindAndItsText(t *testing.T) {
	cause := errors.New("boom")
	e := apperr.New(apperr.CodeUnknown).Wrap(cause)
	assert.ErrorIs(t, e, cause)
	assert.Equal(t, "boom", e.Detail)
	got, ok := apperr.As(errors.Join(errors.New("x"), e))
	require.True(t, ok)
	assert.Same(t, e, got)
}

func TestParseLang(t *testing.T) {
	assert.Equal(t, apperr.LangEn, apperr.ParseLang("en-US,en;q=0.9"))
	assert.Equal(t, apperr.LangEn, apperr.ParseLang("en"))
	assert.Equal(t, apperr.LangJa, apperr.ParseLang("ja,en;q=0.8"))
	assert.Equal(t, apperr.LangJa, apperr.ParseLang(""))
	assert.Equal(t, apperr.LangJa, apperr.ParseLang("fr"))
}
