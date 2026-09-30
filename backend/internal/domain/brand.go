package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// BrandConstraintType is brand_constraint.type: what kind of production rule the constraint is
// (F-23, spec/spec.md §26).
type BrandConstraintType string

// Brand constraint types.
const (
	BrandGuideline     BrandConstraintType = "brand_guideline"
	BrandForbidden     BrandConstraintType = "forbidden_expression"
	BrandDisclaimer    BrandConstraintType = "required_disclaimer"
	BrandProductClaim  BrandConstraintType = "product_claim"
	BrandLogoRule      BrandConstraintType = "logo_rule"
	BrandCharacterRule BrandConstraintType = "character_restriction"
	BrandAssetRights   BrandConstraintType = "asset_rights"
	BrandPlatformRule  BrandConstraintType = "platform_rule"
)

// MaxBrandTextRunes bounds the text of a constraint.
const MaxBrandTextRunes = 1000

// BrandConstraintTypes lists every type in the order of spec/spec.md §26.
func BrandConstraintTypes() []BrandConstraintType {
	return []BrandConstraintType{
		BrandGuideline, BrandForbidden, BrandDisclaimer, BrandProductClaim,
		BrandLogoRule, BrandCharacterRule, BrandAssetRights, BrandPlatformRule,
	}
}

// Valid reports whether t is a known type.
func (t BrandConstraintType) Valid() bool {
	for _, v := range BrandConstraintTypes() {
		if v == t {
			return true
		}
	}
	return false
}

// Label is the name shown to the user.
func (t BrandConstraintType) Label() string {
	switch t {
	case BrandGuideline:
		return "Brand Guideline"
	case BrandForbidden:
		return "禁止表現"
	case BrandDisclaimer:
		return "必須の免責表記"
	case BrandProductClaim:
		return "Product Claims"
	case BrandLogoRule:
		return "Logo Rules"
	case BrandCharacterRule:
		return "Character Restrictions"
	case BrandAssetRights:
		return "音楽・素材の権利"
	case BrandPlatformRule:
		return "Platform Rules"
	}
	return string(t)
}

// englishLabel is the name given to the models, which are prompted in English.
func (t BrandConstraintType) englishLabel() string {
	switch t {
	case BrandGuideline:
		return "Brand guideline"
	case BrandForbidden:
		return "Forbidden expressions"
	case BrandDisclaimer:
		return "Required disclaimer"
	case BrandProductClaim:
		return "Product claims"
	case BrandLogoRule:
		return "Logo rules"
	case BrandCharacterRule:
		return "Character restrictions"
	case BrandAssetRights:
		return "Music / asset rights"
	case BrandPlatformRule:
		return "Platform rules"
	}
	return string(t)
}

// EnforcementLevel is brand_constraint.enforcement_level.
type EnforcementLevel string

// Enforcement levels.
const (
	// EnforceLock: a violation fails the Take or the deliverable whatever its score is.
	EnforceLock EnforcementLevel = "lock"
	// EnforceAdvisory: a violation is a warning; it never fails anything by itself.
	EnforceAdvisory EnforcementLevel = "advisory"
)

// Valid reports whether l is a known level.
func (l EnforcementLevel) Valid() bool { return l == EnforceLock || l == EnforceAdvisory }

// Label is the name shown to the user.
func (l EnforcementLevel) Label() string {
	if l == EnforceAdvisory {
		return "Advisory（警告）"
	}
	return "LOCK（違反は不合格）"
}

// BrandConstraint is one row of brand_constraint. Text is brand_constraint.value's "text": the rule
// in the user's words. For forbidden expressions every non-empty line is one expression that must not
// appear; for a required disclaimer it is the wording that must appear.
type BrandConstraint struct {
	ID        string
	ProjectID string
	Type      BrandConstraintType
	Level     EnforcementLevel
	Text      string
	CreatedAt time.Time
}

// Validate reports why the constraint cannot be registered ("" when it can).
func (c BrandConstraint) Validate() string {
	switch {
	case !c.Type.Valid():
		return "種類が正しくありません"
	case !c.Level.Valid():
		return "強制レベルが正しくありません"
	case strings.TrimSpace(c.Text) == "":
		return "内容を入力してください"
	case utf8.RuneCountInString(c.Text) > MaxBrandTextRunes:
		return fmt.Sprintf("内容は %d 文字以内で入力してください", MaxBrandTextRunes)
	}
	return ""
}

// Terms are the forbidden expressions of a BrandForbidden constraint, one per line.
func (c BrandConstraint) Terms() []string {
	var out []string
	for _, line := range strings.Split(c.Text, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// BrandViolation is one constraint broken by a text: Term is the forbidden expression found ("" for
// a missing disclaimer).
type BrandViolation struct {
	Constraint BrandConstraint
	Term       string
}

// Locked reports whether the violation breaks a LOCK constraint.
func (v BrandViolation) Locked() bool { return v.Constraint.Level == EnforceLock }

// Message is the violation in the user's language.
func (v BrandViolation) Message() string {
	if v.Constraint.Type == BrandDisclaimer {
		return fmt.Sprintf("必須の免責表記「%s」が含まれていません", v.Constraint.Text)
	}
	return fmt.Sprintf("禁止表現「%s」が含まれています", v.Term)
}

// ErrBrandViolation means a Brand / Safety Constraint of the Project is broken (HTTP 403).
var ErrBrandViolation = errors.New("brand constraint violation")

// BrandViolationError is ErrBrandViolation with what was broken.
type BrandViolationError struct{ Violations []BrandViolation }

func (e *BrandViolationError) Error() string { return ErrBrandViolation.Error() + ": " + e.Message() }

// Is makes errors.Is(err, ErrBrandViolation) hold.
func (e *BrandViolationError) Is(target error) bool { return target == ErrBrandViolation }

// Message lists the violations in the user's language.
func (e *BrandViolationError) Message() string {
	msgs := make([]string, len(e.Violations))
	for i, v := range e.Violations {
		msgs[i] = v.Message()
	}
	return strings.Join(msgs, " / ")
}

// BrandConstraints is the set of constraints of a Project.
type BrandConstraints []BrandConstraint

// ByID returns the constraint with the ID.
func (cs BrandConstraints) ByID(id string) (BrandConstraint, bool) {
	for _, c := range cs {
		if c.ID == id {
			return c, true
		}
	}
	return BrandConstraint{}, false
}

// normalizeText makes texts comparable: lower case, runs of white space collapsed.
func normalizeText(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// ForbiddenIn returns every forbidden expression (of any level) that the text contains. The match is
// a case-insensitive substring match.
func (cs BrandConstraints) ForbiddenIn(text string) []BrandViolation {
	hay := normalizeText(text)
	var out []BrandViolation
	for _, c := range cs {
		if c.Type != BrandForbidden {
			continue
		}
		for _, term := range c.Terms() {
			if n := normalizeText(term); n != "" && strings.Contains(hay, n) {
				out = append(out, BrandViolation{Constraint: c, Term: term})
			}
		}
	}
	return out
}

// MissingDisclaimers returns the required disclaimers (of any level) that none of the texts contains.
func (cs BrandConstraints) MissingDisclaimers(texts ...string) []BrandViolation {
	hays := make([]string, len(texts))
	for i, t := range texts {
		hays[i] = normalizeText(t)
	}
	var out []BrandViolation
	for _, c := range cs {
		if c.Type != BrandDisclaimer {
			continue
		}
		need := normalizeText(c.Text)
		found := false
		for _, h := range hays {
			if strings.Contains(h, need) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, BrandViolation{Constraint: c})
		}
	}
	return out
}

// LockedViolations keeps the violations of LOCK constraints.
func LockedViolations(vs []BrandViolation) []BrandViolation {
	var out []BrandViolation
	for _, v := range vs {
		if v.Locked() {
			out = append(out, v)
		}
	}
	return out
}

// CheckText returns an error (ErrBrandViolation) when the text contains a forbidden expression of a
// LOCK constraint; advisory ones are not an error.
func (cs BrandConstraints) CheckText(text string) error {
	if vs := LockedViolations(cs.ForbiddenIn(text)); len(vs) > 0 {
		return &BrandViolationError{Violations: vs}
	}
	return nil
}

// PromptSection is the constraints as instructions for a model, in English ("" when there are none).
// Each entry carries the constraint ID so that a reviewing model can name the one that is broken.
func (cs BrandConstraints) PromptSection() string {
	if len(cs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, c := range cs {
		level := "ADVISORY (avoid where possible)"
		if c.Level == EnforceLock {
			level = "LOCKED (never violate)"
		}
		fmt.Fprintf(&b, "- [%s] id=%s %s: ", level, c.ID, c.Type.englishLabel())
		if c.Type == BrandForbidden {
			b.WriteString("these expressions must not appear anywhere (text on screen, dialogue, narration): " + strings.Join(quoted(c.Terms()), ", "))
		} else {
			b.WriteString(strings.Join(strings.Fields(c.Text), " "))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func quoted(terms []string) []string {
	out := make([]string, len(terms))
	for i, t := range terms {
		out[i] = `"` + t + `"`
	}
	return out
}

// BrandIssueType is the Judge Issue type of a broken Brand / Safety Constraint.
const BrandIssueType = "brand_constraint_violation"

// BrandText is the free text of the spec that Brand constraints are checked against: what the shot
// shows and says (objective, look, actions, label text), not the identifiers and enumerations.
func (s ShotSpec) BrandText() string {
	parts := []string{s.ShotObjective, s.Style, s.Camera.Movement, s.Lighting.Mood}
	if c := s.Character; c != nil {
		parts = append(parts, c.ID, c.Wardrobe, c.Hair)
	}
	if p := s.Product; p != nil {
		parts = append(parts, p.ID, p.Label, p.Orientation)
	}
	if l := s.Location; l != nil {
		parts = append(parts, l.ID, l.Time, l.Weather)
	}
	for _, a := range s.Acting {
		parts = append(parts, a.Action)
	}
	parts = append(parts, s.Constraints...)
	return strings.Join(parts, "\n")
}

// MaxBrandTemplateNameRunes bounds the name of a Brand Template.
const MaxBrandTemplateNameRunes = 100

// BrandTemplate is a named set of Brand / Safety Constraints kept apart from every Project (F-23,
// M3). Applying it to a Project copies the constraints (BrandConstraints.MissingFrom): the Project's
// constraints and the template change independently, and editing a template never reaches a Project
// that has already used it. The constraints have no ProjectID.
type BrandTemplate struct {
	ID          string
	Name        string
	Constraints BrandConstraints
	CreatedAt   time.Time
}

// ValidateBrandTemplateName collapses white space and checks the name is 1..MaxBrandTemplateNameRunes
// runes.
func ValidateBrandTemplateName(name string) (string, bool) {
	name = strings.Join(strings.Fields(name), " ")
	n := utf8.RuneCountInString(name)
	return name, n >= 1 && n <= MaxBrandTemplateNameRunes
}

// MissingFrom returns the constraints of cs that have no equal (same type, level and text) in have:
// what applying a template to a Project with the constraints have has to add.
func (cs BrandConstraints) MissingFrom(have BrandConstraints) BrandConstraints {
	var out BrandConstraints
	for _, c := range cs {
		dup := false
		for _, h := range have {
			if h.Type == c.Type && h.Level == c.Level && h.Text == c.Text {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, c)
		}
	}
	return out
}
