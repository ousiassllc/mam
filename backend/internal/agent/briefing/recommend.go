package briefing

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Platform is a posting destination whose format the Brief proposes (spec/spec.md §4.2, #1114). The
// proposal is tentative: a platform does not decide the length, so Duration is one value inside UsualRange
// and the Brief marks it as recommended until the user confirms or changes it.
type Platform struct {
	Name        string
	AspectRatio string
	Duration    string // the proposed Duration
	UsualRange  string // the range the platform usually takes (documentation of the proposal; never stored)
}

// platforms is the deterministic table of the proposals. A destination that is not in it (Instagram広告, YouTube広告, a web site, nothing at all) gets no proposal: both items stay open for the batch summary.
var platforms = []struct {
	Platform
	matches func(objective string) bool
}{
	{Platform{"TikTok", domain.AspectRatio9x16, "15秒", "15〜60秒"}, func(o string) bool { return hasAny(o, "tiktok") }},
	{Platform{"YouTube Shorts", domain.AspectRatio9x16, "30秒", "15〜60秒"}, func(o string) bool {
		return hasAny(o, "shorts") || (hasAny(o, "youtube") && hasAny(o, "ショート"))
	}},
	{Platform{"Instagram Reels", domain.AspectRatio9x16, "15秒", "15〜30秒"}, func(o string) bool {
		return hasAny(o, "reels") || (hasAny(o, "instagram") && hasAny(o, "リール"))
	}},
}

// PlatformOf is the platform the Objective names, if the table knows it.
func PlatformOf(objective string) (Platform, bool) {
	for _, p := range platforms {
		if p.matches(objective) {
			return p.Platform, true
		}
	}
	return Platform{}, false
}

func hasAny(s string, words ...string) bool {
	s = strings.ToLower(s)
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// Recommend fills Duration and Aspect Ratio from the platform in the Objective as tentative proposals
// (domain.BriefSourceRecommended). It only writes an item nobody gave a value (and did not leave to the AI) or
// one that is itself a proposal, so a value the user stated or confirmed always wins; a proposal whose platform is gone is taken back.
func Recommend(b *domain.Brief) {
	p, known := PlatformOf(b.Objective)
	for f, v := range map[domain.BriefField]string{domain.BriefDuration: p.Duration, domain.BriefAspectRatio: p.AspectRatio} {
		switch {
		case b.Get(f) != "" && b.SourceOf(f) != domain.BriefSourceRecommended:
		case !known && b.Get(f) != "":
			b.Set(f, "")
		case known && !b.IsDelegated(f):
			SetFrom(b, f, v, domain.BriefSourceRecommended)
		}
	}
}
