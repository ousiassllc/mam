package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Project is one video production and its single chat thread (F-25, F-30).
type Project struct {
	ID                string
	Name              string // derived, see ProjectName
	State             ProjectState
	Mode              Mode
	ConfirmationLevel ConfirmationLevel
	// CustomCheckpoints is the selection used when ConfirmationLevel is CUSTOM CHECK.
	CustomCheckpoints  []Checkpoint
	ProductionTemplate ProductionTemplate
	QualityPolicy      QualityPolicy
	CreatedAt          time.Time
}

// UntitledProject is the display name of a Project with neither Subject nor first message.
const UntitledProject = "無題のプロジェクト"

const projectNameMaxRunes = 40

// ProjectName derives the display name (the schema stores none): the Brief Subject,
// otherwise the first user message, truncated to one line.
func ProjectName(subject, firstMessage string) string {
	for _, s := range []string{subject, firstMessage} {
		s = strings.Join(strings.Fields(s), " ")
		if s == "" {
			continue
		}
		if utf8.RuneCountInString(s) > projectNameMaxRunes {
			return string([]rune(s)[:projectNameMaxRunes]) + "…"
		}
		return s
	}
	return UntitledProject
}
