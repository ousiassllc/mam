package lorafile

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain/lorafamily"
)

// Metadata is what the __metadata__ of the header says. A field the file does not say is empty: nothing is guessed, and the screen
// shows "未設定" for it. TagCandidates are only a hint (the tags the training saw most often): they are never the trigger words.
type Metadata struct {
	// BaseModels are the families (lorafamily keys) the architecture / base model entries name; BaseRaw are those entries as written.
	BaseModels []string
	BaseRaw    []string
	Title      string
	// TriggerWords are the words the file itself names as its triggers (modelspec.trigger_phrase, trigger_words).
	TriggerWords  []string
	TagCandidates []string
	// License is the license the file names (modelspec.license); the answer to "commercial use?" is never read from it.
	License                  string
	NetworkDim, NetworkAlpha string
}

// Empty reports that the file says nothing of the kinds above.
func (m Metadata) Empty() bool {
	return len(m.BaseModels) == 0 && len(m.BaseRaw) == 0 && m.Title == "" && len(m.TriggerWords) == 0 && len(m.TagCandidates) == 0 &&
		m.License == "" && m.NetworkDim == "" && m.NetworkAlpha == ""
}

const (
	maxWords       = 30
	maxWordRunes   = 80
	maxMetaRunes   = 120
	maxCandidates  = 10
	maxTagFreqSize = 1 << 20
)

// ReadMetadata reads what the __metadata__ says. It reads only the keys named in the package comment and the spec; unknown keys are
// ignored, and every text is cut and stripped of control characters (the file is not trusted).
func ReadMetadata(meta map[string]string) Metadata {
	m := Metadata{
		BaseModels: lorafamily.FromMetadata(meta), Title: clean(meta["modelspec.title"]), License: clean(meta["modelspec.license"]),
		NetworkDim: shortNumber(meta["ss_network_dim"]), NetworkAlpha: shortNumber(meta["ss_network_alpha"]),
	}
	for _, k := range []string{"modelspec.architecture", "ss_base_model_version"} {
		if v := clean(meta[k]); v != "" && !slices.Contains(m.BaseRaw, v) {
			m.BaseRaw = append(m.BaseRaw, v)
		}
	}
	for _, k := range []string{"modelspec.trigger_phrase", "trigger_words", "trigger_word"} {
		if words, err := ParseWords(meta[k]); err == nil && len(words) > 0 {
			m.TriggerWords = words
			break
		}
	}
	m.TagCandidates = tagCandidates(meta["ss_tag_frequency"])
	return m
}

// tagCandidates are the tags the training saw most often, from ss_tag_frequency ({"<dataset>": {"<tag>": count}}): a hint of what the
// prompt may name, shown beside the trigger words field and never put in it.
func tagCandidates(raw string) []string {
	if raw == "" || len(raw) > maxTagFreqSize {
		return nil
	}
	var sets map[string]map[string]float64
	if json.Unmarshal([]byte(raw), &sets) != nil {
		return nil
	}
	count := map[string]float64{}
	for _, tags := range sets {
		for t, c := range tags {
			if t = strings.TrimSpace(t); t != "" && utf8.RuneCountInString(t) <= maxWordRunes && !strings.ContainsFunc(t, unicode.IsControl) {
				count[t] += c
			}
		}
	}
	tags := make([]string, 0, len(count))
	for t := range count {
		tags = append(tags, t)
	}
	sort.Slice(tags, func(i, j int) bool {
		if count[tags[i]] != count[tags[j]] {
			return count[tags[i]] > count[tags[j]]
		}
		return tags[i] < tags[j]
	})
	return tags[:min(len(tags), maxCandidates)]
}

// clean is s without control characters, trimmed and cut to a length a screen can show.
func clean(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s))
	return abbreviate(s, maxMetaRunes)
}

func abbreviate(s string, runes int) string {
	if utf8.RuneCountInString(s) <= runes {
		return s
	}
	return string([]rune(s)[:runes]) + "…"
}

// shortNumber is s when it is a short plain number ("32", "16.0") and "" otherwise.
func shortNumber(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 12 || strings.Trim(s, "0123456789.") != "" {
		return ""
	}
	return s
}

// ParseWords splits trigger words typed or stored as text: separated by commas (, ,), newlines or the Japanese comma; blanks are dropped
// and a word given twice is kept once. At most 30 words of 80 characters, without control characters (the words are put in a prompt by hand).
func ParseWords(s string) ([]string, error) {
	var out []string
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '、' || r == '，' || r == '\n' || r == '\r' }) {
		w = strings.TrimSpace(w)
		switch {
		case w == "" || slices.Contains(out, w):
		case utf8.RuneCountInString(w) > maxWordRunes:
			return nil, fmt.Errorf("トリガー語は 1 語 %d 文字以内にしてください", maxWordRunes)
		case strings.ContainsFunc(w, unicode.IsControl):
			return nil, errors.New("トリガー語に制御文字は使えません")
		default:
			out = append(out, w)
		}
	}
	if len(out) > maxWords {
		return nil, fmt.Errorf("トリガー語は %d 語までです", maxWords)
	}
	return out, nil
}

// MaxTriggerChars is the length of the trigger words as text that always holds the words ParseWords takes (a form's limit for the field).
const MaxTriggerChars = maxWords * (maxWordRunes + 1)

// MaxLicenseRunes bounds the license name the user may type.
const MaxLicenseRunes = 120
