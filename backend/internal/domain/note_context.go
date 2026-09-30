package domain

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SelectNotes chooses the notes an Agent is given (spec/spec.md §9.4): the pinned notes always, then
// the others by relevance to query for as long as their text fits in what is left of budget (runes).
// Archived notes are never chosen. Equally relevant notes are ordered newest first.
func SelectNotes(notes []NoteView, query string, budget int) []NoteView {
	var pinned, rest []NoteView
	for _, n := range notes {
		switch {
		case n.Archived():
		case n.Pinned:
			pinned = append(pinned, n)
		default:
			rest = append(rest, n)
		}
	}
	out := slices.Clone(pinned)
	for _, n := range pinned {
		budget -= n.textRunes()
	}
	want := textTokens(query)
	score := make(map[string]int, len(rest))
	for _, n := range rest {
		score[n.ID] = relevance(want, n.Version.Title+"\n"+n.Version.Content)
	}
	slices.SortStableFunc(rest, func(a, b NoteView) int {
		if score[a.ID] != score[b.ID] {
			return score[b.ID] - score[a.ID]
		}
		return b.Version.CreatedAt.Compare(a.Version.CreatedAt)
	})
	for _, n := range rest {
		if r := n.textRunes(); r <= budget {
			out = append(out, n)
			budget -= r
		}
	}
	return out
}

func (v NoteView) textRunes() int {
	return utf8.RuneCountInString(v.Version.Title) + utf8.RuneCountInString(v.Version.Content)
}

// NotesText renders notes for the prompt of an Agent. It is empty when there is no note. The notes
// are context only: the structured state and the constraints win over them.
func NotesText(notes []NoteView) string {
	var b strings.Builder
	for _, n := range notes {
		pin := ""
		if n.Pinned {
			pin = ", pinned"
		}
		fmt.Fprintf(&b, "### %s [%s%s]\n%s\n", n.Version.Title, n.Category, pin, strings.TrimSpace(n.Version.Content))
	}
	return b.String()
}

// NoteRefs lists the versions of notes for prompt_version.note_refs (never nil).
func NoteRefs(notes []NoteView) []NoteRef {
	refs := make([]NoteRef, len(notes))
	for i, n := range notes {
		refs[i] = n.Ref()
	}
	return refs
}

// relevance counts the tokens of want that the text has.
func relevance(want map[string]struct{}, text string) int {
	if len(want) == 0 {
		return 0
	}
	have := textTokens(text)
	n := 0
	for t := range want {
		if _, ok := have[t]; ok {
			n++
		}
	}
	return n
}

// textTokens splits text into words (runs of letters and digits, at least 2 runes) and, for runs of
// Japanese or Chinese characters, overlapping pairs of characters, so that matching needs no
// dictionary.
func textTokens(text string) map[string]struct{} {
	tokens := map[string]struct{}{}
	var run []rune
	cjkRun := false
	flush := func() {
		switch {
		case len(run) == 0:
		case cjkRun && len(run) == 1:
			tokens[string(run)] = struct{}{}
		case cjkRun:
			for i := 0; i+1 < len(run); i++ {
				tokens[string(run[i:i+2])] = struct{}{}
			}
		case len(run) >= 2:
			tokens[string(run)] = struct{}{}
		}
		run = run[:0]
	}
	for _, r := range strings.ToLower(text) {
		isCJK := unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana)
		if !isCJK && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(run) > 0 && isCJK != cjkRun {
			flush()
		}
		cjkRun = isCJK
		run = append(run, r)
	}
	flush()
	return tokens
}
