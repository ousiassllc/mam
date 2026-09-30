package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestThreadMessage_PendingIsACardThatCanStillChangeInPlace(t *testing.T) {
	msg := func(kind domain.MessageKind) domain.ThreadMessage {
		return domain.ThreadMessage{ChatMessage: domain.ChatMessage{Kind: kind}}
	}
	withApproval := func(s domain.ApprovalStatus) domain.ThreadMessage {
		m := msg(domain.KindApproval)
		m.ApprovalStatus = s
		return m
	}
	withJob := func(j *domain.Job) domain.ThreadMessage {
		m := msg(domain.KindProgress)
		m.Job = j
		return m
	}
	open := func(kind domain.MessageKind, o bool) domain.ThreadMessage {
		m := msg(kind)
		m.Open = o
		return m
	}
	cases := map[string]struct {
		m    domain.ThreadMessage
		want bool
	}{
		"approval waiting":        {withApproval(domain.ApprovalPending), true},
		"approval approved":       {withApproval(domain.ApprovalApproved), false},
		"approval changes asked":  {withApproval(domain.ApprovalChangesRequested), false},
		"progress of unknown job": {withJob(nil), true},
		"progress queued":         {withJob(&domain.Job{Status: domain.JobQueued}), true},
		"progress running":        {withJob(&domain.Job{Status: domain.JobRunning}), true},
		"progress succeeded":      {withJob(&domain.Job{Status: domain.JobSucceeded}), false},
		"progress failed":         {withJob(&domain.Job{Status: domain.JobFailed}), false},
		"open question":           {open(domain.KindQuestion, true), true},
		"answered question":       {open(domain.KindQuestion, false), false},
		"open escalation":         {open(domain.KindEscalation, true), true},
		"text":                    {msg(domain.KindText), false},
		"take":                    {msg(domain.KindTake), false},
	}
	for name, tc := range cases {
		assert.Equal(t, tc.want, tc.m.Pending(), name)
	}
}
