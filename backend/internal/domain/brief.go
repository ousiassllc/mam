package domain

import (
	"cmp"
	"slices"
	"strings"
)

// BriefField identifies one Creative Brief item (spec/spec.md §4.2).
type BriefField string

// Creative Brief fields. The JSON keys of Brief use the same names.
const (
	BriefContentType BriefField = "content_type"
	BriefObjective   BriefField = "objective"
	BriefSubject     BriefField = "subject"
	BriefTarget      BriefField = "target"
	BriefDuration    BriefField = "duration"
	BriefAspectRatio BriefField = "aspect_ratio"
	BriefMood        BriefField = "mood"
)

// RequiredBriefFields lists the items the AI asks about, in question order
// (spec/spec.md §4.2). Assets, References and Additional Instruction are optional.
func RequiredBriefFields() []BriefField {
	return []BriefField{BriefContentType, BriefObjective, BriefSubject, BriefTarget, BriefDuration, BriefAspectRatio, BriefMood}
}

var briefLabels = map[BriefField]string{
	BriefContentType: "Content Type", BriefObjective: "Objective", BriefSubject: "Subject", BriefTarget: "Target",
	BriefDuration: "Duration", BriefAspectRatio: "Aspect Ratio", BriefMood: "Mood",
}

// Label is the display name of the field.
func (f BriefField) Label() string { return cmp.Or(briefLabels[f], string(f)) }

// Question is the sentence the AI asks when the field is missing.
func (f BriefField) Question() string {
	switch f {
	case BriefContentType:
		return "確認です。どの種類の動画を作りますか？"
	case BriefObjective:
		return "確認です。動画の目的（掲載先・用途）は何ですか？"
	case BriefSubject:
		return "確認です。何（商品・題材）を主役にしますか？"
	case BriefTarget:
		return "確認です。ターゲットは誰ですか？"
	case BriefDuration:
		return "確認です。動画の長さはどれくらいにしますか？"
	case BriefAspectRatio:
		return "確認です。アスペクト比はどれにしますか？"
	case BriefMood:
		return "確認です。どんな雰囲気にしますか？"
	}
	return "確認です。" + f.Label() + " を教えてください。"
}

// Options are the quick-answer buttons of the Question card. Fields without a
// sensible closed set (Subject) return nil and are answered by free input.
func (f BriefField) Options() []string {
	switch f {
	case BriefContentType:
		return []string{"商品CM", "ドラマ", "アニメ", "SNS/ショート動画"}
	case BriefObjective:
		return []string{"Instagram広告", "YouTube広告", "TikTok", "ブランドサイト"}
	case BriefTarget:
		return []string{"20代女性", "30〜40代女性", "20〜30代男性", "ファミリー層"}
	case BriefDuration:
		return []string{"6秒", "15秒", "30秒", "60秒"}
	case BriefAspectRatio:
		return BriefAspectRatios()
	case BriefMood:
		return []string{"高級・ミニマル", "ポップ・明るい", "クール・スタイリッシュ", "ナチュラル・温かい"}
	}
	return nil
}

// DelegateOption is the button label that leaves a field to the AI.
const DelegateOption = "AIに任せる"

// Aspect ratios a Brief can hold; DefaultAspectRatio stands in for a missing or unsupported one.
const (
	AspectRatio16x9    = "16:9"
	AspectRatio9x16    = "9:16"
	AspectRatio1x1     = "1:1"
	DefaultAspectRatio = AspectRatio16x9
)

// BriefAspectRatios lists the aspect ratios a Brief can hold (and generation accepts), in quick-answer order.
func BriefAspectRatios() []string { return []string{AspectRatio9x16, AspectRatio16x9, AspectRatio1x1} }

// IsBriefAspectRatio reports whether r is one of BriefAspectRatios.
func IsBriefAspectRatio(r string) bool { return slices.Contains(BriefAspectRatios(), r) }

// Brief is the Creative Brief stored in brief.content as JSON (F-01, F-25).
type Brief struct {
	ContentType           string `json:"content_type,omitempty"`
	Objective             string `json:"objective,omitempty"`
	Subject               string `json:"subject,omitempty"`
	Target                string `json:"target,omitempty"`
	Duration              string `json:"duration,omitempty"`
	AspectRatio           string `json:"aspect_ratio,omitempty"`
	Mood                  string `json:"mood,omitempty"`
	AdditionalInstruction string `json:"additional_instruction,omitempty"`
	// Style is the canonical Style of the Series an Episode inherits (F-29); it is not a required field.
	Style      string   `json:"style,omitempty"`
	Assets     []string `json:"assets,omitempty"`     // image asset IDs
	References []string `json:"references,omitempty"` // URLs or video asset IDs
	Delegated  []string `json:"delegated,omitempty"`  // fields the user left to the AI
}

// Get returns the value of a required field ("" when unset).
func (b Brief) Get(f BriefField) string {
	switch f {
	case BriefContentType:
		return b.ContentType
	case BriefObjective:
		return b.Objective
	case BriefSubject:
		return b.Subject
	case BriefTarget:
		return b.Target
	case BriefDuration:
		return b.Duration
	case BriefAspectRatio:
		return b.AspectRatio
	case BriefMood:
		return b.Mood
	}
	return ""
}

// Set stores a value (trimmed) and clears any earlier delegation of the field.
func (b *Brief) Set(f BriefField, value string) {
	value = strings.TrimSpace(value)
	switch f {
	case BriefContentType:
		b.ContentType = value
	case BriefObjective:
		b.Objective = value
	case BriefSubject:
		b.Subject = value
	case BriefTarget:
		b.Target = value
	case BriefDuration:
		b.Duration = value
	case BriefAspectRatio:
		b.AspectRatio = value
	case BriefMood:
		b.Mood = value
	}
	if value != "" {
		b.Delegated = slices.DeleteFunc(b.Delegated, func(d string) bool { return d == string(f) })
	}
}

// Delegate marks the field as left to the AI ("AIに任せる").
func (b *Brief) Delegate(f BriefField) {
	if !b.IsDelegated(f) {
		b.Delegated = append(b.Delegated, string(f))
	}
}

// IsDelegated reports whether the user left the field to the AI.
func (b Brief) IsDelegated(f BriefField) bool {
	return slices.Contains(b.Delegated, string(f))
}

// Missing returns the required fields that are neither set nor delegated, in question order.
func (b Brief) Missing() []BriefField {
	var missing []BriefField
	for _, f := range RequiredBriefFields() {
		if b.Get(f) == "" && !b.IsDelegated(f) {
			missing = append(missing, f)
		}
	}
	return missing
}

// Merge copies every value set in extracted into b without overwriting fields
// the user already decided (set or delegated). Assets and References are appended
// without duplicates.
func (b *Brief) Merge(extracted Brief) {
	for _, f := range RequiredBriefFields() {
		if v := extracted.Get(f); v != "" && b.Get(f) == "" && !b.IsDelegated(f) {
			b.Set(f, v)
		}
	}
	if b.AdditionalInstruction == "" {
		b.AdditionalInstruction = extracted.AdditionalInstruction
	}
	b.Assets = appendUnique(b.Assets, extracted.Assets...)
	b.References = appendUnique(b.References, extracted.References...)
}

// AttachAssets takes uploaded images into Assets and videos into References (UC-1).
func (b *Brief) AttachAssets(assets []Asset) {
	for _, a := range assets {
		if a.Type == AssetImage {
			b.Assets = appendUnique(b.Assets, a.ID)
		} else {
			b.References = appendUnique(b.References, a.ID)
		}
	}
}

func appendUnique(dst []string, src ...string) []string {
	for _, s := range src {
		if !slices.Contains(dst, s) {
			dst = append(dst, s)
		}
	}
	return dst
}
