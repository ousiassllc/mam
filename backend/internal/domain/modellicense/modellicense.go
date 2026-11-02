// Package modellicense is the one vocabulary of "may this model be used commercially" (#928): the four answers, the
// filter that narrows a list by them, and the Info a screen, a result and an MCP tool carry for a model. It is a leaf
// (the standard library only) so that the model catalog, the Model Adapter's listings, the screens and the MCP tools agree
// on it without importing each other.
//
// The catalog of the local models (modelmgr/catalog) keeps its license data in its own JSON files and reduces it to an Info;
// the built-in cloud models are in data/cloud.json of this package. A model that follow-up issues add needs one entry in
// one of the two places, and the tests of both fail while it has none.
package modellicense

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Commercial is the answer to "may the model (and what it makes) be used commercially".
type Commercial string

// The answers. Unknown is a value of its own, not a "no": the license does not say, or says something mam could not confirm.
const (
	Yes         Commercial = "yes"         // 商用OK
	Conditional Commercial = "conditional" // 条件つき（Condition に条件を書く）
	No          Commercial = "no"          // 商用NG（非商用・研究用のライセンス）
	Unknown     Commercial = "unknown"     // 不明
)

// All lists the answers from the most to the least permissive: the order the screens and the filter show them in.
func All() []Commercial { return []Commercial{Yes, Conditional, Unknown, No} }

// Valid reports whether c is one of the four answers.
func (c Commercial) Valid() bool {
	switch c {
	case Yes, Conditional, No, Unknown:
		return true
	}
	return false
}

// Label is the short word of the badge.
func (c Commercial) Label() string {
	switch c {
	case Yes:
		return "商用OK"
	case Conditional:
		return "条件つき"
	case No:
		return "商用NG"
	}
	return "不明"
}

// strictness grows with what the user may not do. A known "no" is the strictest; "unknown" is stricter than a stated condition
// because nothing can be assumed of it.
func (c Commercial) strictness() int {
	switch c {
	case Yes:
		return 0
	case Conditional:
		return 1
	case No:
		return 3
	}
	return 2
}

// Strictest is the strictest of cs: the answer for a model made of parts with different licenses (a base model, a LoRA, a text
// encoder). Without any part it is Unknown, and a value that is not an answer counts as Unknown.
func Strictest(cs ...Commercial) Commercial {
	if len(cs) == 0 {
		return Unknown
	}
	out := Yes
	for _, c := range cs {
		if !c.Valid() {
			c = Unknown
		}
		if c.strictness() > out.strictness() {
			out = c
		}
	}
	return out
}

// Filter narrows a list of models by their answer.
type Filter string

// The filters. FilterAll is the empty filter.
const (
	FilterAll             Filter = "all"               // すべて
	FilterOK              Filter = "ok"                // 商用OKだけ
	FilterOKOrConditional Filter = "ok_or_conditional" // 商用OKと条件つき
)

// Filters lists the filters in the order the screens show them.
func Filters() []Filter { return []Filter{FilterAll, FilterOK, FilterOKOrConditional} }

// ParseFilter reads a filter; the empty string is FilterAll. The second result is false for a value that is none of them.
func ParseFilter(s string) (Filter, bool) {
	switch f := Filter(s); f {
	case "":
		return FilterAll, true
	case FilterAll, FilterOK, FilterOKOrConditional:
		return f, true
	}
	return FilterAll, false
}

// Label is the short word of the filter's choice.
func (f Filter) Label() string {
	switch f {
	case FilterOK:
		return "商用OKのみ"
	case FilterOKOrConditional:
		return "条件つきまで"
	}
	return "すべて"
}

// Allows reports whether a model with the answer c passes the filter.
func (f Filter) Allows(c Commercial) bool {
	switch f {
	case FilterOK:
		return c == Yes
	case FilterOKOrConditional:
		return c == Yes || c == Conditional
	}
	return true
}

// Info is what the screens and the MCP tools say of the license of one model.
type Info struct {
	Commercial Commercial `json:"commercial" jsonschema:"商用利用の可否: yes（商用OK）/ conditional（条件つき。condition に条件）/ no（商用NG）/ unknown（不明。notice に注意）"`
	// LicenseName is the name of the license ("Apache License 2.0", "OpenAI 利用規約").
	LicenseName string `json:"license_name" jsonschema:"ライセンス（利用規約）の名前"`
	// Condition says on what condition a conditional model may be used commercially ("" for the others).
	Condition string `json:"condition,omitempty" jsonschema:"条件つきのときの条件（年商の上限など）"`
	// SourceURL is where the license (or the terms) can be read: the original, not a summary.
	SourceURL string `json:"source_url,omitempty" jsonschema:"ライセンス原文（利用規約）の URL"`
	// CheckedAt is the date (YYYY-MM-DD) mam read the source.
	CheckedAt string `json:"checked_at,omitempty" jsonschema:"出典を確認した日（YYYY-MM-DD）"`
	// Notice is what to watch for; it is required for a model that is not Yes.
	Notice string `json:"notice,omitempty" jsonschema:"注意（商用NG・不明のときは必ず付く）"`
}

// UnknownInfo is the answer for a model nobody checked (a model the user defined on an endpoint, a file from Hugging Face
// whose card gives no license): Unknown with the notice that says so.
func UnknownInfo(notice string) Info {
	return Info{Commercial: Unknown, LicenseName: "ライセンス不明", Notice: notice}
}

// Check reports what is missing from an Info of the catalog data (a model that is shipped with mam): a valid answer, the name,
// an https source, a check date, the condition of a conditional answer and the notice of an answer that is not Yes.
func (i Info) Check() error {
	var errs []error
	if !i.Commercial.Valid() {
		errs = append(errs, fmt.Errorf("commercial %q must be yes, conditional, no or unknown", i.Commercial))
	}
	if strings.TrimSpace(i.LicenseName) == "" {
		errs = append(errs, errors.New("license_name is empty"))
	}
	if u, err := url.Parse(i.SourceURL); err != nil || u.Scheme != "https" || u.Host == "" {
		errs = append(errs, fmt.Errorf("source_url %q must be an https URL", i.SourceURL))
	}
	if _, err := time.Parse(time.DateOnly, i.CheckedAt); err != nil {
		errs = append(errs, fmt.Errorf("checked_at %q must be a date (YYYY-MM-DD)", i.CheckedAt))
	}
	if i.Commercial == Conditional && strings.TrimSpace(i.Condition) == "" {
		errs = append(errs, errors.New("a conditional answer needs its condition"))
	}
	if i.Commercial.Valid() && i.Commercial != Yes && strings.TrimSpace(i.Notice) == "" {
		errs = append(errs, fmt.Errorf("commercial %s needs a notice that says what to watch for", i.Commercial))
	}
	return errors.Join(errs...)
}
