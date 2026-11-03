package apperr

import (
	"regexp"
	"strings"
)

var (
	placeholder = regexp.MustCompile(`\{[a-z_]+\}`)
	token       = regexp.MustCompile(`\{\{[a-z_.]+\}\}`)
)

// Lang is a language of the texts. Japanese is the base: every text is written in it first, and a text that another language
// lacks is read in Japanese.
type Lang string

// The languages.
const (
	LangJa Lang = "ja"
	LangEn Lang = "en"
)

// ParseLang is the language of a tag or of an Accept-Language header ("en-US,en;q=0.9"): English for an English tag first, Japanese for
// anything else (including "").
func ParseLang(tag string) Lang {
	first, _, _ := strings.Cut(tag, ",")
	first, _, _ = strings.Cut(first, ";")
	first = strings.ToLower(strings.TrimSpace(first))
	if first == "en" || strings.HasPrefix(first, "en-") || strings.HasPrefix(first, "en_") {
		return LangEn
	}
	return LangJa
}

// catalogs is the dictionary: language, then key ("<code>.cause", "<code>.remedy", "place.<place>", "ui.<name>") to text.
// The texts hold {name} for an argument. messages_ja.go and messages_en.go fill it.
var catalogs = map[Lang]map[string]string{LangJa: messagesJa, LangEn: messagesEn}

// Text is the text of key in lang, built from args. {name} is the value of the argument name (nothing when it is not given); {{key}}
// is the text of another key, built from the same args (so that a sentence a code shares, or one that appears only when an argument
// is given, is written once, and an argument can name a text instead of carrying words). A key that lang lacks is read in Japanese; a
// key that no language has is the key itself (a missing text shows as its key, which a test catches, instead of as nothing).
func Text(lang Lang, key string, args map[string]string) string {
	text, ok := lookup(lang, key)
	if !ok {
		return key
	}
	return strings.TrimSpace(build(lang, text, args, 0))
}

func lookup(lang Lang, key string) (string, bool) {
	if text, ok := catalogs[lang][key]; ok {
		return text, true
	}
	text, ok := catalogs[LangJa][key]
	return text, ok
}

// maxDepth bounds {{key}} inside {{key}}: a text that refers to itself ends there instead of never.
const maxDepth = 4

func build(lang Lang, text string, args map[string]string, depth int) string {
	text = expand(lang, text, args, depth)
	if strings.Contains(text, "{") {
		for name, value := range args {
			text = strings.ReplaceAll(text, "{"+name+"}", expand(lang, value, args, depth))
		}
		text = placeholder.ReplaceAllString(text, "")
	}
	return text
}

// expand replaces the {{key}} of text.
func expand(lang Lang, text string, args map[string]string, depth int) string {
	if depth >= maxDepth || !strings.Contains(text, "{{") {
		return text
	}
	return token.ReplaceAllStringFunc(text, func(m string) string {
		inner, ok := lookup(lang, m[2:len(m)-2])
		if !ok {
			return ""
		}
		return build(lang, inner, args, depth+1)
	})
}

// Keys are the keys of the Japanese texts, sorted by the caller if it needs an order: the keys every language has to have.
func Keys() []string {
	out := make([]string, 0, len(messagesJa))
	for k := range messagesJa {
		out = append(out, k)
	}
	return out
}

// separator is what joins the cause and the remedy in one text: Japanese sentences touch, English ones have a space.
func separator(lang Lang) string {
	if lang == LangEn {
		return " "
	}
	return ""
}
